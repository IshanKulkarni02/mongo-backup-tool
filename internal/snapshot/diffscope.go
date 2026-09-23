package snapshot

import (
	"context"
	"fmt"
)

// DiffScope holds everything one diff needs open: the "from" snapshot, the
// snapshot backend, and either the "to" snapshot or a live scan of the
// database (exactly one of To and live is set). Callers must Close it.
type DiffScope struct {
	From  *Manifest
	To    *Manifest // nil when diffing against the live database
	scope *Scope
	live  *LiveScan
}

// OpenDiff resolves the "from" snapshot and either the "to" snapshot or, when
// toID is empty, a live scan of the database at liveURI (Mongo only: the
// caller must refuse SQL connections before asking for a live diff).
func OpenDiff(connection, database, fromID, toID, liveURI string) (*DiffScope, error) {
	from, err := Get(connection, database, fromID)
	if err != nil {
		return nil, err
	}
	scope, err := OpenScope(connection, database)
	if err != nil {
		return nil, err
	}
	d := &DiffScope{From: from, scope: scope}
	if toID == "" {
		if liveURI == "" {
			scope.Close()
			return nil, fmt.Errorf("a live diff needs the connection's URI")
		}
		if d.live, err = ScanLive(liveURI, database); err != nil {
			scope.Close()
			return nil, err
		}
		return d, nil
	}
	if d.To, err = Get(connection, database, toID); err != nil {
		scope.Close()
		return nil, err
	}
	return d, nil
}

// Close releases the backend and any live-scan spill files.
func (d *DiffScope) Close() {
	if d.live != nil {
		d.live.Close()
	}
	d.scope.Close()
}

func (d *DiffScope) toSource() docRefSource {
	if d.live != nil {
		return d.live.Source()
	}
	return d.scope.Source(d.To.ID)
}

// Compare returns per-collection change counts. It never materializes a
// changed-ID list, so memory stays bounded however much changed.
func (d *DiffScope) Compare(ctx context.Context) (Diff, error) {
	to := d.To
	if d.live != nil {
		to = d.live.Manifest
	}
	return Compare(ctx, d.From, d.scope.Source(d.From.ID), to, d.toSource())
}

// CollectionPage returns one page of one collection's changed IDs for one
// change type (see DiffCollectionPage).
func (d *DiffScope) CollectionPage(ctx context.Context, collection string, ct ChangeType, offset, limit int) ([]string, int, error) {
	return DiffCollectionPage(ctx, d.scope.Source(d.From.ID), d.toSource(), collection, ct, offset, limit)
}
