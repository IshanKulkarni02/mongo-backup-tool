package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	_ "github.com/IshanKulkarni02/dbhelm/internal/engine/sqlite"
)

func withTempConfigDir(t *testing.T) {
	t.Helper()
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
}

func TestSaveAndListQuery(t *testing.T) {
	withTempConfigDir(t)
	a := &App{}

	id, err := a.SaveQuery("", "Active users", "local", "app", "SELECT * FROM users WHERE active")
	if err != nil {
		t.Fatalf("SaveQuery: %v", err)
	}
	if id == "" {
		t.Fatal("expected a generated ID")
	}

	queries, err := a.ListSavedQueries()
	if err != nil {
		t.Fatalf("ListSavedQueries: %v", err)
	}
	if len(queries) != 1 || queries[0].Name != "Active users" {
		t.Fatalf("expected 1 saved query, got %+v", queries)
	}
}

func TestSaveQueryRequiresNameAndText(t *testing.T) {
	withTempConfigDir(t)
	a := &App{}
	if _, err := a.SaveQuery("", "", "local", "app", "SELECT 1"); err == nil {
		t.Fatal("expected an error for a missing name")
	}
	if _, err := a.SaveQuery("", "name", "local", "app", ""); err == nil {
		t.Fatal("expected an error for missing SQL text")
	}
}

func TestDeleteSavedQueryPrunesWidgets(t *testing.T) {
	withTempConfigDir(t)
	a := &App{}

	qid, err := a.SaveQuery("", "q", "local", "app", "SELECT 1")
	if err != nil {
		t.Fatalf("SaveQuery: %v", err)
	}
	wid, err := a.SaveWidget("", "chart", qid, "bar", "x", []string{"y"})
	if err != nil {
		t.Fatalf("SaveWidget: %v", err)
	}

	if err := a.DeleteSavedQuery(qid); err != nil {
		t.Fatalf("DeleteSavedQuery: %v", err)
	}

	widgets, err := a.ListWidgets()
	if err != nil {
		t.Fatalf("ListWidgets: %v", err)
	}
	for _, w := range widgets {
		if w.ID == wid {
			t.Fatal("expected widget depending on deleted query to be pruned")
		}
	}
}

func TestSaveWidgetRequiresExistingQuery(t *testing.T) {
	withTempConfigDir(t)
	a := &App{}
	if _, err := a.SaveWidget("", "chart", "does-not-exist", "bar", "x", []string{"y"}); err == nil {
		t.Fatal("expected an error when the referenced query doesn't exist")
	}
}

func TestDeleteWidgetMissingReturnsError(t *testing.T) {
	withTempConfigDir(t)
	a := &App{}
	if err := a.DeleteWidget("nope"); err == nil {
		t.Fatal("expected an error deleting a nonexistent widget")
	}
}

// newTestAppWithSQLiteConnRO is newTestAppWithSQLiteConn (snapshots_test.go)
// with a configurable ReadOnly flag, for exercising requireWritable gating.
func newTestAppWithSQLiteConnRO(t *testing.T, connName, uri string, readOnly bool) *App {
	t.Helper()
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Connections = append(cfg.Connections, config.Connection{
		Name: connName, URI: uri, Engine: "sqlite", ReadOnly: readOnly,
	})
	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	a := NewApp()
	t.Cleanup(a.engines.Close)
	return a
}

// TestRunSavedQueryBlocksWriteOnReadOnlyConnection guards against #45: a
// saved query isn't guaranteed to be a read (SaveQuery accepts any SQL
// text), so a write saved against a Safe Mode / read-only connection must
// still be refused when re-run from the Dashboard, exactly as it would be
// via RunSQLExecute.
func TestRunSavedQueryBlocksWriteOnReadOnlyConnection(t *testing.T) {
	a := newTestAppWithSQLiteConnRO(t, "ro-conn", "file::memory:?cache=private", true)

	qid, err := a.SaveQuery("", "wipe users", "ro-conn", "main", "DELETE FROM users WHERE id = 1")
	if err != nil {
		t.Fatalf("SaveQuery: %v", err)
	}
	if _, err := a.RunSavedQuery(qid); !errors.Is(err, engine.ErrReadOnly) {
		t.Fatalf("RunSavedQuery = %v, want engine.ErrReadOnly", err)
	}
}

// TestRunSavedQueryBlocksDangerousStatement guards against #45: even on a
// writable connection, RunSavedQuery has no "type the database name"
// confirmation UI, so a dangerous statement (DROP/TRUNCATE/ALTER, or an
// unqualified DELETE/UPDATE) must be refused outright rather than silently
// executed.
func TestRunSavedQueryBlocksDangerousStatement(t *testing.T) {
	a := newTestAppWithSQLiteConnRO(t, "rw-conn", "file::memory:?cache=private", false)

	qid, err := a.SaveQuery("", "drop users", "rw-conn", "main", "DROP TABLE users")
	if err != nil {
		t.Fatalf("SaveQuery: %v", err)
	}
	_, err = a.RunSavedQuery(qid)
	if err == nil {
		t.Fatal("expected RunSavedQuery to refuse a dangerous statement")
	}
	if !strings.Contains(err.Error(), "dangerous statement") {
		t.Fatalf("RunSavedQuery error = %q, want it to mention the dangerous-statement refusal", err.Error())
	}
}

// TestRunSavedQueryAllowsReadOnReadOnlyConnection confirms the fix for #45
// doesn't regress the legitimate case: a saved read must still run fine
// against a connection marked read-only.
func TestRunSavedQueryAllowsReadOnReadOnlyConnection(t *testing.T) {
	a := newTestAppWithSQLiteConnRO(t, "ro-conn", "file::memory:?cache=private", true)

	qid, err := a.SaveQuery("", "trivial read", "ro-conn", "main", "SELECT 1")
	if err != nil {
		t.Fatalf("SaveQuery: %v", err)
	}
	result, err := a.RunSavedQuery(qid)
	if err != nil {
		t.Fatalf("RunSavedQuery: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row from SELECT 1, got %d", len(result.Rows))
	}
}
