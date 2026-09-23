//go:build integration

// Run against real Postgres and MySQL — see docker-compose.test.yml:
//
//	docker compose -f docker-compose.test.yml up -d
//	go test -tags=integration ./internal/guard/...
package guard

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/engine/mysql"
	"github.com/IshanKulkarni02/dbhelm/internal/engine/postgres"
)

type target struct {
	name     string
	open     func(ctx context.Context, cfg engine.ConnConfig) (engine.Session, error)
	uri      string
	database string
	sleepSQL string // a read that runs for 3 seconds
	writeSQL string // a write that must be refused by the database itself
}

func targets() []target {
	pg := os.Getenv("DBHELM_TEST_POSTGRES_URI")
	if pg == "" {
		pg = "postgres://dbhelm:dbhelm@localhost:55432/dbhelm_test?sslmode=disable"
	}
	my := os.Getenv("DBHELM_TEST_MYSQL_URI")
	if my == "" {
		my = "dbhelm:dbhelm@tcp(localhost:53306)/dbhelm_test"
	}
	return []target{
		{"postgres", (postgres.Engine{}).Open, pg, "public", "SELECT pg_sleep(3)", "CREATE TABLE guard_should_not_exist(id int)"},
		{"mysql", (mysql.Engine{}).Open, my, "dbhelm_test", "SELECT SLEEP(3)", "CREATE TABLE guard_should_not_exist(id int)"},
	}
}

func TestIntegrationReadOnlyEnforcedByServer(t *testing.T) {
	for _, tg := range targets() {
		t.Run(tg.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			sess, err := tg.open(ctx, engine.ConnConfig{URI: tg.uri})
			if err != nil {
				t.Skipf("%s not reachable: %v", tg.name, err)
			}
			defer sess.Close(ctx)
			ss := sess.(engine.SQLSession)
			ro := ss.(engine.ReadOnlySQLSession)

			// Bypass the text filter: the server itself must refuse the write.
			if _, err := ro.QueryReadOnly(ctx, tg.database, tg.writeSQL, engine.ReadLimits{}); err == nil {
				t.Fatal("server accepted DDL inside a read-only query")
			}
			// Nothing was created, and the connection is writable again.
			if _, err := ss.Execute(ctx, tg.database, "CREATE TABLE guard_probe(id int)"); err != nil {
				t.Fatalf("connection left read-only after a guarded read: %v", err)
			}
			ss.Execute(ctx, tg.database, "DROP TABLE guard_probe")

			// Timeout is enforced.
			start := time.Now()
			_, err = ReadSQL(ctx, ss, tg.database, tg.sleepSQL, Limits{Timeout: time.Second})
			if err == nil || time.Since(start) > 2500*time.Millisecond {
				t.Fatalf("timeout not enforced: err=%v after %s", err, time.Since(start))
			}
			// A normal read still works afterwards (session state was reset).
			if _, err := ReadSQL(ctx, ss, tg.database, "SELECT 1 AS one", Limits{}); err != nil {
				t.Fatalf("read after timeout failed: %v", err)
			}
		})
	}
}
