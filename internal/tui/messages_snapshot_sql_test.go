package tui

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	_ "github.com/IshanKulkarni02/dbhelm/internal/engine/sqlite"
	"github.com/IshanKulkarni02/dbhelm/internal/snapshot"
)

// TestCreateAndRestoreSnapshotCmdDispatchToSQLForSQLiteConnection exercises
// the engine-capability dispatch added to createSnapshotCmd/restoreSnapshotCmd:
// a SQL connection (sqlite here, no Docker needed) must go through
// snapshot.CreateSQL/RestoreSQLWithSafety, not the Mongo-only
// snapshot.Create/RestoreWithSafety, which would fail against a non-mongodb
// URI.
func TestCreateAndRestoreSnapshotCmdDispatchToSQLForSQLiteConnection(t *testing.T) {
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())

	// A real file, not :memory: — openSQLSession has no session cache (the
	// TUI has no engine.Manager, unlike desktop), so each call below opens
	// (and its release closes) a brand-new connection; an in-memory DSN
	// would lose all data between calls.
	dbPath := filepath.Join(t.TempDir(), "tui-snap-test.db")
	conn := config.Connection{Name: "sqlite-tui-test", URI: dbPath, Engine: "sqlite"}

	sess, release, err := openSQLSession(conn)
	if err != nil {
		t.Fatalf("openSQLSession: %v", err)
	}
	if _, err := sess.Execute(context.Background(), "main", `CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	if _, err := sess.Execute(context.Background(), "main", `INSERT INTO users (id, email) VALUES (1, 'original@example.com')`); err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	release()

	msg := createSnapshotCmd(conn, "main", "before change")()
	done, ok := msg.(actionDoneMsg)
	if !ok {
		t.Fatalf("expected actionDoneMsg, got %T", msg)
	}
	if done.err != nil {
		t.Fatalf("createSnapshotCmd: %v", done.err)
	}

	items, err := snapshot.Log("sqlite-tui-test", "main")
	if err != nil {
		t.Fatalf("listing snapshots: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected exactly 1 snapshot, got %d", len(items))
	}
	snapID := items[0].ID

	sess, release, err = openSQLSession(conn)
	if err != nil {
		t.Fatalf("openSQLSession: %v", err)
	}
	if _, err := sess.Execute(context.Background(), "main", `UPDATE users SET email = 'changed@example.com' WHERE id = 1`); err != nil {
		t.Fatalf("UPDATE: %v", err)
	}
	release()

	msg = restoreSnapshotCmd(conn, "main", snapID)()
	done, ok = msg.(actionDoneMsg)
	if !ok {
		t.Fatalf("expected actionDoneMsg, got %T", msg)
	}
	if done.err != nil {
		t.Fatalf("restoreSnapshotCmd: %v", done.err)
	}

	sess, release, err = openSQLSession(conn)
	if err != nil {
		t.Fatalf("openSQLSession: %v", err)
	}
	defer release()
	res, err := sess.Query(context.Background(), "main", "SELECT email FROM users WHERE id = 1")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Rows) != 1 || res.Rows[0]["email"].Display != "original@example.com" {
		t.Fatalf("expected the restored email to be the original, got %+v", res.Rows)
	}
}

// TestDiffLiveCmdRejectsSQLConnections confirms diffLiveCmd fails clearly
// for a SQL connection rather than letting ScanLive attempt (and fail
// cryptically on) a mongo.Connect against a non-mongodb URI.
func TestDiffLiveCmdRejectsSQLConnections(t *testing.T) {
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
	dbPath := filepath.Join(t.TempDir(), "tui-diff-test.db")
	conn := config.Connection{Name: "sqlite-tui-diff-test", URI: dbPath, Engine: "sqlite"}

	msg := diffLiveCmd(conn, "main", "some-snapshot-id")()
	done, ok := msg.(actionDoneMsg)
	if !ok {
		t.Fatalf("expected actionDoneMsg, got %T", msg)
	}
	if done.err == nil {
		t.Fatal("expected an error for diffing a SQL connection against the live database")
	}
}
