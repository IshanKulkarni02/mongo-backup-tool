package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/engine/safeguard"
)

// TestRunSQLQueryBlocksWriteCTEOnReadOnlyConnection guards against #9: a
// writable CTE (e.g. "WITH x AS (DELETE ...) SELECT * FROM x") looks like a
// read to the frontend's client-side routing, so RunSQLQuery must reject it
// itself on a read-only connection rather than trusting the caller.
func TestRunSQLQueryBlocksWriteCTEOnReadOnlyConnection(t *testing.T) {
	a := newTestAppWithSQLiteConnRO(t, "ro-conn", "file::memory:?cache=private", true)

	sql := "WITH x AS (DELETE FROM sqlite_sequence RETURNING name) SELECT * FROM x"
	if _, err := a.RunSQLQuery("ro-conn", "main", sql); !errors.Is(err, engine.ErrReadOnly) {
		t.Fatalf("RunSQLQuery = %v, want engine.ErrReadOnly", err)
	}
}

// TestRunSQLQueryAllowsReadOnReadOnlyConnection confirms the #9 fix doesn't
// regress the legitimate case: an ordinary read must still run fine against
// a connection marked read-only.
func TestRunSQLQueryAllowsReadOnReadOnlyConnection(t *testing.T) {
	a := newTestAppWithSQLiteConnRO(t, "ro-conn", "file::memory:?cache=private", true)

	result, err := a.RunSQLQuery("ro-conn", "main", "SELECT 1")
	if err != nil {
		t.Fatalf("RunSQLQuery: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row from SELECT 1, got %d", len(result.Rows))
	}
}

// TestRunSQLQueryBlocksDangerousStatement guards against #9: even on a
// writable connection, RunSQLQuery/RunSQLQueryJob have no "type the
// database name" confirmation UI, so a dangerous statement smuggled in via
// a CTE must be refused outright rather than silently executed.
func TestRunSQLQueryBlocksDangerousStatement(t *testing.T) {
	a := newTestAppWithSQLiteConnRO(t, "rw-conn", "file::memory:?cache=private", false)

	sql := "WITH x AS (DELETE FROM sqlite_sequence RETURNING name) SELECT * FROM x"
	_, err := a.RunSQLQuery("rw-conn", "main", sql)
	if err == nil {
		t.Fatal("expected RunSQLQuery to refuse a dangerous statement smuggled in via a CTE")
	}
	if !strings.Contains(err.Error(), "dangerous statement") {
		t.Fatalf("RunSQLQuery error = %q, want it to mention the dangerous-statement refusal", err.Error())
	}
}

// TestRunSQLQueryJobBlocksWriteCTEOnReadOnlyConnection is
// TestRunSQLQueryBlocksWriteCTEOnReadOnlyConnection's counterpart for the
// cancelable job path, which has its own copy of the same gating.
func TestRunSQLQueryJobBlocksWriteCTEOnReadOnlyConnection(t *testing.T) {
	a := newTestAppWithSQLiteConnRO(t, "ro-conn", "file::memory:?cache=private", true)

	sql := "WITH x AS (DELETE FROM sqlite_sequence RETURNING name) SELECT * FROM x"
	id := a.RunSQLQueryJob("ro-conn", "main", sql)
	job := waitForJob(t, a, id)
	if job.Status != JobFailed {
		t.Fatalf("expected job to fail, got status %q", job.Status)
	}
	if !strings.Contains(job.Message, engine.ErrReadOnly.Error()) {
		t.Fatalf("job message = %q, want it to mention read-only refusal", job.Message)
	}
}

// TestExplainSQLBlocksAnalyzeWriteOnReadOnlyConnection guards against #9:
// Postgres's EXPLAIN ANALYZE genuinely executes the wrapped statement, so
// ExplainSQL must reject an ANALYZE-wrapped write on a read-only
// connection just like RunSQLExecute would. sqlite's Explain doesn't
// execute anything, but the gate must trigger before the engine call is
// even made, so this only needs to prove the error itself.
func TestExplainSQLBlocksAnalyzeWriteOnReadOnlyConnection(t *testing.T) {
	a := newTestAppWithSQLiteConnRO(t, "ro-conn", "file::memory:?cache=private", true)

	if _, err := a.ExplainSQL("ro-conn", "main", "ANALYZE DELETE FROM sqlite_sequence"); !errors.Is(err, engine.ErrReadOnly) {
		t.Fatalf("ExplainSQL = %v, want engine.ErrReadOnly", err)
	}
}

// TestCheckQueryStatementSkipsGateForAnalyzeSelect confirms the #9 fix
// doesn't regress the legitimate case: "ANALYZE SELECT ..." is a genuine
// read once its ANALYZE modifier is stripped (see StripExplainAnalyze), so
// ExplainSQL's gate must never even call requireWritable for it — checked
// directly against checkQueryStatement rather than through ExplainSQL
// end-to-end, since "ANALYZE ..." is Postgres-specific EXPLAIN syntax that
// sqlite's own Explain doesn't understand (only the gating logic is
// engine-independent here, not the statement itself).
func TestCheckQueryStatementSkipsGateForAnalyzeSelect(t *testing.T) {
	called := false
	requireWritable := func() error {
		called = true
		return engine.ErrReadOnly
	}
	inner := safeguard.StripExplainAnalyze("ANALYZE SELECT 1")
	if err := checkQueryStatement(inner, requireWritable); err != nil {
		t.Fatalf("checkQueryStatement: %v", err)
	}
	if called {
		t.Error("expected requireWritable not to be called for a genuine read")
	}
}

// TestExplainSQLAllowsPlainSelectOnReadOnlyConnection confirms the common
// case (no ANALYZE modifier at all) still works on a read-only connection.
func TestExplainSQLAllowsPlainSelectOnReadOnlyConnection(t *testing.T) {
	a := newTestAppWithSQLiteConnRO(t, "ro-conn", "file::memory:?cache=private", true)

	if _, err := a.ExplainSQL("ro-conn", "main", "SELECT 1"); err != nil {
		t.Fatalf("ExplainSQL: %v", err)
	}
}
