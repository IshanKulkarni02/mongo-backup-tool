package main

import (
	"testing"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

// TestLikeSearchPatternEscapesMySQLBackslash is the regression test for
// #166: cross-database search's LIKE pattern doubled single quotes but
// never escaped backslashes, the same bug #16 fixed for the snapshot-
// restore path. MySQL treats "\" as a string escape character by
// default, so an unescaped backslash in the search term corrupts the
// generated pattern or breaks out of the string literal.
func TestLikeSearchPatternEscapesMySQLBackslash(t *testing.T) {
	cases := []struct {
		engineID string
		term     string
		want     string
	}{
		{"postgres", "hello", "'%hello%'"},
		{"postgres", "it's", "'%it''s%'"},
		{"postgres", `C:\temp`, `'%C:\temp%'`},
		{"mysql", `C:\temp`, `'%C:\\temp%'`},
		{"mysql", `trailing\`, `'%trailing\\%'`},
		{"sqlite", `C:\temp`, `'%C:\temp%'`},
	}
	for _, c := range cases {
		if got := likeSearchPattern(c.engineID, c.term); got != c.want {
			t.Errorf("likeSearchPattern(%q, %q) = %q, want %q", c.engineID, c.term, got, c.want)
		}
	}
}

func TestInferFieldNames(t *testing.T) {
	docs := []string{
		`{"name": "Ada", "email": "ada@example.com"}`,
		`{"name": "Grace", "plan": "pro"}`,
		`not valid json`,
	}
	fields := inferFieldNames(docs)
	got := map[string]bool{}
	for _, f := range fields {
		got[f] = true
	}
	for _, want := range []string{"name", "email", "plan"} {
		if !got[want] {
			t.Errorf("expected field %q in %v", want, fields)
		}
	}
	if len(fields) != 3 {
		t.Errorf("expected exactly 3 unique fields, got %v", fields)
	}
}

func TestInferFieldNamesCapsAtMax(t *testing.T) {
	var docs []string
	doc := `{`
	for i := 0; i < 30; i++ {
		if i > 0 {
			doc += ","
		}
		doc += `"field` + string(rune('a'+i)) + `": 1`
	}
	doc += `}`
	docs = append(docs, doc)
	fields := inferFieldNames(docs)
	if len(fields) != 20 {
		t.Fatalf("expected fields capped at 20, got %d", len(fields))
	}
}

func TestPreviewRow(t *testing.T) {
	row := map[string]engine.Cell{
		"id":    {Display: "1"},
		"email": {Display: "ada@example.com"},
	}
	columns := []engine.Column{{Name: "id"}, {Name: "email"}}
	got := previewRow(row, columns)
	want := "id=1, email=ada@example.com"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPreviewRowTruncatesLongValues(t *testing.T) {
	long := ""
	for i := 0; i < 60; i++ {
		long += "x"
	}
	row := map[string]engine.Cell{"note": {Display: long}}
	columns := []engine.Column{{Name: "note"}}
	got := previewRow(row, columns)
	want := "note=" + long[:40] + "…"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPreviewDocTruncates(t *testing.T) {
	long := "{"
	for i := 0; i < 300; i++ {
		long += "x"
	}
	long += "}"
	got := previewDoc(long)
	want := long[:200] + "…"
	if got != want {
		t.Errorf("got length %d, want length %d", len(got), len(want))
	}
	short := `{"a":1}`
	if previewDoc(short) != short {
		t.Errorf("expected short doc unchanged, got %q", previewDoc(short))
	}
}
