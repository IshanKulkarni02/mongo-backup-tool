package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

func openTestSession(t *testing.T) *Session {
	t.Helper()
	eng := Engine{}
	// A unique in-memory database per test (shared cache would leak state
	// across tests using the same DSN).
	sess, err := eng.Open(context.Background(), engine.ConnConfig{URI: "file::memory:?cache=private&_pragma=foreign_keys(1)"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s := sess.(*Session)
	t.Cleanup(func() { s.Close(context.Background()) })
	return s
}

func mustExec(t *testing.T, s *Session, sqlText string) {
	t.Helper()
	if _, err := s.Execute(context.Background(), "main", sqlText); err != nil {
		t.Fatalf("exec %q: %v", sqlText, err)
	}
}

func TestSQLiteListNamespacesAndSchema(t *testing.T) {
	s := openTestSession(t)
	mustExec(t, s, `CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL)`)
	mustExec(t, s, `CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER, FOREIGN KEY(user_id) REFERENCES users(id))`)
	mustExec(t, s, `INSERT INTO users (email) VALUES ('a@example.com'), ('b@example.com')`)

	ns, err := s.ListNamespaces(context.Background(), "main")
	if err != nil {
		t.Fatalf("ListNamespaces: %v", err)
	}
	names := map[string]engine.NamespaceInfo{}
	for _, n := range ns {
		names[n.Name] = n
	}
	if names["users"].DocCount != 2 {
		t.Fatalf("expected 2 rows in users, got %d", names["users"].DocCount)
	}
	if _, ok := names["orders"]; !ok {
		t.Fatal("expected orders table in namespace list")
	}

	schema, err := s.TableSchema(context.Background(), "main", "orders")
	if err != nil {
		t.Fatalf("TableSchema: %v", err)
	}
	if len(schema.ForeignKeys) != 1 {
		t.Fatalf("expected 1 FK on orders, got %d", len(schema.ForeignKeys))
	}
	fk := schema.ForeignKeys[0]
	if fk.Column != "user_id" || fk.RefTable != "users" || fk.RefColumn != "id" {
		t.Fatalf("unexpected FK: %+v", fk)
	}

	userSchema, err := s.TableSchema(context.Background(), "main", "users")
	if err != nil {
		t.Fatalf("TableSchema(users): %v", err)
	}
	var pkFound bool
	for _, c := range userSchema.Columns {
		if c.Name == "id" && c.IsPK {
			pkFound = true
		}
	}
	if !pkFound {
		t.Fatal("expected id column to be marked as primary key")
	}
}

func TestSQLiteQueryReturnsTypedCells(t *testing.T) {
	s := openTestSession(t)
	mustExec(t, s, `CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT, price REAL, active INTEGER)`)
	mustExec(t, s, `INSERT INTO items (name, price, active) VALUES ('widget', 9.99, 1)`)

	res, err := s.Query(context.Background(), "main", "SELECT id, name, price, active FROM items")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Total != 1 || len(res.Rows) != 1 {
		t.Fatalf("expected 1 row, got total=%d rows=%d", res.Total, len(res.Rows))
	}
	row := res.Rows[0]
	if row["name"].Type != engine.CellString || row["name"].Display != "widget" {
		t.Fatalf("unexpected name cell: %+v", row["name"])
	}
	if row["id"].Type != engine.CellNumber {
		t.Fatalf("expected numeric id cell, got %+v", row["id"])
	}
}

func TestSQLiteExecuteReportsRowsAffected(t *testing.T) {
	s := openTestSession(t)
	mustExec(t, s, `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)
	mustExec(t, s, `INSERT INTO t (v) VALUES ('a'), ('b'), ('c')`)

	n, err := s.Execute(context.Background(), "main", `DELETE FROM t WHERE v != 'a'`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 rows affected, got %d", n)
	}
}

func TestSQLiteExplainReturnsPlanText(t *testing.T) {
	s := openTestSession(t)
	mustExec(t, s, `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)

	plan, err := s.Explain(context.Background(), "main", "SELECT * FROM t WHERE v = 'x'")
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if strings.TrimSpace(plan) == "" {
		t.Fatal("expected non-empty explain output")
	}
}

func TestSQLiteQueryRespectsRowCap(t *testing.T) {
	s := openTestSession(t)
	mustExec(t, s, `CREATE TABLE t (id INTEGER PRIMARY KEY)`)
	for i := 0; i < 10; i++ {
		mustExec(t, s, "INSERT INTO t DEFAULT VALUES")
	}
	res, err := s.Query(context.Background(), "main", "SELECT id FROM t")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Rows) != 10 {
		t.Fatalf("expected all 10 rows under the cap, got %d", len(res.Rows))
	}
}

func TestSQLiteTableSchemaCompositePrimaryKeyOrder(t *testing.T) {
	s := openTestSession(t)
	mustExec(t, s, `CREATE TABLE membership (org_id INTEGER, user_id INTEGER, role TEXT, PRIMARY KEY (user_id, org_id))`)

	schema, err := s.TableSchema(context.Background(), "main", "membership")
	if err != nil {
		t.Fatalf("TableSchema: %v", err)
	}
	want := []string{"user_id", "org_id"}
	if len(schema.PrimaryKey) != len(want) {
		t.Fatalf("expected PrimaryKey %v, got %v", want, schema.PrimaryKey)
	}
	for i, col := range want {
		if schema.PrimaryKey[i] != col {
			t.Fatalf("expected PrimaryKey %v, got %v", want, schema.PrimaryKey)
		}
	}
	for _, c := range schema.Columns {
		wantPK := c.Name == "user_id" || c.Name == "org_id"
		if c.IsPK != wantPK {
			t.Fatalf("column %s: IsPK=%v, want %v", c.Name, c.IsPK, wantPK)
		}
	}
}

func TestSQLiteTableSchemaNoPrimaryKey(t *testing.T) {
	s := openTestSession(t)
	mustExec(t, s, `CREATE TABLE logs (message TEXT)`)
	schema, err := s.TableSchema(context.Background(), "main", "logs")
	if err != nil {
		t.Fatalf("TableSchema: %v", err)
	}
	if len(schema.PrimaryKey) != 0 {
		t.Fatalf("expected no primary key, got %v", schema.PrimaryKey)
	}
}

func TestSQLiteListTableIndexesExcludesConstraintBackedIndexes(t *testing.T) {
	s := openTestSession(t)
	mustExec(t, s, `CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT UNIQUE)`)
	mustExec(t, s, `CREATE INDEX idx_users_email_lower ON users (email)`)

	indexes, err := s.ListTableIndexes(context.Background(), "main", "users")
	if err != nil {
		t.Fatalf("ListTableIndexes: %v", err)
	}
	if len(indexes) != 1 {
		t.Fatalf("expected exactly 1 explicit index (constraint-backed ones excluded), got %+v", indexes)
	}
	if indexes[0].Name != "idx_users_email_lower" {
		t.Fatalf("unexpected index: %+v", indexes[0])
	}
	if !strings.Contains(indexes[0].DDL, "idx_users_email_lower") {
		t.Fatalf("expected DDL to reference the index name, got %q", indexes[0].DDL)
	}
}

func TestSQLiteBeginConsistentReadStreamsAllRowsIncludingBinary(t *testing.T) {
	s := openTestSession(t)
	mustExec(t, s, `CREATE TABLE files (id INTEGER PRIMARY KEY, name TEXT, data BLOB)`)
	blob := []byte{0x00, 0x01, 0xFF, 0xFE, 'h', 'i'}
	for i := 0; i < 600; i++ { // exceeds sqlbase.QueryRowCap, unlike Query
		_, err := s.Execute(context.Background(), "main", "INSERT INTO files (name, data) VALUES ('f', X'"+bytesToHex(blob)+"')")
		if err != nil {
			t.Fatalf("seeding row %d: %v", i, err)
		}
	}

	tx, err := s.BeginConsistentRead(context.Background())
	if err != nil {
		t.Fatalf("BeginConsistentRead: %v", err)
	}
	defer tx.Close(context.Background())

	var rowCount int
	var sawBinary bool
	err = tx.StreamRows(context.Background(), "main", "files", func(row map[string]any) error {
		rowCount++
		if b, ok := row["data"].([]byte); ok {
			if string(b) == string(blob) {
				sawBinary = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamRows: %v", err)
	}
	if rowCount != 600 {
		t.Fatalf("expected all 600 rows streamed (no cap), got %d", rowCount)
	}
	if !sawBinary {
		t.Fatal("expected at least one row's binary column to round-trip as real []byte matching the inserted blob")
	}
}

func bytesToHex(b []byte) string {
	const hexDigits = "0123456789ABCDEF"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexDigits[v>>4]
		out[i*2+1] = hexDigits[v&0x0F]
	}
	return string(out)
}
