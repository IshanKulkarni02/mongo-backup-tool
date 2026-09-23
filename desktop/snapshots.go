package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/service"
	"github.com/IshanKulkarni02/dbhelm/internal/snapshot"
)

// ListSnapshots returns a connection+database's snapshot history, newest first.
func (a *App) ListSnapshots(connection, database string) ([]snapshot.Summary, error) {
	items, err := snapshot.Log(connection, database)
	if err != nil {
		return nil, err
	}
	// snapshot.Log returns oldest-first; the timeline view wants newest-first.
	out := make([]snapshot.Summary, len(items))
	for i, s := range items {
		out[len(items)-1-i] = s
	}
	return out, nil
}

// CreateSnapshot starts a snapshot as a background job and returns its job ID.
func (a *App) CreateSnapshot(connectionName, database, message string) (string, error) {
	if _, err := a.resolveConn(connectionName); err != nil {
		return "", err
	}
	return a.jobs.run("snapshot-create", func() (any, error) {
		return service.CreateSnapshot(context.Background(), a.engines, connectionName, database, message)
	}), nil
}

// CollectionDiffSummary is one collection's change counts — cheap to send
// over IPC regardless of how many documents actually changed.
type CollectionDiffSummary struct {
	Name          string `json:"name"`
	AddedCount    int    `json:"addedCount"`
	ModifiedCount int    `json:"modifiedCount"`
	RemovedCount  int    `json:"removedCount"`
}

// DiffSummaryResult is what DiffSnapshots returns: per-collection counts
// only. The full list of changed document IDs for one collection is fetched
// separately, paginated, via DiffCollectionChanges — a diff between two
// large snapshots can have tens of thousands of changed IDs (proven at
// 1M-document scale during Phase 1's load test), and that must never cross
// the IPC bridge in a single unbounded array.
type DiffSummaryResult struct {
	FromID      string                  `json:"fromId"`
	ToID        string                  `json:"toId"` // empty when diffed against the live database
	Collections []CollectionDiffSummary `json:"collections"`
}

// DiffSnapshots compares two snapshots, or a snapshot against the live
// database when toID is empty, returning per-collection change counts.
// Compare never materializes a changed-ID list (diff.go), so this stays
// bounded in memory even for very large, heavily-changed databases.
func (a *App) DiffSnapshots(connectionName, database, fromID, toID string) (DiffSummaryResult, error) {
	d, err := a.openDiff(connectionName, database, fromID, toID)
	if err != nil {
		return DiffSummaryResult{}, err
	}
	defer d.Close()
	diff, err := d.Compare(context.Background())
	if err != nil {
		return DiffSummaryResult{}, err
	}
	return summarizeDiff(diff), nil
}

// DiffChangePage is one page of a single collection's changed document IDs
// for a specific change type ("added", "modified", or "removed").
type DiffChangePage struct {
	IDs    []string `json:"ids"`
	Total  int      `json:"total"`
	Offset int      `json:"offset"`
}

const diffPageMaxLimit = 500

// DiffCollectionChanges returns one page of changed IDs for one collection
// and change type via DiffCollectionPage, which re-runs just that one
// collection's merge-join and never materializes the full matched-ID list —
// memory is bounded by limit (capped at diffPageMaxLimit), not by how many
// documents changed, unlike computing and slicing a full Diff per page.
func (a *App) DiffCollectionChanges(connectionName, database, fromID, toID, collection, changeType string, offset, limit int) (DiffChangePage, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > diffPageMaxLimit {
		limit = diffPageMaxLimit
	}
	var ct snapshot.ChangeType
	switch changeType {
	case "added":
		ct = snapshot.Added
	case "modified":
		ct = snapshot.Modified
	case "removed":
		ct = snapshot.Removed
	default:
		return DiffChangePage{}, fmt.Errorf("unknown change type %q", changeType)
	}

	d, err := a.openDiff(connectionName, database, fromID, toID)
	if err != nil {
		return DiffChangePage{}, err
	}
	defer d.Close()
	ids, total, err := d.CollectionPage(context.Background(), collection, ct, offset, limit)
	if err != nil {
		return DiffChangePage{}, err
	}
	if ids == nil {
		ids = []string{}
	}
	return DiffChangePage{IDs: ids, Total: total, Offset: offset}, nil
}

// openDiff opens a diff between two snapshots, or a snapshot and the live
// database when toID is empty (Mongo only: a SQL live diff isn't implemented,
// so it fails clearly here rather than let ScanLive's mongo.Connect error on
// the URI scheme mismatch).
func (a *App) openDiff(connectionName, database, fromID, toID string) (*snapshot.DiffScope, error) {
	liveURI := ""
	if toID == "" {
		conn, err := a.resolveConn(connectionName)
		if err != nil {
			return nil, err
		}
		if eng, eerr := engine.Lookup(conn.EngineID()); eerr == nil && eng.Capabilities().SQL {
			return nil, fmt.Errorf("comparing against the live database isn't supported for SQL connections yet — compare two snapshots instead")
		}
		liveURI = conn.URI
	}
	return snapshot.OpenDiff(connectionName, database, fromID, toID, liveURI)
}

func summarizeDiff(diff snapshot.Diff) DiffSummaryResult {
	names := make([]string, 0, len(diff.Collections))
	for name := range diff.Collections {
		names = append(names, name)
	}
	sort.Strings(names)

	// Collections must always marshal to [] rather than JSON null (Go's zero
	// value for a nil slice) — the frontend calls .length on it unconditionally.
	out := DiffSummaryResult{FromID: diff.FromID, ToID: diff.ToID, Collections: []CollectionDiffSummary{}}
	for _, name := range names {
		cd := diff.Collections[name]
		out.Collections = append(out.Collections, CollectionDiffSummary{
			Name:          name,
			AddedCount:    cd.AddedCount,
			ModifiedCount: cd.ModifiedCount,
			RemovedCount:  cd.RemovedCount,
		})
	}
	return out
}

// RestoreSnapshot starts an in-place, safety-snapshotted restore as a
// background job. The restore's error message already says whether it
// auto-rolled back, so it is returned straight through.
func (a *App) RestoreSnapshot(connectionName, database, snapshotID string) (string, error) {
	if _, err := a.resolveConn(connectionName); err != nil {
		return "", err
	}
	return a.jobs.run("snapshot-restore", func() (any, error) {
		out, err := service.RestoreSnapshot(context.Background(), a.engines, connectionName, database, snapshotID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"result": out.Result, "safetySnapshotId": out.SafetySnapshotID}, nil
	}), nil
}

// TagSnapshot labels a snapshot, protecting it from gc.
func (a *App) TagSnapshot(connectionName, database, snapshotID, tag string) error {
	if tag == "" {
		return fmt.Errorf("a tag is required")
	}
	return snapshot.Tag(connectionName, database, snapshotID, tag)
}

// GCSnapshots prunes old untagged snapshots and reclaims their storage.
func (a *App) GCSnapshots(connectionName, database string, keepLast int) (*snapshot.GCResult, error) {
	return snapshot.GC(snapshot.GCOptions{Connection: connectionName, Database: database, KeepLast: keepLast})
}
