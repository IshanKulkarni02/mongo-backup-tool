package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportQuoteIdent(t *testing.T) {
	cases := []struct {
		engineID string
		name     string
		want     string
	}{
		{"postgres", "users", `"users"`},
		{"sqlite", "users", `"users"`},
		{"mysql", "users", "`users`"},
		{"postgres", `we"ird`, `"we""ird"`},
		{"mysql", "we`ird", "`we``ird`"},
	}
	for _, c := range cases {
		if got := importQuoteIdent(c.engineID, c.name); got != c.want {
			t.Errorf("importQuoteIdent(%q, %q) = %q, want %q", c.engineID, c.name, got, c.want)
		}
	}
}

func TestImportLiteral(t *testing.T) {
	cases := []struct {
		engineID string
		value    string
		want     string
	}{
		{"postgres", "hello", "'hello'"},
		{"postgres", "it's", "'it''s'"},
		{"postgres", "42", "42"},
		{"postgres", "-3.14", "-3.14"},
		{"postgres", "NULL", "NULL"},
		{"postgres", "null", "NULL"},
		{"postgres", "", "''"},
		{"postgres", "007", "007"}, // matches the numeric regex — deliberately not re-parsed/reformatted
		// #166: MySQL treats "\" as a string escape character by default,
		// so a value containing one must have it escaped first, before
		// quote-doubling — but only for MySQL, since Postgres/SQLite don't
		// treat "\" specially in standard-conforming-strings mode.
		{"mysql", `C:\temp\new`, `'C:\\temp\\new'`},
		{"postgres", `C:\temp\new`, `'C:\temp\new'`},
		{"mysql", `trailing\`, `'trailing\\'`},
	}
	for _, c := range cases {
		if got := importLiteral(c.engineID, c.value); got != c.want {
			t.Errorf("importLiteral(%q, %q) = %q, want %q", c.engineID, c.value, got, c.want)
		}
	}
}

// TestImportCSVWithoutHeaderRowUsesPositionalMapping is the regression test
// for #21: with hasHeaderRow=false there's no header row for the frontend
// to build a name-based columnMapping from, so ImportCSV must instead
// accept each mapped column's 0-based positional index as a decimal
// string — matching what the frontend now sends in that mode. Previously
// this errored on every row with "mapped CSV column ... not found in the
// file's header" because csvIndexByHeader was always empty when there was
// no header to read.
func TestImportCSVWithoutHeaderRowUsesPositionalMapping(t *testing.T) {
	a := newTestAppWithSQLiteConnRO(t, "rw-conn", "file::memory:?cache=private", false)
	if _, err := a.RunSQLExecute("rw-conn", "main", `CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT, age INTEGER)`, ""); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}

	csvPath := filepath.Join(t.TempDir(), "no_header.csv")
	if err := os.WriteFile(csvPath, []byte("1,a@example.com,30\n2,b@example.com,40\n"), 0o644); err != nil {
		t.Fatalf("writing CSV fixture: %v", err)
	}

	n, err := a.ImportCSV("rw-conn", "main", "users", csvPath, "sqlite", false, map[string]string{
		"id":    "0",
		"email": "1",
		"age":   "2",
	})
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 rows imported (including the first CSV row, since there's no header to skip), got %d", n)
	}

	sess, release, err := a.sqlSession("rw-conn")
	if err != nil {
		t.Fatalf("sqlSession: %v", err)
	}
	defer release()
	res, err := sess.Query(t.Context(), "main", "SELECT id, email, age FROM users ORDER BY id")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(res.Rows))
	}
	if res.Rows[0]["email"].Display != "a@example.com" || res.Rows[0]["age"].Display != "30" {
		t.Fatalf("row 0 not imported correctly: %+v", res.Rows[0])
	}
	if res.Rows[1]["email"].Display != "b@example.com" || res.Rows[1]["age"].Display != "40" {
		t.Fatalf("row 1 not imported correctly: %+v", res.Rows[1])
	}
}

// TestImportCSVWithoutHeaderRowRejectsNonNumericMapping guards the error
// path: a columnMapping value that isn't a valid position (e.g. leftover
// header text from switching modes) must be rejected with a clear error
// instead of silently mismapping or panicking.
func TestImportCSVWithoutHeaderRowRejectsNonNumericMapping(t *testing.T) {
	a := newTestAppWithSQLiteConnRO(t, "rw-conn", "file::memory:?cache=private", false)
	if _, err := a.RunSQLExecute("rw-conn", "main", `CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)`, ""); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}

	csvPath := filepath.Join(t.TempDir(), "no_header.csv")
	if err := os.WriteFile(csvPath, []byte("1,a@example.com\n"), 0o644); err != nil {
		t.Fatalf("writing CSV fixture: %v", err)
	}

	if _, err := a.ImportCSV("rw-conn", "main", "users", csvPath, "sqlite", false, map[string]string{
		"email": "email", // not a valid position
	}); err == nil {
		t.Fatal("expected an error for a non-numeric column mapping when hasHeaderRow is false")
	}
}
