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

// RestoreResult summarizes a restore. When Restore returns an error,
// Collections/DocsWritten still reflect exactly how far it got before
// failing — every collection named in Collections was already
// dropped-and-reinserted (if Drop was set) and had its indexes recreated;
// the failure happened on the next one.
//
// Touched is a superset of Collections: it also includes a collection that
// was dropped but then failed before finishing (e.g. insertDocs or
// recreateIndexes errored right after Drop succeeded) — that collection is
// destructively damaged (its old content is gone) even though it never
// made it into Collections. RestoreWithSafety uses Touched, not Collections,
// to decide whether an automatic rollback is needed — using Collections
// there would miss damage confined to the very first collection that began
// dropping before it failed.
type RestoreResult struct {
	Database    string
	Collections []string
	Touched     []string
	DocsWritten int
}

// Restore applies a stored snapshot to a live database via batched
// InsertMany, then recreates the snapshot's indexes, one collection at a
// time. If a collection fails partway through a multi-collection restore,
// the returned RestoreResult still lists every collection successfully
// completed (Collections) and every collection that was at least
// destructively touched (Touched) before the failure — see
// RestoreWithSafety, which uses Touched to automatically roll back a
// destructive restore left half-applied.
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

	result := &RestoreResult{Database: targetDB}
	for _, name := range names {
		cm := m.Collections[name]
		coll := db.Collection(name)

		if opts.Drop {
			// Recorded BEFORE Drop runs, not after it succeeds: Drop itself
			// is the destructive act, so even if Drop's own call somehow
			// returns an error after partially applying (or anything after
			// it fails), this collection must be treated as damaged.
			result.Touched = append(result.Touched, name)
			if err := coll.Drop(ctx); err != nil {
				return result, fmt.Errorf("dropping %s before restore: %w", name, err)
			}
		}

		it, err := backend.IterDocRefs(m.ID, name)
		if err != nil {
			return result, fmt.Errorf("reading doc refs for %s: %w", name, err)
		}
		written, err := insertDocs(ctx, coll, backend, it)
		if err != nil {
			return result, fmt.Errorf("restoring %s: %w", name, err)
		}
		result.DocsWritten += written

		if err := recreateIndexes(ctx, coll, cm.Indexes); err != nil {
			return result, fmt.Errorf("recreating indexes for %s: %w", name, err)
		}

		result.Collections = append(result.Collections, name)
	}

	return result, nil
}

