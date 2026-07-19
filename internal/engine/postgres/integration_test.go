//go:build integration

// Run against a real Postgres — see docker-compose.test.yml and
// scripts/dev-seed/README.md:
//
//	docker compose -f docker-compose.test.yml up -d postgres
//	go test -tags=integration ./internal/engine/postgres/...
package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

func testURI() string {
	if v := os.Getenv("DBHELM_TEST_POSTGRES_URI"); v != "" {
		return v
	}
	return "postgres://dbhelm:dbhelm@localhost:55432/dbhelm_test?sslmode=disable"
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

func TestIntegrationPostgresCompositePrimaryKeyAndIndexes(t *testing.T) {
	s := openTestSession(t)
	ctx := context.Background()

	mustExec(t, s, `DROP TABLE IF EXISTS it_membership`)
	t.Cleanup(func() { mustExec(t, s, `DROP TABLE IF EXISTS it_membership`) })

	mustExec(t, s, `CREATE TABLE it_membership (org_id INT, user_id INT, role TEXT, PRIMARY KEY (user_id, org_id))`)
	mustExec(t, s, `CREATE INDEX it_membership_role_idx ON it_membership (role)`)

	schema, err := s.TableSchema(ctx, "public", "it_membership")
	if err != nil {
		t.Fatalf("TableSchema: %v", err)
	}
	if len(schema.PrimaryKey) != 2 || schema.PrimaryKey[0] != "user_id" || schema.PrimaryKey[1] != "org_id" {
		t.Fatalf("expected composite PK [user_id org_id] in declared order, got %v", schema.PrimaryKey)
	}

	indexes, err := s.ListTableIndexes(ctx, "public", "it_membership")
	if err != nil {
		t.Fatalf("ListTableIndexes: %v", err)
	}
	if len(indexes) != 1 || indexes[0].Name != "it_membership_role_idx" {
		t.Fatalf("expected exactly the explicit role index (PK-backing index excluded), got %+v", indexes)
	}
}

func TestIntegrationPostgresBeginConsistentReadStreamsAllRowsIncludingBinary(t *testing.T) {
	s := openTestSession(t)
	ctx := context.Background()

	mustExec(t, s, `DROP TABLE IF EXISTS it_files`)
	t.Cleanup(func() { mustExec(t, s, `DROP TABLE IF EXISTS it_files`) })
	mustExec(t, s, `CREATE TABLE it_files (id SERIAL PRIMARY KEY, data BYTEA)`)

	blob := []byte{0x00, 0x01, 0xFF, 0xFE, 'h', 'i'}
	for i := 0; i < 600; i++ { // exceeds sqlbase.QueryRowCap, unlike Query
		mustExec(t, s, fmt.Sprintf(`INSERT INTO it_files (data) VALUES ('\x%x')`, blob))
	}

	tx, err := s.BeginConsistentRead(ctx)
	if err != nil {
		t.Fatalf("BeginConsistentRead: %v", err)
	}
	defer tx.Close(ctx)

	var rowCount int
	var sawBinary bool
	err = tx.StreamRows(ctx, "public", "it_files", func(row map[string]any) error {
		rowCount++
		if b, ok := row["data"].([]byte); ok && string(b) == string(blob) {
			sawBinary = true
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
		t.Fatal("expected a row's BYTEA column to round-trip as real []byte matching the inserted blob")
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
