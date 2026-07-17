package snapshot

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
)

// CreateOptions configures a new snapshot.
type CreateOptions struct {
	Connection string
	URI        string
	Database   string
	Message    string
	// Backend selects the storage engine for a brand-new scope (ignored for
	// a scope that already exists — it keeps its original backend). Empty
	// defaults to BackendBolt.
	Backend BackendKind
}

// CreateResult summarizes a newly created snapshot.
type CreateResult struct {
	Summary    Summary
	Consistent bool // true if readConcern:snapshot was used (replica set); false if degraded to a plain scan
}

// Create scans a live database and stores a new snapshot of it, deduping
// document content against everything already stored for this
// connection+database.
func Create(opts CreateOptions) (*CreateResult, error) {
	scope, err := scopeDir(opts.Connection, opts.Database)
	if err != nil {
		return nil, err
	}
	backend, err := OpenBackend(scope, opts.Backend)
	if err != nil {
		return nil, err
	}
	defer backend.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(opts.URI))
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(context.Background())

	scanCtx, consistent, endSession := beginSnapshotRead(ctx, client)
	defer endSession()

	idx, err := loadIndex(scope)
	if err != nil {
		return nil, err
	}
	parentID := ""
	if latest, ok := idx.Latest(); ok {
		parentID = latest.ID
	}

	// The manifest ID is generated up front (rather than after scanning) so
	// each collection's doc refs can be written to the backend as soon as
	// that collection finishes scanning — never holding more than one
	// collection's bounded spill in memory, and never holding every
	// collection's doc-ref list until the whole database scan completes.
	manifestID := uuid.NewString()

	var writeErr error
	onCollection := func(name string, spill *extSortSpill) error {
		defer spill.Cleanup()
		it, err := spill.NewIterator()
		if err != nil {
			writeErr = err
			return err
		}
		if err := backend.WriteDocRefs(manifestID, name, it); err != nil {
			writeErr = fmt.Errorf("writing doc refs for %s: %w", name, err)
			return writeErr
		}
		return nil
	}

	collections, newObjects, err := scanDatabase(ctx, scanCtx, client.Database(opts.Database), backend, onCollection)
	if err != nil {
		if writeErr != nil {
			return nil, writeErr
		}
		return nil, err
	}

	m := &Manifest{
		ID:          manifestID,
		Connection:  opts.Connection,
		Database:    opts.Database,
		Message:     opts.Message,
		CreatedAt:   time.Now().Format(time.RFC3339),
		ParentID:    parentID,
		Collections: collections,
	}

	if err := saveManifest(scope, m); err != nil {
		return nil, err
	}

	summary := Summary{
		ID:         m.ID,
		Connection: m.Connection,
		Database:   m.Database,
		Message:    m.Message,
		CreatedAt:  m.CreatedAt,
		ParentID:   m.ParentID,
		DocCount:   m.DocCount(),
		NewObjects: newObjects,
	}
	// Re-read the index inside the lock (not just reuse the copy read
	// before the scan) and append to that fresh copy — another process
	// (or another goroutine, for the fs backend which lacks bbolt's
	// exclusive-open protection) could have published its own snapshot
	// while this one was scanning; publishing against a stale in-memory
	// index would silently discard that snapshot from index.json.
	if err := withScopeLock(scope, func() error {
		fresh, err := loadIndex(scope)
		if err != nil {
			return err
		}
		fresh.Snapshots = append(fresh.Snapshots, summary)
		return saveIndex(scope, fresh)
	}); err != nil {
		return nil, err
	}

	return &CreateResult{Summary: summary, Consistent: consistent}, nil
}

// LiveScan is the result of ScanLive: a Manifest plus each collection's
// sorted doc-ref list held as a bounded external-merge spill (never a full
// in-memory slice, never persisted to a Backend), so it can be diffed
// against a stored snapshot — even a very large one — without creating one.
// Callers must call Close() when done to remove the spills' temp files.
type LiveScan struct {
	Manifest *Manifest
	spills   map[string]*extSortSpill
}

