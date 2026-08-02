package vectorutil

import (
	"math"
	"testing"
)

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestParseJSONArray(t *testing.T) {
	v, err := Parse("[0.1, -0.2, 0.3]")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []float64{0.1, -0.2, 0.3}
	for i := range want {
		if !almostEqual(v[i], want[i]) {
			t.Fatalf("Parse(%v) = %v, want %v", v, v, want)
		}
	}
}

func TestParseCommaSeparated(t *testing.T) {
	v, err := Parse("1, 2, 3")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(v) != 3 || v[0] != 1 || v[1] != 2 || v[2] != 3 {
		t.Fatalf("unexpected parse result: %v", v)
	}
}

func TestParseBracketedCommaSeparated(t *testing.T) {
	// pgvector's text format is "[1,2,3]" but a malformed/truncated paste
	// might not be valid JSON (e.g. trailing comma) — the bracket-stripped
	// fallback should still parse plain numeric content.
	v, err := Parse("[1, 2, 3]")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(v) != 3 {
		t.Fatalf("expected 3 elements, got %v", v)
	}
}

func TestParseEmptyFails(t *testing.T) {
	if _, err := Parse(""); err == nil {
		t.Fatal("expected an error for empty input")
	}
	if _, err := Parse("   "); err == nil {
		t.Fatal("expected an error for whitespace-only input")
	}
}

func TestParseInvalidNumberFails(t *testing.T) {
	if _, err := Parse("1, abc, 3"); err == nil {
		t.Fatal("expected an error for a non-numeric element")
	}
}

func TestCosineSimilarityIdentical(t *testing.T) {
	a := []float64{1, 2, 3}
	sim, err := CosineSimilarity(a, a)
	if err != nil {
		t.Fatalf("CosineSimilarity: %v", err)
	}
	if !almostEqual(sim, 1.0) {
		t.Fatalf("expected cosine similarity 1.0 for identical vectors, got %v", sim)
	}
}

func TestCosineSimilarityOrthogonal(t *testing.T) {
	sim, err := CosineSimilarity([]float64{1, 0}, []float64{0, 1})
	if err != nil {
		t.Fatalf("CosineSimilarity: %v", err)
	}
	if !almostEqual(sim, 0.0) {
		t.Fatalf("expected cosine similarity 0.0 for orthogonal vectors, got %v", sim)
	}
}

func TestCosineSimilarityOpposite(t *testing.T) {
	sim, err := CosineSimilarity([]float64{1, 0}, []float64{-1, 0})
	if err != nil {
		t.Fatalf("CosineSimilarity: %v", err)
	}
	if !almostEqual(sim, -1.0) {
		t.Fatalf("expected cosine similarity -1.0 for opposite vectors, got %v", sim)
	}
}

func TestCosineSimilarityZeroVectorErrors(t *testing.T) {
	if _, err := CosineSimilarity([]float64{0, 0}, []float64{1, 1}); err == nil {
		t.Fatal("expected an error comparing against a zero vector")
	}
}

func TestEuclideanDistance(t *testing.T) {
	d, err := EuclideanDistance([]float64{0, 0}, []float64{3, 4})
	if err != nil {
		t.Fatalf("EuclideanDistance: %v", err)
	}
	if !almostEqual(d, 5.0) {
		t.Fatalf("expected distance 5.0 (3-4-5 triangle), got %v", d)
	}
}

func TestEuclideanDistanceIdenticalIsZero(t *testing.T) {
	a := []float64{1, 2, 3}
	d, err := EuclideanDistance(a, a)
	if err != nil {
		t.Fatalf("EuclideanDistance: %v", err)
	}
	if !almostEqual(d, 0.0) {
		t.Fatalf("expected distance 0.0 for identical vectors, got %v", d)
	}
}

func TestMismatchedDimensionsError(t *testing.T) {
	if _, err := CosineSimilarity([]float64{1, 2}, []float64{1, 2, 3}); err == nil {
		t.Fatal("expected an error for mismatched dimensions in CosineSimilarity")
	}
	if _, err := EuclideanDistance([]float64{1, 2}, []float64{1, 2, 3}); err == nil {
		t.Fatal("expected an error for mismatched dimensions in EuclideanDistance")
	}
}

// TestParseRejectsInfinityAndNaN is the regression test for #46: the
// comma-separated fallback path uses strconv.ParseFloat, which (unlike
// encoding/json) happily accepts "Infinity"/"-Infinity"/"NaN" as valid
// float64 literals. A resulting NaN/Inf value eventually fails to
// JSON-marshal when returned across Wails' IPC boundary, and a marshal
// failure there is a hard os.Exit — crashing the entire desktop app
// instead of surfacing an ordinary validation error. Parse must reject
// these at the source, exactly the repro from the issue:
// Parse("[1, 2, Infinity]") (invalid as JSON, so it falls through to the
// comma-separated path) must error, not silently return [1 2 +Inf].
func TestParseRejectsInfinityAndNaN(t *testing.T) {
	cases := []string{
		"[1, 2, Infinity]",
		"[1, 2, -Infinity]",
		"[1, 2, NaN]",
		"1, 2, Infinity",
		"1, 2, NaN",
	}
	for _, in := range cases {
		if v, err := Parse(in); err == nil {
			t.Fatalf("Parse(%q) = %v, want an error rejecting the non-finite value", in, v)
		}
	}
}

// TestCosineSimilarityRejectsOverflow and
// TestEuclideanDistanceRejectsOverflow are regression tests for the
// second half of #46: even without any literal "Infinity"/"NaN" text,
// individually finite but large-magnitude values can overflow float64
// (~1.8e308) once squared and summed, producing a non-finite result from
// otherwise "valid" input. Both functions must catch this in their own
// output rather than returning NaN/Inf to the caller.
func TestCosineSimilarityRejectsOverflow(t *testing.T) {
	huge := []float64{1e200, 1e200, 1e200}
	if sim, err := CosineSimilarity(huge, huge); err == nil {
		t.Fatalf("CosineSimilarity(huge, huge) = %v, want an error for a non-finite result", sim)
	}
}

func TestEuclideanDistanceRejectsOverflow(t *testing.T) {
	a := []float64{1e200, 1e200, 1e200}
	b := []float64{-1e200, -1e200, -1e200}
	if d, err := EuclideanDistance(a, b); err == nil {
		t.Fatalf("EuclideanDistance(a, b) = %v, want an error for a non-finite result", d)
	}
}
