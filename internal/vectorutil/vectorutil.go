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
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no numbers found in vector")
	}
	return out, nil
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
	return dot / (math.Sqrt(normA) * math.Sqrt(normB)), nil
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
	return math.Sqrt(sum), nil
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
