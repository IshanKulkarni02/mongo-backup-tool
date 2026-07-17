package mongodb

import (
	"context"
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
)

const queryMaxLimit = 200

// QueryDocuments runs a filtered, sorted, paginated find.
func (s *Session) QueryDocuments(ctx context.Context, q engine.DocQuery) (engine.DocPage, error) {
	limit := q.Limit
	if limit <= 0 || limit > queryMaxLimit {
		limit = 50
	}
	skip := q.Skip
	if skip < 0 {
		skip = 0
	}

	filter := bson.D{}
	if strings.TrimSpace(q.FilterJSON) != "" {
		if err := bson.UnmarshalExtJSON([]byte(q.FilterJSON), true, &filter); err != nil {
			return engine.DocPage{}, fmt.Errorf("invalid filter JSON: %w", err)
		}
	}
	var sortDoc bson.D
	if strings.TrimSpace(q.SortJSON) != "" {
		if err := bson.UnmarshalExtJSON([]byte(q.SortJSON), true, &sortDoc); err != nil {
			return engine.DocPage{}, fmt.Errorf("invalid sort JSON: %w", err)
		}
	}

	ctx, cancel := opCtx(ctx)
	defer cancel()

	coll := s.client.Database(q.Database).Collection(q.Namespace)

	total, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return engine.DocPage{}, fmt.Errorf("counting documents: %w", err)
	}

	findOpts := options.Find().SetSkip(int64(skip)).SetLimit(int64(limit))
	if len(sortDoc) > 0 {
		findOpts.SetSort(sortDoc)
	}
	cur, err := coll.Find(ctx, filter, findOpts)
	if err != nil {
		return engine.DocPage{}, fmt.Errorf("querying: %w", err)
	}
	defer cur.Close(ctx)

	docs := []string{}
	for cur.Next(ctx) {
		var doc bson.D
		if err := cur.Decode(&doc); err != nil {
			return engine.DocPage{}, err
		}
		b, err := bson.MarshalExtJSONIndent(doc, false, false, "", "  ")
		if err != nil {
			return engine.DocPage{}, err
		}
		docs = append(docs, string(b))
	}
	if err := cur.Err(); err != nil {
		return engine.DocPage{}, err
	}

	return engine.DocPage{Documents: docs, Total: total, Skip: skip, Limit: limit}, nil
}

func parseIDFilter(idJSON string) (bson.D, error) {
	wrapped := fmt.Sprintf(`{"_id":%s}`, idJSON)
	var filter bson.D
	if err := bson.UnmarshalExtJSON([]byte(wrapped), true, &filter); err != nil {
		return nil, fmt.Errorf("invalid document id: %w", err)
	}
	return filter, nil
}

// InsertDocument inserts a new document (Extended JSON text).
func (s *Session) InsertDocument(ctx context.Context, database, namespace, docJSON string) error {
	var doc bson.D
	if err := bson.UnmarshalExtJSON([]byte(docJSON), true, &doc); err != nil {
		return fmt.Errorf("invalid document JSON: %w", err)
	}
	ctx, cancel := opCtx(ctx)
	defer cancel()
	_, err := s.client.Database(database).Collection(namespace).InsertOne(ctx, doc)
	return err
}

// UpdateDocument replaces a document, matched by its original _id.
func (s *Session) UpdateDocument(ctx context.Context, database, namespace, originalIDJSON, docJSON string) error {
	idFilter, err := parseIDFilter(originalIDJSON)
	if err != nil {
		return err
	}
	var doc bson.D
	if err := bson.UnmarshalExtJSON([]byte(docJSON), true, &doc); err != nil {
		return fmt.Errorf("invalid document JSON: %w", err)
	}
	ctx, cancel := opCtx(ctx)
	defer cancel()
	res, err := s.client.Database(database).Collection(namespace).ReplaceOne(ctx, idFilter, doc)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("document not found (it may have been modified or deleted since this page loaded)")
	}
	return nil
}

// DeleteDocument removes one document by _id.
func (s *Session) DeleteDocument(ctx context.Context, database, namespace, idJSON string) error {
	idFilter, err := parseIDFilter(idJSON)
	if err != nil {
		return err
	}
	ctx, cancel := opCtx(ctx)
	defer cancel()
	res, err := s.client.Database(database).Collection(namespace).DeleteOne(ctx, idFilter)
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return fmt.Errorf("document not found")
	}
	return nil
}
