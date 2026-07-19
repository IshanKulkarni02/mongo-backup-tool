package snapshot

import (
	"context"
	"testing"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	_ "github.com/IshanKulkarni02/dbhelm/internal/engine/sqlite"
)

func openSQLiteTestSession(t *testing.T) engine.SQLSession {
	t.Helper()
	eng, err := engine.Lookup("sqlite")
	if err != nil {
		t.Fatalf("engine.Lookup(sqlite): %v", err)
	}
	sess, err := eng.Open(context.Background(), engine.ConnConfig{URI: "file::memory:?cache=private"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { sess.Close(context.Background()) })
	return sess.(engine.SQLSession)
}

func mustExecSQL(t *testing.T, s engine.SQLSession, sqlText string) {
	t.Helper()
	if _, err := s.Execute(context.Background(), "main", sqlText); err != nil {
		t.Fatalf("exec %q: %v", sqlText, err)
	}
}

func TestCreateSQLScansTablesAndSkipsNoPKTables(t *testing.T) {
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
	s := openSQLiteTestSession(t)

	mustExecSQL(t, s, `CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)`)
	mustExecSQL(t, s, `INSERT INTO users (email) VALUES ('a@example.com'), ('b@example.com')`)
	mustExecSQL(t, s, `CREATE INDEX users_email_idx ON users (email)`)

	mustExecSQL(t, s, `CREATE TABLE membership (org_id INTEGER, user_id INTEGER, role TEXT, PRIMARY KEY (user_id, org_id))`)
	mustExecSQL(t, s, `INSERT INTO membership (org_id, user_id, role) VALUES (1, 1, 'admin'), (1, 2, 'member')`)

	mustExecSQL(t, s, `CREATE TABLE files (id INTEGER PRIMARY KEY, data BLOB)`)
	mustExecSQL(t, s, `INSERT INTO files (data) VALUES (X'00FF10')`)

	mustExecSQL(t, s, `CREATE TABLE logs (message TEXT)`) // no primary key — must be skipped

	result, err := CreateSQL(context.Background(), SQLCreateOptions{
		Connection: "sqlite-test",
		Database:   "main",
		Message:    "initial",
		Session:    s,
	})
	if err != nil {
		t.Fatalf("CreateSQL: %v", err)
	}
	if !result.Consistent {
		t.Fatal("expected SQL snapshot to always be Consistent")
	}
	if len(result.SkippedTables) != 1 || result.SkippedTables[0] != "logs" {
		t.Fatalf("expected logs to be the only skipped table, got %v", result.SkippedTables)
	}

	m, err := Get("sqlite-test", "main", result.Summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, ok := m.Collections["logs"]; ok {
		t.Fatal("expected logs to be absent from the manifest, not just empty")
	}

	users, ok := m.Collections["users"]
	if !ok {
		t.Fatal("expected users in manifest")
	}
	if users.DocCount != 2 {
		t.Fatalf("expected 2 rows in users, got %d", users.DocCount)
	}
	if len(users.PrimaryKey) != 1 || users.PrimaryKey[0] != "id" {
		t.Fatalf("expected users PrimaryKey [id], got %v", users.PrimaryKey)
	}
	if len(users.IndexDDL) != 1 {
		t.Fatalf("expected 1 captured index DDL for users, got %v", users.IndexDDL)
	}

	membership, ok := m.Collections["membership"]
	if !ok {
		t.Fatal("expected membership in manifest")
	}
	if len(membership.PrimaryKey) != 2 || membership.PrimaryKey[0] != "user_id" || membership.PrimaryKey[1] != "org_id" {
		t.Fatalf("expected composite PrimaryKey [user_id org_id], got %v", membership.PrimaryKey)
	}
	if membership.DocCount != 2 {
		t.Fatalf("expected 2 rows in membership, got %d", membership.DocCount)
	}

	files, ok := m.Collections["files"]
	if !ok || files.DocCount != 1 {
		t.Fatalf("expected 1 row in files, got %+v", files)
	}
}

func TestCreateSQLDedupesIdenticalContentAcrossSnapshots(t *testing.T) {
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
	s := openSQLiteTestSession(t)
	mustExecSQL(t, s, `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)
	mustExecSQL(t, s, `INSERT INTO t (v) VALUES ('a'), ('b'), ('c')`)

	first, err := CreateSQL(context.Background(), SQLCreateOptions{Connection: "dedup-test", Database: "main", Session: s})
	if err != nil {
		t.Fatalf("first CreateSQL: %v", err)
	}
	if first.Summary.NewObjects != 3 {
		t.Fatalf("expected 3 new objects on first snapshot, got %d", first.Summary.NewObjects)
	}

	// No data changed — a second snapshot should dedupe against the first
	// snapshot's stored content and write zero new objects.
	second, err := CreateSQL(context.Background(), SQLCreateOptions{Connection: "dedup-test", Database: "main", Session: s})
	if err != nil {
		t.Fatalf("second CreateSQL: %v", err)
	}
	if second.Summary.NewObjects != 0 {
		t.Fatalf("expected 0 new objects on identical second snapshot, got %d", second.Summary.NewObjects)
	}
	if second.Summary.ParentID != first.Summary.ID {
		t.Fatalf("expected second snapshot's ParentID to be the first snapshot's ID")
	}
}

func TestCreateSQLTableWithNoRowsIsStillCaptured(t *testing.T) {
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
	s := openSQLiteTestSession(t)
	mustExecSQL(t, s, `CREATE TABLE empty_table (id INTEGER PRIMARY KEY)`)

	result, err := CreateSQL(context.Background(), SQLCreateOptions{Connection: "empty-test", Database: "main", Session: s})
	if err != nil {
		t.Fatalf("CreateSQL: %v", err)
	}
	m, err := Get("empty-test", "main", result.Summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	et, ok := m.Collections["empty_table"]
	if !ok {
		t.Fatal("expected empty_table to be present in the manifest even with 0 rows")
	}
	if et.DocCount != 0 {
		t.Fatalf("expected 0 rows, got %d", et.DocCount)
	}
}
