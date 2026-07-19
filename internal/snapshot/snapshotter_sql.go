package snapshot

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

// SQLCreateOptions configures a new SQL snapshot. Session is an
// already-resolved engine.SQLSession — the caller acquires it (typically
// via engine.Manager), so internal/snapshot doesn't need to know how
// connections are resolved, only how to scan one. This mirrors
// CreateOptions' shape but takes a live session instead of a raw URI,
// since (unlike Mongo's snapshotter, which predates the engine
// abstraction and still connects directly) this path is built on top of
// internal/engine from the start.
type SQLCreateOptions struct {
	Connection string
	Database   string
	Message    string
	Session    engine.SQLSession
	Backend    BackendKind
}

// CreateSQL is Create's SQL-engine counterpart. The consistency guarantee
// comes from engine.SQLSession.BeginConsistentRead (a real REPEATABLE
// READ/snapshot-isolated transaction per table — see internal/engine's
// per-dialect implementations), not Mongo's readConcern:snapshot
// transaction, so CreateResult.Consistent is always true for a successful
// SQL scan — there's no non-transactional degraded path the way a
// non-replica-set Mongo deployment has.
func CreateSQL(ctx context.Context, opts SQLCreateOptions) (*CreateResult, error) {
	scope, err := scopeDir(opts.Connection, opts.Database)
	if err != nil {
		return nil, err
	}
	backend, err := OpenBackend(scope, opts.Backend)
	if err != nil {
		return nil, err
	}
	defer backend.Close()

	idx, err := loadIndex(scope)
	if err != nil {
		return nil, err
	}
	parentID := ""
	if latest, ok := idx.Latest(); ok {
		parentID = latest.ID
	}

	// Generated up front, same reasoning as Create: each table's doc refs
	// can be written to the backend as soon as that table finishes
	// scanning, never holding more than one table's bounded spill in
	// memory at once.
	manifestID := uuid.NewString()

	var writeErr error
	onTable := func(name string, cm CollectionManifest, spill *extSortSpill) error {
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

	tables, newObjects, skipped, err := scanSQLDatabase(ctx, opts.Session, opts.Database, backend, onTable)
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
		Collections: tables,
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
	// Re-read the index inside the lock, same reasoning as Create: another
	// process/goroutine could have published its own snapshot while this
	// one was scanning.
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

	return &CreateResult{Summary: summary, Consistent: true, SkippedTables: skipped}, nil
}
