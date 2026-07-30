package main

import "testing"

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
		value string
		want  string
	}{
		{"hello", "'hello'"},
		{"it's", "'it''s'"},
		{"42", "42"},
		{"-3.14", "-3.14"},
		{"NULL", "NULL"},
		{"null", "NULL"},
		{"", "''"},
		{"007", "007"}, // matches the numeric regex — deliberately not re-parsed/reformatted
	}
	for _, c := range cases {
		if got := importLiteral(c.value); got != c.want {
			t.Errorf("importLiteral(%q) = %q, want %q", c.value, got, c.want)
		}
	}
}
