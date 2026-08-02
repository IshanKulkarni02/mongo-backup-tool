package sqlbase

import (
	"database/sql"
	"testing"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

// TestCellFromRawBareTime guards against #26: a TIME column's raw value
// (e.g. "14:23:01") never matched any of parseAnyTime's layouts, all of
// which require a date component, so cellFromRaw fell back to a Cell with
// Display set but Raw left as the zero value — unlike every other
// successful branch, which sets Raw. A caller depending on Cell.Raw for a
// TIME column (e.g. an edit-and-write-back flow) silently lost the value.
func TestCellFromRawBareTime(t *testing.T) {
	got := cellFromRaw(sql.RawBytes("14:23:01"), "TIME")
	if got.Type != engine.CellDate {
		t.Fatalf("expected CellDate, got %v", got.Type)
	}
	if got.Display != "14:23:01" {
		t.Fatalf("expected Display to be the plain time (not reformatted with a zero date), got %q", got.Display)
	}
	if got.Raw != "14:23:01" {
		t.Fatalf("expected Raw to be set to the original value, got %v", got.Raw)
	}
}

// TestCellFromRawTimestampParseFailureStillSetsRaw confirms the same
// "Raw silently unset on the fallback path" bug is also fixed for
// TIMESTAMP/DATE/DATETIME when parseAnyTime doesn't recognize the format.
func TestCellFromRawTimestampParseFailureStillSetsRaw(t *testing.T) {
	got := cellFromRaw(sql.RawBytes("not-a-real-timestamp"), "TIMESTAMP")
	if got.Type != engine.CellDate {
		t.Fatalf("expected CellDate, got %v", got.Type)
	}
	if got.Display != "not-a-real-timestamp" {
		t.Fatalf("expected Display to fall back to the raw string, got %q", got.Display)
	}
	if got.Raw != "not-a-real-timestamp" {
		t.Fatalf("expected Raw to still be set on the parse-failure fallback, got %v", got.Raw)
	}
}

// TestCellFromRawTimestampParsesNormally confirms the fix doesn't regress
// the ordinary case: a genuinely parseable TIMESTAMP is still reformatted
// to RFC3339 for Display while Raw keeps the original string.
func TestCellFromRawTimestampParsesNormally(t *testing.T) {
	got := cellFromRaw(sql.RawBytes("2024-03-05 10:30:00"), "TIMESTAMP")
	if got.Type != engine.CellDate {
		t.Fatalf("expected CellDate, got %v", got.Type)
	}
	if got.Display != "2024-03-05T10:30:00Z" {
		t.Fatalf("expected RFC3339 display, got %q", got.Display)
	}
	if got.Raw != "2024-03-05 10:30:00" {
		t.Fatalf("expected Raw to be the original string, got %v", got.Raw)
	}
}
