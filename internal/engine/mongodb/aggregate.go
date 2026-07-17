package mongodb

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// aggregateMaxDocs caps the number of documents materialized from a
// pipeline result, matching the same defensive bound QueryDocuments uses
// for plain finds.
const aggregateMaxDocs = 200

// Aggregate runs a pipeline (a JSON array of stage documents, Extended
// JSON text) and returns each result document as relaxed Extended JSON.
func (s *Session) Aggregate(ctx context.Context, database, namespace, pipelineJSON string) ([]string, error) {
	var stages []bson.D
	if err := bson.UnmarshalExtJSON([]byte(pipelineJSON), true, &stages); err != nil {
		return nil, fmt.Errorf("invalid pipeline JSON (expected a JSON array of stage objects): %w", err)
	}

	ctx, cancel := opCtx(ctx)
	defer cancel()

	pipeline := make(bson.A, len(stages))
	for i, stage := range stages {
		pipeline[i] = stage
	}

	cur, err := s.client.Database(database).Collection(namespace).Aggregate(ctx, pipeline, options.Aggregate())
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	docs := []string{}
	for cur.Next(ctx) && len(docs) < aggregateMaxDocs {
		var doc bson.D
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		b, err := bson.MarshalExtJSONIndent(doc, false, false, "", "  ")
		if err != nil {
			return nil, err
		}
		docs = append(docs, string(b))
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}
	return docs, nil
}
