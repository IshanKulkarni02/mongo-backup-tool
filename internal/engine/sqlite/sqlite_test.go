package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
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
