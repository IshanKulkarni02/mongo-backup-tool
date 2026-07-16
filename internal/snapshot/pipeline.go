package snapshot

import (
	"context"
	"runtime"
	"sort"
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
func scanCollectionDocs(ctx context.Context, coll *mongo.Collection, store ObjectStore) ([]DocRef, int, error) {
	cur, err := coll.Find(ctx, bson.D{})
	if err != nil {
		return nil, 0, err
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

	const writeBatchSize = 500
	batch := make([]EncodedObject, 0, writeBatchSize)
	var refs []DocRef
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
		refs = append(refs, r.ref)
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
		return nil, 0, firstErr
	}
	if err := cur.Err(); err != nil {
		return nil, 0, err
	}

	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	return refs, newObjects, nil
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

// scanDatabase reads every collection in db. store may be nil (ScanLive: hash
// but don't persist). It returns each collection's manifest metadata, its
// full sorted DocRef list (the caller decides whether/how to persist those —
// Create writes them via Backend.WriteDocRefs once it has a snapshot ID;
// ScanLive keeps them in memory for an immediate diff), and how many objects
// were newly written.
func scanDatabase(ctx context.Context, db *mongo.Database, store ObjectStore) (map[string]CollectionManifest, map[string][]DocRef, int, error) {
	names, err := db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, nil, 0, err
	}

	collections := make(map[string]CollectionManifest, len(names))
	docRefs := make(map[string][]DocRef, len(names))
	newObjects := 0

	for _, name := range names {
		coll := db.Collection(name)

		indexSpecs, err := scanIndexes(ctx, coll)
		if err != nil {
			return nil, nil, 0, err
		}

		refs, n, err := scanCollectionDocs(ctx, coll, store)
		if err != nil {
			return nil, nil, 0, err
		}
		newObjects += n

		collections[name] = CollectionManifest{Indexes: indexSpecs, DocCount: len(refs)}
		docRefs[name] = refs
	}

	return collections, docRefs, newObjects, nil
}
