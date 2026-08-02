package snapshot

import (
	"context"
	"errors"
	"testing"
)

// TestIsAlreadyExistsError guards against #23: a blanket
// strings.Contains(msg, "duplicate") also matched unrelated errors where
// CREATE UNIQUE INDEX fails because existing row data violates the new
// constraint — a real data problem, not "index already exists" — and
// recreateSQLIndexes silently swallowed those too, leaving the restore
// reporting success with the index never recreated and the underlying
// duplicate-data problem hidden.
func TestIsAlreadyExistsError(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want bool
	}{
		{"postgres already exists", `pq: relation "idx_name" already exists`, true},
		{"sqlite already exists", `index idx_name already exists`, true},
		{"mysql already exists", `Error 1061 (42000): Duplicate key name 'idx_name'`, true},

		// Real data problems that must NOT be swallowed as "already exists".
		{"mysql duplicate row data", `Error 1062 (23000): Duplicate entry 'x' for key 'y'`, false},
		{"postgres duplicate row data", `pq: could not create unique index "idx_name" (SQLSTATE 23505): Key (col)=(x) is duplicated.`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isAlreadyExistsError(errors.New(c.msg)); got != c.want {
				t.Errorf("isAlreadyExistsError(%q) = %v, want %v", c.msg, got, c.want)
			}
		})
	}
}

// TestSQLStringLiteral guards against #16: MySQL treats \ as an escape
// character inside a single-quoted string literal by default, so a
// literal backslash in the value must itself be doubled there — but
// Postgres and SQLite treat \ as an ordinary character in a plain '...'
// literal, so escaping it for them would be incorrect, not just
// unnecessary.
func TestSQLStringLiteral(t *testing.T) {
	cases := []struct {
		name     string
		engineID string
		in       string
		want     string
	}{
		{"mysql backslash", "mysql", `C:\temp\new`, `'C:\\temp\\new'`},
		{"mysql single quote", "mysql", `it's`, `'it''s'`},
		{"mysql backslash and quote", "mysql", `a\b'c`, `'a\\b''c'`},
		{"mysql odd trailing backslash", "mysql", `a\`, `'a\\'`},
		{"postgres backslash left alone", "postgres", `C:\temp\new`, `'C:\temp\new'`},
		{"postgres single quote", "postgres", `it's`, `'it''s'`},
		{"sqlite backslash left alone", "sqlite", `C:\temp\new`, `'C:\temp\new'`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sqlStringLiteral(c.engineID, c.in); got != c.want {
				t.Errorf("sqlStringLiteral(%q, %q) = %q, want %q", c.engineID, c.in, got, c.want)
			}
		})
	}
}

// TestSQLLiteralForRestoreEscapesMySQLBackslash confirms
// sqlLiteralForRestore's string-value path actually reaches
// sqlStringLiteral's MySQL escaping, not just the helper in isolation.
func TestSQLLiteralForRestoreEscapesMySQLBackslash(t *testing.T) {
	got := sqlLiteralForRestore("mysql", `C:\temp\new`, false)
	want := `'C:\\temp\\new'`
	if got != want {
		t.Errorf("sqlLiteralForRestore(mysql, ...) = %q, want %q", got, want)
	}
}

// TestIsBinaryDataType guards against #15: information_schema.columns.
// data_type is reported lowercase for both Postgres ("bytea") and MySQL
// ("blob"/"binary"/"varbinary") — only the SQLite test schema happens to
// declare BLOB uppercase, so a plain-uppercase comparison here matched in
// tests but never matched a real Postgres/MySQL binary column, silently
// corrupting (MySQL) or outright failing (Postgres) their restore.
func TestIsBinaryDataType(t *testing.T) {
	cases := []struct {
		dbType string
		want   bool
	}{
		{"bytea", true}, // Postgres, as reported
		{"BYTEA", true},
		{"blob", true}, // MySQL, as reported
		{"BLOB", true},
		{"binary", true}, // MySQL, as reported
		{"BINARY", true},
		{"varbinary", true}, // MySQL, as reported
		{"VARBINARY", true},
		{"VarBinary", true},
		{"text", false},
		{"integer", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.dbType, func(t *testing.T) {
			if got := isBinaryDataType(c.dbType); got != c.want {
				t.Errorf("isBinaryDataType(%q) = %v, want %v", c.dbType, got, c.want)
			}
		})
	}
}

