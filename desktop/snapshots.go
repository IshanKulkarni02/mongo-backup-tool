package main

import (
	"fmt"
	"sort"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/snapshot"
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
	conn, err := a.resolveConn(connectionName)
	if err != nil {
		return "", err
	}
	return a.jobs.run("snapshot-create", func() (any, error) {
		res, err := snapshot.Create(snapshot.CreateOptions{
			Connection: connectionName,
			URI:        conn.URI,
			Database:   database,
			Message:    message,
		})
		if err != nil {
			return nil, err
		}
		return res, nil
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
func (a *App) DiffSnapshots(connectionName, database, fromID, toID string) (DiffSummaryResult, error) {
	diff, err := a.computeDiff(connectionName, database, fromID, toID)
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

// DiffCollectionChanges returns one page of changed IDs for one collection,
// recomputing the diff (Compare() completes in ~1s even at 1M documents, so
// this is simpler and fast enough without adding a server-side diff cache).
func (a *App) DiffCollectionChanges(connectionName, database, fromID, toID, collection, changeType string, offset, limit int) (DiffChangePage, error) {
	if limit <= 0 || limit > diffPageMaxLimit {
		limit = diffPageMaxLimit
	}
	diff, err := a.computeDiff(connectionName, database, fromID, toID)
	if err != nil {
		return DiffChangePage{}, err
	}
	cd := diff.Collections[collection]
	var all []string
	switch changeType {
	case "added":
		all = cd.Added
	case "modified":
		all = cd.Modified
	case "removed":
		all = cd.Removed
	default:
		return DiffChangePage{}, fmt.Errorf("unknown change type %q", changeType)
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	if offset > len(all) {
		offset = len(all)
	}
	return DiffChangePage{IDs: all[offset:end], Total: len(all), Offset: offset}, nil
}

func (a *App) computeDiff(connectionName, database, fromID, toID string) (snapshot.Diff, error) {
	from, err := snapshot.Get(connectionName, database, fromID)
	if err != nil {
		return snapshot.Diff{}, err
	}

	scope, err := snapshot.OpenScope(connectionName, database)
	if err != nil {
		return snapshot.Diff{}, err
	}
	defer scope.Close()

	if toID == "" {
		conn, err := a.resolveConn(connectionName)
		if err != nil {
			return snapshot.Diff{}, err
		}
		live, err := snapshot.ScanLive(conn.URI, database)
		if err != nil {
			return snapshot.Diff{}, err
		}
		return snapshot.Compare(from, scope.Source(from.ID), live.Manifest, live.Source())
	}

	to, err := snapshot.Get(connectionName, database, toID)
	if err != nil {
		return snapshot.Diff{}, err
	}
	return snapshot.Compare(from, scope.Source(from.ID), to, scope.Source(to.ID))
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
			AddedCount:    len(cd.Added),
			ModifiedCount: len(cd.Modified),
			RemovedCount:  len(cd.Removed),
		})
	}
	return out
}

// RestoreSnapshot starts an in-place, safety-snapshotted restore as a
// background job.
func (a *App) RestoreSnapshot(connectionName, database, snapshotID string) (string, error) {
	conn, err := a.resolveConn(connectionName)
	if err != nil {
		return "", err
	}
	return a.jobs.run("snapshot-restore", func() (any, error) {
		result, safety, err := snapshot.RestoreWithSafety(snapshot.RestoreOptions{
			SourceConnection: connectionName,
			SourceDatabase:   database,
			SnapshotID:       snapshotID,
			TargetURI:        conn.URI,
			Drop:             true,
		}, connectionName)
		if err != nil {
			return nil, err
		}
		return map[string]any{"result": result, "safetySnapshotId": safetyID(safety)}, nil
	}), nil
}

func safetyID(res *snapshot.CreateResult) string {
	if res == nil {
		return ""
	}
	return res.Summary.ID
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
