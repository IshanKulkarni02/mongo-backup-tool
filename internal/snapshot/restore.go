package snapshot

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// RestoreOptions configures restoring a snapshot into a live database.
type RestoreOptions struct {
	SourceConnection string // connection name the snapshot was created from (identifies its scope)
	SourceDatabase   string // database name the snapshot was created from (identifies its scope)
	SnapshotID       string // snapshot ID, or a unique ID prefix

	TargetURI      string // connection URI to restore into
	TargetDatabase string // defaults to SourceDatabase if empty
	Collection     string // restore only this collection; empty = every collection in the snapshot
	Drop           bool   // drop each target collection before inserting
}

// RestoreResult summarizes a completed restore.
type RestoreResult struct {
	Database    string
	Collections []string
	DocsWritten int
}

// Restore applies a stored snapshot to a live database via batched
// InsertMany, then recreates the snapshot's indexes.
func Restore(opts RestoreOptions) (*RestoreResult, error) {
	m, err := Get(opts.SourceConnection, opts.SourceDatabase, opts.SnapshotID)
	if err != nil {
		return nil, err
	}

	scope, err := scopeDir(opts.SourceConnection, opts.SourceDatabase)
	if err != nil {
		return nil, err
	}
	backend, err := OpenBackend(scope, "")
	if err != nil {
		return nil, err
	}
	defer backend.Close()

	targetDB := opts.TargetDatabase
	if targetDB == "" {
		targetDB = opts.SourceDatabase
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(opts.TargetURI))
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(context.Background())

	db := client.Database(targetDB)

	var names []string
	for name := range m.Collections {
		if opts.Collection != "" && name != opts.Collection {
			continue
		}
		names = append(names, name)
	}
	if opts.Collection != "" && len(names) == 0 {
		return nil, fmt.Errorf("snapshot %s has no collection %q", m.ID, opts.Collection)
	}
	sort.Strings(names)

	totalDocs := 0
	for _, name := range names {
		cm := m.Collections[name]
		coll := db.Collection(name)

		if opts.Drop {
			if err := coll.Drop(ctx); err != nil {
				return nil, fmt.Errorf("dropping %s before restore: %w", name, err)
			}
		}

		it, err := backend.IterDocRefs(m.ID, name)
		if err != nil {
			return nil, fmt.Errorf("reading doc refs for %s: %w", name, err)
		}
		written, err := insertDocs(ctx, coll, backend, it)
		if err != nil {
			return nil, fmt.Errorf("restoring %s: %w", name, err)
		}
		totalDocs += written

		if err := recreateIndexes(ctx, coll, cm.Indexes); err != nil {
			return nil, fmt.Errorf("recreating indexes for %s: %w", name, err)
		}
	}

	return &RestoreResult{Database: targetDB, Collections: names, DocsWritten: totalDocs}, nil
}

// RestoreWithSafety snapshots the restore target before a destructive
// (Drop=true) restore, so the prior state is never unrecoverable. A
// non-destructive restore (Drop=false) only adds/overwrites documents and
// never deletes data, so no safety snapshot is needed.
func RestoreWithSafety(opts RestoreOptions, safetyConnectionName string) (result *RestoreResult, safety *CreateResult, err error) {
	if opts.Drop {
		targetDB := opts.TargetDatabase
		if targetDB == "" {
			targetDB = opts.SourceDatabase
		}
		safety, err = Create(CreateOptions{
			Connection: safetyConnectionName,
			URI:        opts.TargetURI,
			Database:   targetDB,
			Message:    fmt.Sprintf("auto safety snapshot before restoring %s", opts.SnapshotID),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("taking safety snapshot before restore: %w", err)
		}
	}
	result, err = Restore(opts)
	return result, safety, err
}

// insertDocs streams documents out of it (never materializing the whole
// collection at once) and batch-inserts them, returning how many were
// written.
func insertDocs(ctx context.Context, coll *mongo.Collection, store ObjectStore, it docRefIterator) (int, error) {
	defer it.Close()

	const batchSize = 500
	batch := make([]interface{}, 0, batchSize)
	written := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if _, err := coll.InsertMany(ctx, batch); err != nil {
			return err
		}
		written += len(batch)
		batch = batch[:0]
		return nil
	}

	for {
		ref, ok, err := it.Next()
		if err != nil {
			return written, err
		}
		if !ok {
			break
		}
		data, err := store.Get(ref.Hash)
		if err != nil {
			return written, err
		}
		var doc bson.D
		if err := bson.UnmarshalExtJSON(data, true, &doc); err != nil {
			return written, err
		}
		batch = append(batch, doc)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return written, err
			}
		}
	}
	if err := flush(); err != nil {
		return written, err
	}
	return written, nil
}

func recreateIndexes(ctx context.Context, coll *mongo.Collection, specs []IndexSpec) error {
	for _, spec := range specs {
		if spec.Name == "_id_" {
			continue // Mongo creates this automatically
		}
		idxOpts := options.Index().SetName(spec.Name)
		if v, ok := spec.Options["unique"].(bool); ok && v {
			idxOpts.SetUnique(true)
		}
		if v, ok := spec.Options["sparse"].(bool); ok && v {
			idxOpts.SetSparse(true)
		}
		if v, ok := spec.Options["expireAfterSeconds"]; ok {
			if secs, ok := toInt32(v); ok {
				idxOpts.SetExpireAfterSeconds(secs)
			}
		}
		model := mongo.IndexModel{Keys: spec.Keys, Options: idxOpts}
		if _, err := coll.Indexes().CreateOne(ctx, model); err != nil {
			return fmt.Errorf("index %s: %w", spec.Name, err)
		}
	}
	return nil
}

func toInt32(v interface{}) (int32, bool) {
	switch n := v.(type) {
	case int32:
		return n, true
	case int64:
		return int32(n), true
	case float64:
		return int32(n), true
	}
	return 0, false
}