func TestRestoreSQLRoundTripBasic(t *testing.T) {
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
	s := openSQLiteTestSession(t)
	mustExecSQL(t, s, `CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)`)
	mustExecSQL(t, s, `INSERT INTO users (id, email) VALUES (1, 'a@example.com'), (2, 'b@example.com')`)

	snap, err := CreateSQL(context.Background(), SQLCreateOptions{Connection: "restore-basic", Database: "main", Session: s})
	if err != nil {
		t.Fatalf("CreateSQL: %v", err)
	}

	// Mutate the live data after the snapshot was taken.
	mustExecSQL(t, s, `DELETE FROM users WHERE id = 2`)
	mustExecSQL(t, s, `UPDATE users SET email = 'changed@example.com' WHERE id = 1`)
	mustExecSQL(t, s, `INSERT INTO users (id, email) VALUES (3, 'c@example.com')`)

	result, err := RestoreSQL(context.Background(), SQLRestoreOptions{
		SourceConnection: "restore-basic",
		SourceDatabase:   "main",
		SnapshotID:       snap.Summary.ID,
		Session:          s,
		EngineID:         "sqlite",
		Drop:             true,
	})
	if err != nil {
		t.Fatalf("RestoreSQL: %v", err)
	}
	if result.DocsWritten != 2 {
		t.Fatalf("expected 2 rows written, got %d", result.DocsWritten)
	}

	res, err := s.Query(context.Background(), "main", "SELECT id, email FROM users ORDER BY id")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("expected 2 rows after restore, got %d", len(res.Rows))
	}
	if res.Rows[0]["id"].Display != "1" || res.Rows[0]["email"].Display != "a@example.com" {
		t.Fatalf("row 0 not restored correctly: %+v", res.Rows[0])
	}
	if res.Rows[1]["id"].Display != "2" || res.Rows[1]["email"].Display != "b@example.com" {
		t.Fatalf("row 1 not restored correctly: %+v", res.Rows[1])
	}
}

