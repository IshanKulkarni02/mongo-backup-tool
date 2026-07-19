package snapshot

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

// scanSQLTableRows reads every row in table (via tx, a consistent-read
// transaction) and computes each one's stable ID (from its primary key)
// and content hash — the SQL analog of pipeline.go's scanCollectionDocs.
// CPU-bound work (canonical JSON marshaling, SHA-256 hashing, zstd
// compression) runs in parallel across a worker pool, fed by a single
// goroutine draining StreamRows and drained by a single writer batching
// results into store.PutMany — the identical shape scanCollectionDocs
// uses, just reading rows via engine.ConsistentReadTx instead of a Mongo
// cursor. Results are sorted via the same bounded external-merge spill
// (extsort.go); the caller owns the returned spill and must eventually
// call its Cleanup().
func scanSQLTableRows(ctx context.Context, tx engine.ConsistentReadTx, database, table string, pkColumns []string, store ObjectStore) (*extSortSpill, int, int, error) {
	type job struct {
		row map[string]any
	}
	type result struct {
		ref DocRef
		obj EncodedObject
		err error
	}

	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan job, workers*2)
	results := make(chan result, workers*2)

	var workerWG sync.WaitGroup
	for i := 0; i < workers; i++ {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			codec, err := newDocCodec()
			if err != nil {
				results <- result{err: err}
				return
			}
			defer codec.Close()

			for j := range jobs {
				id, err := rowIdentity(pkColumns, j.row)
				if err != nil {
					results <- result{err: err}
					continue
				}
				data, err := canonicalRowBytes(j.row)
				if err != nil {
					results <- result{err: err}
					continue
				}
				obj := encodeDocument(codec, data)
				results <- result{ref: DocRef{ID: id, Hash: obj.Hash}, obj: obj}
			}
		}()
	}

	// scanErr is only written before the goroutine below returns (which
	// triggers the deferred close(jobs)), and only read after results is
	// drained — which can't happen until workerWG.Wait() sees every worker
	// finish, which can't happen until jobs is closed. That chain of
	// channel/WaitGroup happens-before relationships makes this safe
	// without its own lock.
	var scanErr error
	go func() {
		defer close(jobs)
		if err := tx.StreamRows(ctx, database, table, func(row map[string]any) error {
			jobs <- job{row: row}
			return nil
		}); err != nil {
			scanErr = err
		}
	}()

	go func() {
		workerWG.Wait()
		close(results)
	}()

	spill, err := newExtSortSpill()
	if err != nil {
		return nil, 0, 0, err
	}

	const writeBatchSize = 500
	batch := make([]EncodedObject, 0, writeBatchSize)
	rowCount := 0
	newObjects := 0
	var firstErr error

	flush := func() error {
		if len(batch) == 0 || store == nil {
			batch = batch[:0]
			return nil
		}
		n, err := store.PutMany(batch)
		batch = batch[:0]
		if err != nil {
			return err
		}
		newObjects += n
		return nil
	}

	for r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		if firstErr != nil {
			continue
		}
		if err := spill.Add(r.ref); err != nil {
			firstErr = err
			continue
		}
		rowCount++
		if store != nil {
			batch = append(batch, r.obj)
			if len(batch) >= writeBatchSize {
				if err := flush(); err != nil && firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	if err := flush(); err != nil && firstErr == nil {
		firstErr = err
	}
	if firstErr == nil && scanErr != nil {
		firstErr = scanErr
	}
	if firstErr != nil {
		spill.Cleanup()
		return nil, 0, 0, firstErr
	}

	return spill, rowCount, newObjects, nil
}

// scanSQLDatabase reads every table in database. store may be nil (a
// future SQL live-scan/diff feature, mirroring Mongo's ScanLive — not used
// by CreateSQL, which always persists).
//
// A table with no primary key is skipped (name recorded in skipped, not
// added to tables) rather than snapshotted: there's no stable per-row
// identity to diff against without one, and using row content itself as
// identity would make every edit look like a delete+insert, defeating the
// point of the modified/added/removed distinction diff.go provides.
//
// For each scanned table, once its DocRefs are fully scanned and sorted
// (bounded memory throughout, see scanSQLTableRows), onTable is invoked
// with the table's spill — mirroring scanDatabase's onCollection contract
// (the caller decides what to do with it, and is responsible for
// eventually cleaning it up).
func scanSQLDatabase(ctx context.Context, sess engine.SQLSession, database string, store ObjectStore, onTable func(name string, cm CollectionManifest, spill *extSortSpill) error) (tables map[string]CollectionManifest, newObjects int, skipped []string, err error) {
	namespaces, err := sess.ListNamespaces(ctx, database)
	if err != nil {
		return nil, 0, nil, err
	}

	tables = make(map[string]CollectionManifest, len(namespaces))

	for _, ns := range namespaces {
		schema, err := sess.TableSchema(ctx, database, ns.Name)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("introspecting %s: %w", ns.Name, err)
		}
		if len(schema.PrimaryKey) == 0 {
			skipped = append(skipped, ns.Name)
			continue
		}

		indexes, err := sess.ListTableIndexes(ctx, database, ns.Name)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("listing indexes for %s: %w", ns.Name, err)
		}
		indexDDL := make([]string, len(indexes))
		for i, idx := range indexes {
			indexDDL[i] = idx.DDL
		}

		tx, err := sess.BeginConsistentRead(ctx)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("beginning consistent read for %s: %w", ns.Name, err)
		}

		spill, rowCount, n, scanErr := scanSQLTableRows(ctx, tx, database, ns.Name, schema.PrimaryKey, store)
		closeErr := tx.Close(ctx)
		if scanErr != nil {
			return nil, 0, nil, scanErr
		}
		if closeErr != nil {
			return nil, 0, nil, closeErr
		}
		newObjects += n

		cm := CollectionManifest{DocCount: rowCount, PrimaryKey: schema.PrimaryKey, IndexDDL: indexDDL}
		if err := onTable(ns.Name, cm, spill); err != nil {
			return nil, 0, nil, err
		}
		tables[ns.Name] = cm
	}

	return tables, newObjects, skipped, nil
}
