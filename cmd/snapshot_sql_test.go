package cmd

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/IshanKulkarni02/dbhelm/internal/snapshot"
)

// TestSnapshotCreateAndRestoreDispatchToSQLForSQLiteConnection exercises the
// engine-capability dispatch added to snapshotCreateCmd/snapshotRestoreCmd:
// a SQL connection (sqlite here, no Docker needed) must go through
// snapshot.CreateSQL/RestoreSQLWithSafety, not the Mongo-only
// snapshot.Create/RestoreWithSafety, which would fail against a non-mongodb
// URI.
func TestSnapshotCreateAndRestoreDispatchToSQLForSQLiteConnection(t *testing.T) {
	withTempConfigDir(t)
	resetConnAddFlags()

	dbPath := filepath.Join(t.TempDir(), "snap-cli-test.db")
	connAddURI = dbPath
	connAddEngine = "sqlite"
	if err := connectionAddCmd.RunE(connectionAddCmd, []string{"sqlite-cli-test"}); err != nil {
		t.Fatalf("connection add: %v", err)
	}

	conn, err := resolveConn("sqlite-cli-test")
	if err != nil {
		t.Fatalf("resolveConn: %v", err)
	}
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

	snapConn = "sqlite-cli-test"
	snapDB = "main"
	snapCreateMsg = "before change"
	if err := snapshotCreateCmd.RunE(snapshotCreateCmd, nil); err != nil {
		t.Fatalf("snapshot create: %v", err)
	}

	items, err := snapshot.Log("sqlite-cli-test", "main")
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

	snapRestoreID = snapID
	snapRestoreTargetConn = ""
	snapRestoreTargetDB = ""
	snapRestoreCollection = ""
	snapRestoreDrop = true
	if err := snapshotRestoreCmd.RunE(snapshotRestoreCmd, nil); err != nil {
		t.Fatalf("snapshot restore: %v", err)
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