func TestRestoreSQLBinaryDataRoundTrip(t *testing.T) {
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
	s := openSQLiteTestSession(t)
	mustExecSQL(t, s, `CREATE TABLE files (id INTEGER PRIMARY KEY, data BLOB)`)
	mustExecSQL(t, s, `INSERT INTO files (id, data) VALUES (1, X'00FF10AB')`)

	snap, err := CreateSQL(context.Background(), SQLCreateOptions{Connection: "restore-binary", Database: "main", Session: s})
	if err != nil {
		t.Fatalf("CreateSQL: %v", err)
	}

	mustExecSQL(t, s, `DELETE FROM files`)

	_, err = RestoreSQL(context.Background(), SQLRestoreOptions{
		SourceConnection: "restore-binary",
		SourceDatabase:   "main",
		SnapshotID:       snap.Summary.ID,
		Session:          s,
		EngineID:         "sqlite",
		Drop:             true,
	})
	if err != nil {
		t.Fatalf("RestoreSQL: %v", err)
	}

	tx, err := s.BeginConsistentRead(context.Background())
	if err != nil {
		t.Fatalf("BeginConsistentRead: %v", err)
	}
	defer tx.Close(context.Background())
	var gotBytes []byte
	err = tx.StreamRows(context.Background(), "main", "files", func(row map[string]any) error {
		if b, ok := row["data"].([]byte); ok {
			gotBytes = b
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamRows: %v", err)
	}
	want := []byte{0x00, 0xFF, 0x10, 0xAB}
	if string(gotBytes) != string(want) {
		t.Fatalf("expected binary data %v to round-trip exactly, got %v", want, gotBytes)
	}
}

func TestRestoreSQLCompositePrimaryKeyRoundTrip(t *testing.T) {
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
	s := openSQLiteTestSession(t)
	mustExecSQL(t, s, `CREATE TABLE membership (org_id INTEGER, user_id INTEGER, role TEXT, PRIMARY KEY (user_id, org_id))`)
	mustExecSQL(t, s, `INSERT INTO membership (org_id, user_id, role) VALUES (1, 1, 'admin'), (1, 2, 'member'), (2, 1, 'viewer')`)

	snap, err := CreateSQL(context.Background(), SQLCreateOptions{Connection: "restore-composite", Database: "main", Session: s})
	if err != nil {
		t.Fatalf("CreateSQL: %v", err)
	}

	mustExecSQL(t, s, `DELETE FROM membership`)

	result, err := RestoreSQL(context.Background(), SQLRestoreOptions{
		SourceConnection: "restore-composite",
		SourceDatabase:   "main",
		SnapshotID:       snap.Summary.ID,
		Session:          s,
		EngineID:         "sqlite",
		Drop:             true,
	})
	if err != nil {
		t.Fatalf("RestoreSQL: %v", err)
	}
	if result.DocsWritten != 3 {
		t.Fatalf("expected 3 rows written, got %d", result.DocsWritten)
	}

	res, err := s.Query(context.Background(), "main", "SELECT COUNT(*) as n FROM membership")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Rows[0]["n"].Display != "3" {
		t.Fatalf("expected 3 rows restored, got %s", res.Rows[0]["n"].Display)
	}
}

func TestRestoreSQLRecreatesIndexes(t *testing.T) {
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
	s := openSQLiteTestSession(t)
	mustExecSQL(t, s, `CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)`)
	mustExecSQL(t, s, `CREATE INDEX users_email_idx ON users (email)`)
	mustExecSQL(t, s, `INSERT INTO users (id, email) VALUES (1, 'a@example.com')`)

	snap, err := CreateSQL(context.Background(), SQLCreateOptions{Connection: "restore-idx", Database: "main", Session: s})
	if err != nil {
		t.Fatalf("CreateSQL: %v", err)
	}

	// Simulate the index having been dropped independently of the data.
	mustExecSQL(t, s, `DROP INDEX users_email_idx`)

	if _, err := RestoreSQL(context.Background(), SQLRestoreOptions{
		SourceConnection: "restore-idx",
		SourceDatabase:   "main",
		SnapshotID:       snap.Summary.ID,
		Session:          s,
		EngineID:         "sqlite",
		Drop:             true,
	}); err != nil {
		t.Fatalf("RestoreSQL: %v", err)
	}

	indexes, err := s.ListTableIndexes(context.Background(), "main", "users")
	if err != nil {
		t.Fatalf("ListTableIndexes: %v", err)
	}
	if len(indexes) != 1 || indexes[0].Name != "users_email_idx" {
		t.Fatalf("expected users_email_idx to be recreated, got %+v", indexes)
	}

	// Restoring again (index already present this time) must not error —
	// "already exists" is tolerated, not fatal.
	if _, err := RestoreSQL(context.Background(), SQLRestoreOptions{
		SourceConnection: "restore-idx",
		SourceDatabase:   "main",
		SnapshotID:       snap.Summary.ID,
		Session:          s,
		EngineID:         "sqlite",
		Drop:             true,
	}); err != nil {
		t.Fatalf("expected second restore (index already present) to succeed, got: %v", err)
	}
}

func TestRestoreSQLErrorsIfTargetTableMissing(t *testing.T) {
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
	s := openSQLiteTestSession(t)
	mustExecSQL(t, s, `CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)`)
	mustExecSQL(t, s, `INSERT INTO users (id, email) VALUES (1, 'a@example.com')`)

	snap, err := CreateSQL(context.Background(), SQLCreateOptions{Connection: "restore-missing", Database: "main", Session: s})
	if err != nil {
		t.Fatalf("CreateSQL: %v", err)
	}
	mustExecSQL(t, s, `DROP TABLE users`)

	_, err = RestoreSQL(context.Background(), SQLRestoreOptions{
		SourceConnection: "restore-missing",
		SourceDatabase:   "main",
		SnapshotID:       snap.Summary.ID,
		Session:          s,
		EngineID:         "sqlite",
		Drop:             true,
	})
	if err == nil {
		t.Fatal("expected an error when the target table doesn't exist")
	}
}

func TestRestoreSQLWithSafetyRollsBackOnFailure(t *testing.T) {
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
	s := openSQLiteTestSession(t)
	mustExecSQL(t, s, `CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)`)
	mustExecSQL(t, s, `CREATE TABLE zz_orders (id INTEGER PRIMARY KEY, note TEXT)`)
	mustExecSQL(t, s, `INSERT INTO users (id, email) VALUES (1, 'original@example.com')`)
	mustExecSQL(t, s, `INSERT INTO zz_orders (id, note) VALUES (1, 'original-order')`)

	snap, err := CreateSQL(context.Background(), SQLCreateOptions{Connection: "restore-safety", Database: "main", Session: s})
	if err != nil {
		t.Fatalf("CreateSQL: %v", err)
	}

	mustExecSQL(t, s, `UPDATE users SET email = 'changed@example.com' WHERE id = 1`)
	// Drop "zz_orders" so restoring it fails partway through a 2-table
	// restore, after "users" (alphabetically first) already succeeded —
	// RestoreSQLWithSafety should detect the partial damage and roll
	// "users" back to its pre-restore (safety-snapshotted) state.
	mustExecSQL(t, s, `DROP TABLE zz_orders`)

	result, safety, rolledBack, err := RestoreSQLWithSafety(context.Background(), SQLRestoreOptions{
		SourceConnection: "restore-safety",
		SourceDatabase:   "main",
		SnapshotID:       snap.Summary.ID,
		Session:          s,
		EngineID:         "sqlite",
		Drop:             true,
	}, "restore-safety")
	if err == nil {
		t.Fatal("expected an error since zz_orders no longer exists")
	}
	if safety == nil {
		t.Fatal("expected a safety snapshot to have been taken")
	}
	if !rolledBack {
		t.Fatalf("expected automatic rollback to have happened, result=%+v", result)
	}

	res, qerr := s.Query(context.Background(), "main", "SELECT email FROM users WHERE id = 1")
	if qerr != nil {
		t.Fatalf("Query: %v", qerr)
	}
	if len(res.Rows) != 1 || res.Rows[0]["email"].Display != "changed@example.com" {
		t.Fatalf("expected users to be rolled back to its pre-restore (changed) state, got %+v", res.Rows)
	}
}
