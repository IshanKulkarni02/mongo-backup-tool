package main

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

// fakeSession is a minimal engine.Session for testing testConnectionNames'
// dispatch without a live database connection.
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

// fakeSchemaListerSession additionally implements engine.SchemaLister, the
// way internal/engine/postgres's Session does.
type fakeSchemaListerSession struct {
	fakeSession
	schemas []string
}

func (f *fakeSchemaListerSession) ListSchemas(ctx context.Context) ([]string, error) {
	return f.schemas, nil
}

// TestTestConnectionNamesUsesSchemaListerWhenAvailable guards against #19:
// TestConnection populated the Postgres schema picker with ListDatabases'
// real sibling database names (e.g. myapp_prod), none of which will ever
// match a pg_namespace schema name — table browsing silently came up
// empty. An engine.SchemaLister session (Postgres) must have its
// ListSchemas used instead of the base ListDatabases.
func TestTestConnectionNamesUsesSchemaListerWhenAvailable(t *testing.T) {
	sess := &fakeSchemaListerSession{
		fakeSession: fakeSession{databases: []string{"myapp_prod", "myapp_staging"}},
		schemas:     []string{"public", "reporting"},
	}
	got, err := testConnectionNames(context.Background(), sess)
	if err != nil {
		t.Fatalf("testConnectionNames: %v", err)
	}
	if len(got) != 2 || got[0] != "public" || got[1] != "reporting" {
		t.Fatalf("expected ListSchemas' schema names, got %v", got)
	}
}

// TestTestConnectionNamesFallsBackToListDatabases confirms the #19 fix
// doesn't regress engines with no schema concept (Mongo, MySQL, SQLite):
// a plain Session (no SchemaLister) must still return ListDatabases' names.
func TestTestConnectionNamesFallsBackToListDatabases(t *testing.T) {
	sess := &fakeSession{databases: []string{"app", "analytics"}}
	got, err := testConnectionNames(context.Background(), sess)
	if err != nil {
		t.Fatalf("testConnectionNames: %v", err)
	}
	if len(got) != 2 || got[0] != "app" || got[1] != "analytics" {
		t.Fatalf("expected ListDatabases' database names, got %v", got)
	}
}

// TestAddConnectionConcurrentCallsDontLoseWrites guards against #22's
// central failure scenario: Wails dispatches every exported method call
// (AddConnection included) in its own goroutine, so a user adding several
// connections in quick succession — or one add landing while the startup
// credential-migration goroutine is still saving — used to race on
// config.json, with whichever Save landed last silently discarding
// earlier connections. AddConnection now goes through config.Update,
// which serializes the whole load-mutate-save sequence.
func TestAddConnectionConcurrentCallsDontLoseWrites(t *testing.T) {
	withTempConfigDir(t)
	a := NewApp()
	t.Cleanup(a.engines.Close)

	const n = 30
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := a.AddConnection(ConnectionInput{
				Name: fmt.Sprintf("conn-%d", i),
				URI:  "mongodb://localhost:27017",
			})
			if err != nil {
				t.Errorf("AddConnection: %v", err)
			}
		}(i)
	}
	wg.Wait()

	conns, err := a.ListConnections()
	if err != nil {
		t.Fatalf("ListConnections: %v", err)
	}
	if len(conns) != n {
		t.Fatalf("expected %d connections after %d concurrent AddConnection calls, got %d", n, n, len(conns))
	}
	for i := 0; i < n; i++ {
		found := false
		for _, c := range conns {
			if c.Name == fmt.Sprintf("conn-%d", i) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("conn-%d is missing — its AddConnection was lost to a race", i)
		}
	}
}
