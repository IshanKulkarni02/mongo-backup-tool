//go:build integration

// Run against a real Postgres — see docker-compose.test.yml and
// scripts/dev-seed/README.md:
//
//	docker compose -f docker-compose.test.yml up -d postgres
//	go test -tags=integration ./internal/engine/postgres/...
package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
)

func testURI() string {
	if v := os.Getenv("MONGOBAK_TEST_POSTGRES_URI"); v != "" {
		return v
	}
	return "postgres://mongobak:mongobak@localhost:55432/mongobak_test?sslmode=disable"
}

func openTestSession(t *testing.T) engine.SQLSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := (Engine{}).Open(ctx, engine.ConnConfig{URI: testURI()})
	if err != nil {
		t.Skipf("postgres not reachable at %s (start it with docker-compose.test.yml): %v", testURI(), err)
	}
	sqlSess := sess.(engine.SQLSession)
	t.Cleanup(func() { sess.Close(context.Background()) })
	return sqlSess
}

func mustExec(t *testing.T, s engine.SQLSession, sqlText string) {
	t.Helper()
	if _, err := s.Execute(context.Background(), "public", sqlText); err != nil {
		t.Fatalf("exec %q: %v", sqlText, err)
	}
}

func TestIntegrationPostgresIntrospectionAndQuery(t *testing.T) {
	s := openTestSession(t)
	ctx := context.Background()

	mustExec(t, s, `DROP TABLE IF EXISTS it_orders`)
	mustExec(t, s, `DROP TABLE IF EXISTS it_users`)
	t.Cleanup(func() {
		mustExec(t, s, `DROP TABLE IF EXISTS it_orders`)
		mustExec(t, s, `DROP TABLE IF EXISTS it_users`)
	})

	mustExec(t, s, `CREATE TABLE it_users (id SERIAL PRIMARY KEY, email TEXT NOT NULL)`)
	mustExec(t, s, `CREATE TABLE it_orders (id SERIAL PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES it_users(id), total NUMERIC(10,2) NOT NULL)`)
	mustExec(t, s, `INSERT INTO it_users (email) VALUES ('a@example.com'), ('b@example.com')`)
	mustExec(t, s, `INSERT INTO it_orders (user_id, total) VALUES (1, 19.99), (1, 5.00), (2, 100.00)`)

	dbs, err := s.ListDatabases(ctx)
	if err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	if len(dbs) == 0 {
		t.Fatal("expected at least one database")
	}

	namespaces, err := s.ListNamespaces(ctx, "public")
	if err != nil {
		t.Fatalf("ListNamespaces: %v", err)
	}
	names := map[string]bool{}
	for _, ns := range namespaces {
		names[ns.Name] = true
	}
	if !names["it_users"] || !names["it_orders"] {
		t.Fatalf("expected it_users and it_orders in namespace list, got %+v", namespaces)
	}

	schema, err := s.TableSchema(ctx, "public", "it_orders")
	if err != nil {
		t.Fatalf("TableSchema: %v", err)
	}
	if len(schema.ForeignKeys) != 1 {
		t.Fatalf("expected 1 FK on it_orders, got %d: %+v", len(schema.ForeignKeys), schema.ForeignKeys)
	}
	fk := schema.ForeignKeys[0]
	if fk.Column != "user_id" || fk.RefTable != "it_users" || fk.RefColumn != "id" {
		t.Fatalf("unexpected FK: %+v", fk)
	}

	result, err := s.Query(ctx, "public", `SELECT id, email FROM it_users ORDER BY id`)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result.Total != 2 {
		t.Fatalf("expected 2 rows, got %d", result.Total)
	}
	if result.Rows[0]["email"].Display != "a@example.com" {
		t.Fatalf("unexpected first row: %+v", result.Rows[0])
	}

	n, err := s.Execute(ctx, "public", `UPDATE it_orders SET total = total + 1 WHERE user_id = 1`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 rows updated, got %d", n)
	}

	plan, err := s.Explain(ctx, "public", `SELECT * FROM it_orders`)
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if plan == "" {
		t.Fatal("expected non-empty EXPLAIN output")
	}
}

func TestIntegrationPostgresTenantSessionVar(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := (Engine{}).Open(ctx, engine.ConnConfig{
		URI:              testURI(),
		TenantSessionVar: "app.current_tenant",
		TenantValue:      "acme",
	})
	if err != nil {
		t.Skipf("postgres not reachable: %v", err)
	}
	defer sess.Close(context.Background())

	sqlSess := sess.(engine.SQLSession)
	result, err := sqlSess.Query(context.Background(), "public", `SELECT current_setting('app.current_tenant', true) AS tenant`)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["tenant"].Display != "acme" {
		t.Fatalf("expected tenant session var to be set to 'acme', got %+v", result.Rows)
	}
}
