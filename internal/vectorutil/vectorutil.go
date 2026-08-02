// Package vectorutil parses and compares embedding vectors — pgvector's
// text format, a MongoDB array, or a plain JSON array all look the same
// once stripped to numbers — so a user can paste two cells (biometric face
// embeddings, text embeddings, anything stored as a float array) and see
// how similar the database considers them.
package vectorutil

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Parse reads a vector from its textual representation: a JSON array
// ("[0.12, -0.45, 0.9]", what pgvector's text format and Mongo's Extended
// JSON array both look like) or a bare comma-separated list ("0.12,
// -0.45, 0.9").
func Parse(s string) ([]float64, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil, fmt.Errorf("empty vector")
	}

	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		var out []float64
		if err := json.Unmarshal([]byte(trimmed), &out); err == nil {
			if err := checkFinite(out); err != nil {
				return nil, err
			}
			return out, nil
		}
		trimmed = strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
	}

	parts := strings.Split(trimmed, ",")
	out := make([]float64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number %q in vector: %w", p, err)
		}
		// strconv.ParseFloat (unlike the encoding/json path above)
		// recognizes "Infinity"/"-Infinity"/"NaN" as valid input and
		// happily returns the corresponding non-finite float64 — a
		// downstream value that eventually fails to JSON-marshal when
		// returned across Wails' IPC boundary, which crashes the entire
		// desktop app (a marshal failure there calls a Fatal logger, i.e.
		// os.Exit) instead of surfacing as an ordinary error.
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("vector value %q is not a finite number", p)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no numbers found in vector")
	}
	return out, nil
}

// checkFinite reports an error if any value in vec is NaN or +/-Inf.
func checkFinite(vec []float64) error {
	for _, v := range vec {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("vector contains a non-finite value (NaN or Infinity)")
		}
	}
	return nil
}

// CosineSimilarity returns the cosine similarity of a and b, in [-1, 1]
// (1 = identical direction). Requires equal-length, non-zero vectors.
func CosineSimilarity(a, b []float64) (float64, error) {
	if err := checkDims(a, b); err != nil {
		return 0, err
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0, fmt.Errorf("cannot compute cosine similarity of a zero vector")
	}
	result := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	if math.IsNaN(result) || math.IsInf(result, 0) {
		// Every individual input value can be finite and still land here:
		// large-magnitude but legitimate embedding values can overflow
		// float64 (~1.8e308) once squared and summed in the loop above.
		return 0, fmt.Errorf("cosine similarity is not finite (input values are too large in magnitude)")
	}
	return result, nil
}

// EuclideanDistance returns the straight-line distance between a and b.
// Requires equal-length vectors.
func EuclideanDistance(a, b []float64) (float64, error) {
	if err := checkDims(a, b); err != nil {
		return 0, err
	}
	var sum float64
	for i := range a {
		d := a[i] - b[i]
		sum += d * d
	}
	result := math.Sqrt(sum)
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return 0, fmt.Errorf("euclidean distance is not finite (input values are too large in magnitude)")
	}
	return result, nil
}

func checkDims(a, b []float64) error {
	if len(a) == 0 || len(b) == 0 {
		return fmt.Errorf("vectors must be non-empty")
	}
	if len(a) != len(b) {
		return fmt.Errorf("vectors have different dimensions: %d vs %d", len(a), len(b))
	}
	return nil
}
