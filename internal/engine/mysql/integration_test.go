//go:build integration

// Run against a real MySQL — see docker-compose.test.yml and
// scripts/dev-seed/README.md:
//
//	docker compose -f docker-compose.test.yml up -d mysql
//	go test -tags=integration ./internal/engine/mysql/...
package mysql

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
)

func testURI() string {
	if v := os.Getenv("MONGOBAK_TEST_MYSQL_URI"); v != "" {
		return v
	}
	return "mongobak:mongobak@tcp(127.0.0.1:53306)/mongobak_test"
}

func openTestSession(t *testing.T) engine.SQLSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := (Engine{}).Open(ctx, engine.ConnConfig{URI: testURI()})
	if err != nil {
		t.Skipf("mysql not reachable at %s (start it with docker-compose.test.yml): %v", testURI(), err)
	}
	sqlSess := sess.(engine.SQLSession)
	t.Cleanup(func() { sess.Close(context.Background()) })
	return sqlSess
}

func mustExec(t *testing.T, s engine.SQLSession, database, sqlText string) {
	t.Helper()
	if _, err := s.Execute(context.Background(), database, sqlText); err != nil {
		t.Fatalf("exec %q: %v", sqlText, err)
	}
}

func TestIntegrationMySQLIntrospectionAndQuery(t *testing.T) {
	s := openTestSession(t)
	ctx := context.Background()
	const db = "mongobak_test"

	mustExec(t, s, db, `DROP TABLE IF EXISTS it_orders`)
	mustExec(t, s, db, `DROP TABLE IF EXISTS it_users`)
	t.Cleanup(func() {
		mustExec(t, s, db, `DROP TABLE IF EXISTS it_orders`)
		mustExec(t, s, db, `DROP TABLE IF EXISTS it_users`)
	})

	mustExec(t, s, db, `CREATE TABLE it_users (id INT AUTO_INCREMENT PRIMARY KEY, email VARCHAR(255) NOT NULL)`)
	mustExec(t, s, db, `CREATE TABLE it_orders (id INT AUTO_INCREMENT PRIMARY KEY, user_id INT NOT NULL, total DECIMAL(10,2) NOT NULL, FOREIGN KEY (user_id) REFERENCES it_users(id))`)
	mustExec(t, s, db, `INSERT INTO it_users (email) VALUES ('a@example.com'), ('b@example.com')`)
	mustExec(t, s, db, `INSERT INTO it_orders (user_id, total) VALUES (1, 19.99), (1, 5.00), (2, 100.00)`)

	dbs, err := s.ListDatabases(ctx)
	if err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	found := false
	for _, d := range dbs {
		if d == db {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %q in database list, got %v", db, dbs)
	}

	namespaces, err := s.ListNamespaces(ctx, db)
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

	schema, err := s.TableSchema(ctx, db, "it_orders")
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

	result, err := s.Query(ctx, db, `SELECT id, email FROM it_users ORDER BY id`)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result.Total != 2 {
		t.Fatalf("expected 2 rows, got %d", result.Total)
	}
	if result.Rows[0]["email"].Display != "a@example.com" {
		t.Fatalf("unexpected first row: %+v", result.Rows[0])
	}

	n, err := s.Execute(ctx, db, `UPDATE it_orders SET total = total + 1 WHERE user_id = 1`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 rows updated, got %d", n)
	}

	plan, err := s.Explain(ctx, db, `SELECT * FROM it_orders`)
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if plan == "" {
		t.Fatal("expected non-empty EXPLAIN output")
	}
}
