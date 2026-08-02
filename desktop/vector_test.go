package main

import (
	"encoding/json"
	"testing"
)

// TestCompareVectorsRejectsNonFiniteInput is the regression test for #46:
// pasting Vector A = "[1, 2, Infinity]" and Vector B = "[1, 2, 3]" into
// the Vector Compare tool used to compute successfully — cosine
// similarity NaN, Euclidean distance +Inf — and Wails' IPC layer then
// crashed the entire desktop app (all open connections, unsaved query
// tabs, etc. lost) trying to JSON-marshal the response, since
// encoding/json can't marshal NaN/Inf floats. CompareVectors must now
// return an ordinary error instead of ever reaching that state.
func TestCompareVectorsRejectsNonFiniteInput(t *testing.T) {
	a := &App{}
	if _, err := a.CompareVectors("[1, 2, Infinity]", "[1, 2, 3]"); err == nil {
		t.Fatal("expected CompareVectors to reject a non-finite input value, got success")
	}
}

// TestCompareVectorsResultAlwaysMarshals confirms the invariant Wails'
// IPC layer depends on: whenever CompareVectors succeeds, its result
// must be safely JSON-marshalable — the exact property that failed
// before this fix and crashed the app via Wails' Fatal-on-marshal-error
// path.
func TestCompareVectorsResultAlwaysMarshals(t *testing.T) {
	a := &App{}
	result, err := a.CompareVectors("[1, 2, 3]", "[4, 5, 6]")
	if err != nil {
		t.Fatalf("CompareVectors: %v", err)
	}
	if _, err := json.Marshal(result); err != nil {
		t.Fatalf("result failed to JSON-marshal (this is exactly the failure mode that crashes the desktop app via Wails' IPC layer): %v", err)
	}
}

// TestCompareVectorsRejectsOverflowingMagnitudes confirms the
// non-literal-text half of #46: individually finite but very
// large-magnitude values whose squared sum overflows float64 in
// EuclideanDistance/CosineSimilarity must also be rejected as an error,
// not returned as a non-finite result.
func TestCompareVectorsRejectsOverflowingMagnitudes(t *testing.T) {
	a := &App{}
	huge := "[1e200, 1e200, 1e200]"
	if _, err := a.CompareVectors(huge, huge); err == nil {
		t.Fatal("expected CompareVectors to reject an overflowing magnitude, got success")
	}
}
