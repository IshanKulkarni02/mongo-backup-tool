package service

import (
	"context"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/snapshot"
)

// CreateSnapshot snapshots one database of a saved connection, dispatching
// on the engine's capabilities: SQL engines go through a session from m,
// document engines through the connection's URI.
func CreateSnapshot(ctx context.Context, m *engine.Manager, connection, database, message string) (*snapshot.CreateResult, error) {
	conn, err := ResolveConn(connection)
	if err != nil {
		return nil, err
	}
	eng, err := engine.Lookup(conn.EngineID())
	if err != nil {
		return nil, err
	}
	if eng.Capabilities().SQL {
		sess, release, err := SQLSessionFrom(ctx, m, connection)
		if err != nil {
			return nil, err
		}
		defer release()
		return snapshot.CreateSQL(ctx, snapshot.SQLCreateOptions{
			Connection: connection,
			Database:   database,
			Message:    message,
			Session:    sess,
		})
	}
	return snapshot.Create(snapshot.CreateOptions{
		Connection: connection,
		URI:        conn.URI,
		Database:   database,
		Message:    message,
	})
}

// RestoreOutcome is what an in-place restore reports.
type RestoreOutcome struct {
	Result           *snapshot.RestoreResult
	SafetySnapshotID string // the automatic pre-restore snapshot, the way to undo this restore
}

// RestoreSnapshot restores a snapshot over its own database in place. It
// refuses a read-only connection, takes a safety snapshot first, and rolls
// back automatically if the restore fails partway (snapshot.*WithSafety).
func RestoreSnapshot(ctx context.Context, m *engine.Manager, connection, database, snapshotID string) (*RestoreOutcome, error) {
	if err := RequireWritable(connection); err != nil {
		return nil, err
	}
	conn, err := ResolveConn(connection)
	if err != nil {
		return nil, err
	}
	eng, err := engine.Lookup(conn.EngineID())
	if err != nil {
		return nil, err
	}
	var (
		result *snapshot.RestoreResult
		safety *snapshot.CreateResult
	)
	if eng.Capabilities().SQL {
		sess, release, err := SQLSessionFrom(ctx, m, connection)
		if err != nil {
			return nil, err
		}
		defer release()
		result, safety, _, err = snapshot.RestoreSQLWithSafety(ctx, snapshot.SQLRestoreOptions{
			SourceConnection: connection,
			SourceDatabase:   database,
			SnapshotID:       snapshotID,
			Session:          sess,
			EngineID:         conn.EngineID(),
			Drop:             true,
		}, connection)
		if err != nil {
			return nil, err
		}
	} else {
		result, safety, _, err = snapshot.RestoreWithSafety(snapshot.RestoreOptions{
			SourceConnection: connection,
			SourceDatabase:   database,
			SnapshotID:       snapshotID,
			TargetURI:        conn.URI,
			Drop:             true,
		}, connection)
		if err != nil {
			return nil, err
		}
	}
	out := &RestoreOutcome{Result: result}
	if safety != nil {
		out.SafetySnapshotID = safety.Summary.ID
	}
	return out, nil
}