// RestoreWithSafety snapshots the restore target before a destructive
// (Drop=true) restore, so the prior state is never unrecoverable. A
// non-destructive restore (Drop=false) only adds/overwrites documents and
// never deletes data, so no safety snapshot is needed.
//
// If a destructive, multi-collection restore fails partway through — e.g.
// collection two of three's index recreation errors, after collection one
// was already dropped and reinserted — the target is left in a mixed state:
// part still-original, part already-swapped-to-the-new-snapshot's-content.
// Rather than leave that for the caller to notice and recover manually,
// RestoreWithSafety detects it (via Restore's partial RestoreResult) and
// automatically restores from the safety snapshot to bring every touched
// collection back to its pre-restore state. rolledBack reports whether that
// happened; if the rollback attempt itself fails, both errors are reported
// together and manual recovery from the safety snapshot ID is the fallback.
func RestoreWithSafety(opts RestoreOptions, safetyConnectionName string) (result *RestoreResult, safety *CreateResult, rolledBack bool, err error) {
	targetDB := opts.TargetDatabase
	if targetDB == "" {
		targetDB = opts.SourceDatabase
	}

	if opts.Drop {
		safety, err = Create(CreateOptions{
			Connection: safetyConnectionName,
			URI:        opts.TargetURI,
			Database:   targetDB,
			Message:    fmt.Sprintf("auto safety snapshot before restoring %s", opts.SnapshotID),
		})
		if err != nil {
			return nil, nil, false, fmt.Errorf("taking safety snapshot before restore: %w", err)
		}
	}

	result, err = Restore(opts)
	if err == nil {
		return result, safety, false, nil
	}

	// A restore that failed without ever touching a collection (e.g. the
	// snapshot ID didn't resolve, or the target URI was unreachable) needs
	// no rollback — nothing changed. Deliberately checked via Touched, not
	// Collections: a failure right after the very first collection's Drop
	// (before it ever reaches Collections) still did real, rollback-worthy
	// damage.
	partialDamage := opts.Drop && safety != nil && result != nil && len(result.Touched) > 0
	if !partialDamage {
		return result, safety, false, err
	}

	_, rbErr := Restore(RestoreOptions{
		SourceConnection: safetyConnectionName,
		SourceDatabase:   targetDB,
		SnapshotID:       safety.Summary.ID,
		TargetURI:        opts.TargetURI,
		TargetDatabase:   targetDB,
		Drop:             true,
	})
	if rbErr != nil {
		return result, safety, false, fmt.Errorf("restore failed partway through (%d collection(s) applied: %w) — automatic rollback ALSO failed (%v); manually restore safety snapshot %s into %q to recover", len(result.Collections), err, rbErr, safety.Summary.ID, targetDB)
	}
	return result, safety, true, fmt.Errorf("restore failed partway through (%d collection(s) applied) and was automatically rolled back to the pre-restore state using safety snapshot %s: %w", len(result.Collections), safety.Summary.ID, err)
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

// recreateIndexes restores every index option scanIndexes captured, not
// just unique/sparse/TTL: hidden, partial-filter, collation, wildcard
// projection, and the text/geo index options (default language, language
// override, text version, weights, 2dsphere version, bits/min/max for
// legacy 2d/geoHaystack indexes). scanIndexes already stores every option
// MongoDB reports for an index generically (see pipeline.go); this is what
// actually applies all of it back, rather than silently dropping anything
// not explicitly named.
func recreateIndexes(ctx context.Context, coll *mongo.Collection, specs []IndexSpec) error {
	for _, spec := range specs {
		if spec.Name == "_id_" {
			continue // Mongo creates this automatically
		}
		idxOpts := options.Index().SetName(spec.Name)
		opts := spec.Options

		if v, ok := opts["unique"].(bool); ok && v {
			idxOpts.SetUnique(true)
		}
		if v, ok := opts["sparse"].(bool); ok && v {
			idxOpts.SetSparse(true)
		}
		if v, ok := opts["hidden"].(bool); ok && v {
			idxOpts.SetHidden(true)
		}
		if v, ok := opts["expireAfterSeconds"]; ok {
			if secs, ok := toInt32(v); ok {
				idxOpts.SetExpireAfterSeconds(secs)
			}
		}
		if v, ok := opts["partialFilterExpression"]; ok {
			idxOpts.SetPartialFilterExpression(v)
		}
		if v, ok := opts["wildcardProjection"]; ok {
			idxOpts.SetWildcardProjection(v)
		}
		if v, ok := opts["default_language"].(string); ok && v != "" {
			idxOpts.SetDefaultLanguage(v)
		}
		if v, ok := opts["language_override"].(string); ok && v != "" {
			idxOpts.SetLanguageOverride(v)
		}
		if v, ok := opts["textIndexVersion"]; ok {
			if ver, ok := toInt32(v); ok {
				idxOpts.SetTextVersion(ver)
			}
		}
		if v, ok := opts["weights"]; ok {
			idxOpts.SetWeights(v)
		}
		if v, ok := opts["2dsphereIndexVersion"]; ok {
			if ver, ok := toInt32(v); ok {
				idxOpts.SetSphereVersion(ver)
			}
		}
		if v, ok := opts["bits"]; ok {
			if bits, ok := toInt32(v); ok {
				idxOpts.SetBits(bits)
			}
		}
		if v, ok := opts["min"]; ok {
			if f, ok := toFloat64(v); ok {
				idxOpts.SetMin(f)
			}
		}
		if v, ok := opts["max"]; ok {
			if f, ok := toFloat64(v); ok {
				idxOpts.SetMax(f)
			}
		}
		if v, ok := opts["collation"]; ok {
			if c := toCollation(v); c != nil {
				idxOpts.SetCollation(c)
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

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// toCollation converts a captured collation sub-document — a bson.M when
// read directly off a live index, or a map[string]interface{} with
// float64-typed numbers once round-tripped through the JSON-encoded
// manifest — into the driver's typed Collation options struct.
func toCollation(v interface{}) *options.Collation {
	m, ok := v.(map[string]interface{})
	if !ok {
		if bm, ok := v.(bson.M); ok {
			m = map[string]interface{}(bm)
		} else {
			return nil
		}
	}
	c := &options.Collation{}
	if s, ok := m["locale"].(string); ok {
		c.Locale = s
	}
	if b, ok := m["caseLevel"].(bool); ok {
		c.CaseLevel = b
	}
	if s, ok := m["caseFirst"].(string); ok {
		c.CaseFirst = s
	}
	if n, ok := toInt32(m["strength"]); ok {
		c.Strength = int(n)
	}
	if b, ok := m["numericOrdering"].(bool); ok {
		c.NumericOrdering = b
	}
	if s, ok := m["alternate"].(string); ok {
		c.Alternate = s
	}
	if s, ok := m["maxVariable"].(string); ok {
		c.MaxVariable = s
	}
	if b, ok := m["normalization"].(bool); ok {
		c.Normalization = b
	}
	if b, ok := m["backwards"].(bool); ok {
		c.Backwards = b
	}
	return c
}
