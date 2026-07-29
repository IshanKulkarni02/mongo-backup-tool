package snapshot

import (
	"context"
	"runtime"
	"sync"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// scanCollectionDocs reads every document in coll and computes each one's
// stable ID and content hash. The CPU-bound work — canonical Extended JSON
// marshaling, SHA-256 hashing, zstd compression — runs in parallel across a
// worker pool (default GOMAXPROCS workers), fed by a single goroutine
// reading the cursor (cursors aren't safe for concurrent use) and drained by
// a single writer that batches results into store.PutMany, keeping lock
// contention on the backend low regardless of worker count. If store is
// nil (used by ScanLive), content is hashed but never persisted.
//
// Results are sorted via a bounded external-merge spill (extsort.go) rather
// than an in-memory slice: memory use stays proportional to one merge-sort
// run (extSortRunSize entries), not the collection size, so scanning a
// million-document collection doesn't require holding every one of its
// DocRefs in RAM at once. The caller owns the returned spill and must
// eventually call its Cleanup().
func scanCollectionDocs(ctx context.Context, coll *mongo.Collection, store ObjectStore) (*extSortSpill, int, int, error) {
	cur, err := coll.Find(ctx, bson.D{})
	if err != nil {
		return nil, 0, 0, err
	}
	defer cur.Close(ctx)

	type job struct {
		doc bson.D
		err error
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
				if j.err != nil {
					results <- result{err: j.err}
					continue
				}
				id, err := idKey(j.doc)
				if err != nil {
					results <- result{err: err}
					continue
				}
				data, err := canonicalBytes(j.doc)
				if err != nil {
					results <- result{err: err}
					continue
				}
				obj := encodeDocument(codec, data)
				results <- result{ref: DocRef{ID: id, Hash: obj.Hash}, obj: obj}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for cur.Next(ctx) {
			var doc bson.D
			err := cur.Decode(&doc)
			jobs <- job{doc: doc, err: err}
			if err != nil {
				return
			}
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
	docCount := 0
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
			continue // drain remaining results after an error, but stop doing work
		}
		if err := spill.Add(r.ref); err != nil {
			firstErr = err
			continue
		}
		docCount++
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
	if firstErr != nil {
		spill.Cleanup()
		return nil, 0, 0, firstErr
	}
	if err := cur.Err(); err != nil {
		spill.Cleanup()
		return nil, 0, 0, err
	}

	return spill, docCount, newObjects, nil
}

func scanIndexes(ctx context.Context, coll *mongo.Collection) ([]IndexSpec, error) {
	cur, err := coll.Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var specs []IndexSpec
	for cur.Next(ctx) {
		var raw bson.D
		if err := cur.Decode(&raw); err != nil {
			return nil, err
		}
		spec := IndexSpec{Options: bson.M{}}
		for _, e := range raw {
			switch e.Key {
			case "name":
				if s, ok := e.Value.(string); ok {
					spec.Name = s
				}
			case "key":
				if d, ok := e.Value.(bson.D); ok {
					spec.Keys = d
				}
			case "v", "ns":
				// build metadata; not needed to recreate the index
			default:
				spec.Options[e.Key] = e.Value
			}
		}
		if len(spec.Options) == 0 {
			spec.Options = nil
		}
		specs = append(specs, spec)
	}
	return specs, cur.Err()
}

// scanDatabase reads every collection in db. store may be nil (ScanLive:
// hash but don't persist object content).
//
// catalogCtx is used only for catalog/DDL-style reads (listCollections,
// listIndexes) and scanCtx only for document reads (coll.Find) — MongoDB
// does not allow listCollections (and, per the same catalog-vs-data-snapshot
// restriction, listIndexes) inside a multi-document transaction, so a
// snapshot's readConcern:snapshot session (beginSnapshotRead) must never be
// used for those calls. catalogCtx is ordinarily the plain pre-transaction
// context; scanCtx is the (possibly transactional) context that carries the
// point-in-time guarantee for document content. This means the collection
// list and index definitions are captured just before the transactional
// document scan begins, not atomically with it — an inherent MongoDB
// limitation (there's no snapshot-consistent way to read the catalog), not
// a gap in this tool: the guarantee that matters (every collection's
// document content reflects one consistent instant) still holds.
//
// For each collection, once its DocRefs are fully scanned and sorted
// (bounded memory throughout, see scanCollectionDocs), onCollection is
// invoked with the collection's spill — the caller decides what to do with
// it (persist immediately and clean up, for Create; or keep it open as a
// diff source, for ScanLive) and is responsible for eventually cleaning it
// up. Only one collection's spill is ever open here at a time —
// onCollection must finish with it (persist or take ownership) before the
// next collection starts scanning.
func scanDatabase(catalogCtx, scanCtx context.Context, db *mongo.Database, store ObjectStore, onCollection func(name string, spill *extSortSpill) error) (map[string]CollectionManifest, int, error) {
	names, err := db.ListCollectionNames(catalogCtx, bson.D{})
	if err != nil {
		return nil, 0, err
	}

	collections := make(map[string]CollectionManifest, len(names))
	newObjects := 0

	for _, name := range names {
		coll := db.Collection(name)

		indexSpecs, err := scanIndexes(catalogCtx, coll)
		if err != nil {
			return nil, 0, err
		}

		spill, docCount, n, err := scanCollectionDocs(scanCtx, coll, store)
		if err != nil {
			return nil, 0, err
		}
		newObjects += n

		if err := onCollection(name, spill); err != nil {
			return nil, 0, err
		}

		collections[name] = CollectionManifest{Indexes: indexSpecs, DocCount: docCount}
	}

	return collections, newObjects, nil
}
