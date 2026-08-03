package cmd

import (
	"context"
	"testing"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

// fakeSession and fakeSchemaListerSession mirror desktop/connections_test.go's
// and internal/tui/messages_connection_test.go's identically-named types: a
// minimal engine.Session (and its SchemaLister-implementing variant) for
// testing connectionNames' dispatch without a live database connection.
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

// TestConnectionNamesDispatchesToSchemaListerForPostgres is the
// regression test for #177: `connection test` called sess.ListDatabases
// directly, with no engine.SchemaLister dispatch — for Postgres,
// ListDatabases is documented as "informational only" and returns real
// sibling database names, not the schemas ListSchemas exists to provide.
// The command's printed output would mislead the user about what to pass
// as --db elsewhere (Query/TableSchema/ListNamespaces all treat that
// parameter as a schema name for Postgres).
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