// Source returns a docRefSource for Compare, backed by this scan's spills.
// Each call opens a fresh iterator, so Source() may be used more than once
// (e.g. if the same LiveScan were ever compared twice) without exhausting
// the underlying data.
func (s *LiveScan) Source() docRefSource {
	return func(collection string) (docRefIterator, error) {
		spill, ok := s.spills[collection]
		if !ok {
			return newSliceDocRefIterator(nil), nil
		}
		return spill.NewIterator()
	}
}

// Close removes every collection spill's temp files. Safe to call once the
// scan is no longer needed (e.g. after Compare() has consumed it).
func (s *LiveScan) Close() error {
	var firstErr error
	for _, spill := range s.spills {
		if err := spill.Cleanup(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ScanLive builds a snapshot of a database's current state without
// persisting anything, so it can be diffed against a stored snapshot
// without creating one. Doc-ref lists are held in bounded external-merge
// spills (see extsort.go), not full in-memory slices, so scanning a
// million-document collection for a live diff doesn't require holding the
// whole thing in RAM.
func ScanLive(uri, database string) (*LiveScan, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(context.Background())

	scanCtx, _, endSession := beginSnapshotRead(ctx, client)
	defer endSession()

	spills := make(map[string]*extSortSpill)
	onCollection := func(name string, spill *extSortSpill) error {
		spills[name] = spill
		return nil
	}

	collections, _, err := scanDatabase(ctx, scanCtx, client.Database(database), nil, onCollection)
	if err != nil {
		for _, spill := range spills {
			spill.Cleanup()
		}
		return nil, err
	}
	return &LiveScan{
		Manifest: &Manifest{
			Database:    database,
			Collections: collections,
			CreatedAt:   time.Now().Format(time.RFC3339),
		},
		spills: spills,
	}, nil
}

// beginSnapshotRead attempts to open a session with readConcern:snapshot so
// a multi-collection scan reads one consistent point in time rather than a
// rolling view. This requires the deployment to support transactions (a
// replica set — Atlas clusters qualify; a bare standalone mongod does not).
// When unsupported, it degrades to a plain scan on the original context
// (consistent=false) rather than failing the snapshot outright.
func beginSnapshotRead(ctx context.Context, client *mongo.Client) (scanCtx context.Context, consistent bool, end func()) {
	noop := func() {}

	// A standalone mongod accepts StartTransaction() client-side without
	// error — it only rejects the first real operation inside the session
	// with "Transaction numbers are only allowed on a replica set member or
	// mongos". Checking replica-set membership up front (via `hello`) avoids
	// that wasted round trip and lets us decide before touching any data.
	if !supportsTransactions(ctx, client) {
		return ctx, false, noop
	}

	sess, err := client.StartSession()
	if err != nil {
		return ctx, false, noop
	}
	sessCtx := mongo.NewSessionContext(ctx, sess)

	txnOpts := options.Transaction().SetReadConcern(readconcern.Snapshot())
	if err := sess.StartTransaction(txnOpts); err != nil {
		sess.EndSession(ctx)
		return ctx, false, noop
	}

	return sessCtx, true, func() {
		// A read-only "transaction": abort rather than commit, since nothing
		// was written and there's nothing to persist server-side.
		_ = sess.AbortTransaction(context.Background())
		sess.EndSession(context.Background())
	}
}

// supportsTransactions reports whether the deployment is a replica set (or
// mongos), which readConcern:snapshot transactions require.
func supportsTransactions(ctx context.Context, client *mongo.Client) bool {
	var result bson.M
	if err := client.Database("admin").RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&result); err != nil {
		return false
	}
	_, isReplicaSet := result["setName"]
	msg, isMongos := result["msg"]
	return isReplicaSet || (isMongos && msg == "isdbgrid")
}
