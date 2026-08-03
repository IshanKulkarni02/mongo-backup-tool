package tui

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	_ "github.com/IshanKulkarni02/dbhelm/internal/engine/sqlite"
)

// fakeSession and fakeSchemaListerSession mirror desktop/connections_test.go's
// identically-named types, adapted for this package: a minimal
// engine.Session (and its SchemaLister-implementing variant) for testing
// connectionNames' dispatch without a live database connection.
type fakeSession struct {
	databases []string
}

func (f *fakeSession) Ping(ctx context.Context) error { return nil }
func (f *fakeSession) ListDatabases(ctx context.Context) ([]string, error) {
	return f.databases, nil
}
func (f *fakeSession) ListNamespaces(ctx context.Context, database string) ([]engine.NamespaceInfo, error) {
	return nil, nil
}
func (f *fakeSession) Close(ctx context.Context) error { return nil }

type fakeSchemaListerSession struct {
	fakeSession
	schemas []string
}

func (f *fakeSchemaListerSession) ListSchemas(ctx context.Context) ([]string, error) {
	return f.schemas, nil
}

// TestConnectionNamesDispatchesToSchemaListerForPostgres is the regression
// test for #169: connectionNames must prefer an engine.SchemaLister's
// ListSchemas over the base ListDatabases, mirroring
// desktop/connections.go's testConnectionNames — otherwise a Postgres
// connection's real sibling database names would be returned instead of
// its schemas, and browsing/table listing would silently come up empty.
func TestConnectionNamesDispatchesToSchemaListerForPostgres(t *testing.T) {
	sess := &fakeSchemaListerSession{
		fakeSession: fakeSession{databases: []string{"myapp_prod", "myapp_staging"}},
		schemas:     []string{"public", "reporting"},
	}
	got, err := connectionNames(context.Background(), sess)
	if err != nil {
		t.Fatalf("connectionNames: %v", err)
	}
	if len(got) != 2 || got[0] != "public" || got[1] != "reporting" {
		t.Fatalf("connectionNames = %v, want the SchemaLister's schemas, not ListDatabases' database names", got)
	}
}

// TestConnectionNamesFallsBackToListDatabases confirms an engine that
// doesn't implement SchemaLister (MongoDB, MySQL, SQLite) still gets its
// real database names.
func TestConnectionNamesFallsBackToListDatabases(t *testing.T) {
	sess := &fakeSession{databases: []string{"app", "analytics"}}
	got, err := connectionNames(context.Background(), sess)
	if err != nil {
		t.Fatalf("connectionNames: %v", err)
	}
	if len(got) != 2 || got[0] != "app" || got[1] != "analytics" {
		t.Fatalf("connectionNames = %v, want ListDatabases' names", got)
	}
}

// TestLoadDatabasesCmdWorksForNonMongoConnection is the end-to-end
// regression test for #169: loadDatabasesCmd used to call
// mongotools.TestConnection(uri) unconditionally, which rejects any
// non-mongodb:// URI outright. A saved SQL connection (sqlite here, no
// Docker needed) must be able to browse its databases from the TUI's
// connections screen without hitting the Mongo driver at all.
func TestLoadDatabasesCmdWorksForNonMongoConnection(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tui-conn-test.db")
	conn := config.Connection{Name: "sqlite-tui-test", URI: dbPath, Engine: "sqlite"}

	msg := loadDatabasesCmd(conn)()
	loaded, ok := msg.(databasesLoadedMsg)
	if !ok {
		t.Fatalf("loadDatabasesCmd returned %T, want databasesLoadedMsg", msg)
	}
	if loaded.err != nil {
		t.Fatalf("loadDatabasesCmd: %v", loaded.err)
	}
}
