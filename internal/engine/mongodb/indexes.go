package mongodb

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
)

// ListIndexes returns every index on a collection.
func (s *Session) ListIndexes(ctx context.Context, database, namespace string) ([]engine.IndexInfo, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()

	cur, err := s.client.Database(database).Collection(namespace).Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	out := []engine.IndexInfo{}
	for cur.Next(ctx) {
		var raw bson.D
		if err := cur.Decode(&raw); err != nil {
			return nil, err
		}
		info := engine.IndexInfo{}
		for _, e := range raw {
			switch e.Key {
			case "name":
				info.Name, _ = e.Value.(string)
			case "key":
				if keys, ok := e.Value.(bson.D); ok {
					b, err := bson.MarshalExtJSON(keys, false, false)
					if err == nil {
						info.KeysJSON = string(b)
					}
				}
			case "unique":
				info.Unique, _ = e.Value.(bool)
			}
		}
		out = append(out, info)
	}
	return out, cur.Err()
}

// CreateIndex builds a new index from Extended JSON keys, e.g. {"email":1}
// or {"location":"2dsphere"}.
func (s *Session) CreateIndex(ctx context.Context, database, namespace, keysJSON string, unique bool) error {
	var keys bson.D
	if err := bson.UnmarshalExtJSON([]byte(keysJSON), true, &keys); err != nil {
		return fmt.Errorf("invalid index keys JSON: %w", err)
	}
	if len(keys) == 0 {
		return fmt.Errorf("at least one field is required")
	}

	ctx, cancel := opCtx(ctx)
	defer cancel()

	model := mongo.IndexModel{Keys: keys}
	if unique {
		model.Options = options.Index().SetUnique(true)
	}
	_, err := s.client.Database(database).Collection(namespace).Indexes().CreateOne(ctx, model)
	return err
}

// DropIndex removes an index by name.
func (s *Session) DropIndex(ctx context.Context, database, namespace, name string) error {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	return s.client.Database(database).Collection(namespace).Indexes().DropOne(ctx, name)
}
