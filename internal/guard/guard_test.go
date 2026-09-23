package guard

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/engine/sqlite"
)

func openSQLite(t *testing.T) engine.SQLSession {
	t.Helper()
	sess, err := (sqlite.Engine{}).Open(context.Background(), engine.ConnConfig{URI: filepath.Join(t.TempDir(), "g.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close(context.Background()) })
	ss := sess.(engine.SQLSession)
	for _, q := range []string{
		"CREATE TABLE t(id INTEGER PRIMARY KEY, v TEXT)",
		"INSERT INTO t(v) VALUES ('a'),('b'),('c'),('d'),('e')",
	} {
		if _, err := ss.Execute(context.Background(), "main", q); err != nil {
			t.Fatal(err)
		}
	}
	return ss
}

func codeOf(t *testing.T, err error) Code {
	t.Helper()
	var ge *Error
	if !errors.As(err, &ge) {
		t.Fatalf("want *guard.Error, got %T: %v", err, err)
	}
	return ge.Code
}

func TestReadSQLRefusesWrites(t *testing.T) {
	ss := openSQLite(t)
	for _, q := range []string{
		"INSERT INTO t(v) VALUES ('x')",
		"DELETE FROM t",
		"DROP TABLE t",
		"WITH x AS (DELETE FROM t RETURNING *) SELECT * FROM x",
		"",
	} {
		_, err := ReadSQL(context.Background(), ss, "main", q, Limits{})
		if err == nil {
			t.Errorf("ReadSQL(%q) succeeded, want refusal", q)
			continue
		}
		if c := codeOf(t, err); c != CodeReadOnlyViolation && c != CodeBadRequest {
			t.Errorf("ReadSQL(%q) code = %s", q, c)
		}
	}
	res, err := ReadSQL(context.Background(), ss, "main", "SELECT COUNT(*) AS n FROM t", Limits{})
	if err != nil || res.Rows[0]["n"].Display != "5" {
		t.Fatalf("table changed or read failed: %v %+v", err, res)
	}
}

// The database itself must refuse a write even when the text filter is
// bypassed (here by calling the engine's read-only path directly).
func TestQueryReadOnlyIsEnforcedByTheDatabase(t *testing.T) {
	ss := openSQLite(t)
	ro := ss.(engine.ReadOnlySQLSession)
	if _, err := ro.QueryReadOnly(context.Background(), "main", "INSERT INTO t(v) VALUES ('x') RETURNING id", engine.ReadLimits{}); err == nil {
		t.Fatal("database accepted a write inside a read-only query")
	}
	// The pinned connection must be writable again afterwards.
	if _, err := ss.Execute(context.Background(), "main", "INSERT INTO t(v) VALUES ('y')"); err != nil {
		t.Fatalf("query_only leaked past the guarded read: %v", err)
	}
}

func TestReadSQLTruncationIsExplicit(t *testing.T) {
	ss := openSQLite(t)
	res, err := ReadSQL(context.Background(), ss, "main", "SELECT id, v FROM t ORDER BY id", Limits{MaxRows: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 2 || !res.Truncated {
		t.Fatalf("rows=%d truncated=%v, want 2/true", len(res.Rows), res.Truncated)
	}
	res, err = ReadSQL(context.Background(), ss, "main", "SELECT id FROM t", Limits{MaxRows: 5})
	if err != nil || len(res.Rows) != 5 || res.Truncated {
		t.Fatalf("exact-fit result must not be marked truncated: %v %d %v", err, len(res.Rows), res.Truncated)
	}
}

func TestReadSQLCapsCellSize(t *testing.T) {
	ss := openSQLite(t)
	res, err := ReadSQL(context.Background(), ss, "main", "SELECT printf('%.*c', 500, 'z') AS big", Limits{MaxCellBytes: 40})
	if err != nil {
		t.Fatal(err)
	}
	got := res.Rows[0]["big"].Display
	if !strings.Contains(got, "cell truncated: 500 bytes") || len(got) > 100 {
		t.Fatalf("cell not capped: %q", got)
	}
}

func TestExplainRefusesAnalyzeAndWrites(t *testing.T) {
	ss := openSQLite(t)
	if _, err := Explain(context.Background(), ss, "main", "ANALYZE DELETE FROM t"); err == nil {
		t.Error("ANALYZE explain allowed")
	}
	if _, err := Explain(context.Background(), ss, "main", "DELETE FROM t"); err == nil {
		t.Error("explain of a write allowed")
	}
	if _, err := Explain(context.Background(), ss, "main", "SELECT * FROM t"); err != nil {
		t.Errorf("plain explain failed: %v", err)
	}
}

func TestForbiddenJavaScriptOperators(t *testing.T) {
	for _, f := range []string{
		`{"$where":"sleep(1000)"}`,
		`{"a":{"$or":[{"$where":"true"}]}}`,
		`{"$expr":{"$function":{"body":"x","args":[],"lang":"js"}}}`,
	} {
		if err := checkJSON(f); err == nil {
			t.Errorf("filter %s allowed", f)
		}
	}
	if err := checkJSON(`{"a":{"$gt":1}}`); err != nil {
		t.Errorf("plain filter refused: %v", err)
	}
	if err := checkJSON(""); err != nil {
		t.Errorf("empty filter refused: %v", err)
	}
}

func TestScrubRemovesConnectionDetails(t *testing.T) {
	cases := map[string]string{
		`dial tcp: postgres://bob:hunter2@db.internal:5432/app failed`: "hunter2",
		`mongodb+srv://u:pw@cluster0.mongodb.net/?retryWrites=true`:    "pw@",
		`user:secret@tcp(10.0.0.5:3306)/shop: connection refused`:      "secret",
		`failed with password=hunter2 for host`:                        "hunter2",
		`connect to 10.1.2.3:5432 timed out`:                           "10.1.2.3",
	}
	for in, leaked := range cases {
		if out := Scrub(in); strings.Contains(out, leaked) {
			t.Errorf("Scrub(%q) = %q still contains %q", in, out, leaked)
		}
	}
	if got := Scrub("relation \"t\" does not exist"); got != "relation \"t\" does not exist" {
		t.Errorf("ordinary error altered: %q", got)
	}
}

func TestLimitsDefaults(t *testing.T) {
	l := Limits{}.normalized()
	if l.MaxRows != 100 || l.MaxCellBytes != 4096 || l.Timeout != 15*time.Second {
		t.Fatalf("unexpected defaults: %+v", l)
	}
}
