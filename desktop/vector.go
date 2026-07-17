package main

import "github.com/IshanKulkarni02/mongo-backup-tool/internal/vectorutil"

// VectorComparison is the result of comparing two embedding vectors.
type VectorComparison struct {
	Dimensions int     `json:"dimensions"`
	Cosine     float64 `json:"cosine"`
	Euclidean  float64 `json:"euclidean"`
}

// CompareVectors parses two vectors (pgvector text, a JSON array, or a
// bare comma-separated list — typically pasted from a grid cell) and
// returns their cosine similarity and Euclidean distance, the two
// measures used to tune face-embedding / text-embedding match thresholds.
func (a *App) CompareVectors(vecA, vecB string) (VectorComparison, error) {
	pa, err := vectorutil.Parse(vecA)
	if err != nil {
		return VectorComparison{}, err
	}
	pb, err := vectorutil.Parse(vecB)
	if err != nil {
		return VectorComparison{}, err
	}
	cosine, err := vectorutil.CosineSimilarity(pa, pb)
	if err != nil {
		return VectorComparison{}, err
	}
	euclidean, err := vectorutil.EuclideanDistance(pa, pb)
	if err != nil {
		return VectorComparison{}, err
	}
	return VectorComparison{Dimensions: len(pa), Cosine: cosine, Euclidean: euclidean}, nil
}
