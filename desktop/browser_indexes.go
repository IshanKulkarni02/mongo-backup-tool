package main

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// IndexInfo is one index, rendered for display (not round-tripped back into
// a create call — CreateIndex takes its own fresh keysJSON/unique input).
type IndexInfo struct {
	Name     string `json:"name"`
	KeysJSON string `json:"keysJson"`
	Unique   bool   `json:"unique"`
}

// ListIndexes returns every index on a collection.
func (a *App) ListIndexes(connectionName, database, collection string) ([]IndexInfo, error) {
	client, ctx, cancel, err := a.connectBrowser(connectionName)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer client.Disconnect(context.Background())

	cur, err := client.Database(database).Collection(collection).Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	out := []IndexInfo{}
	for cur.Next(ctx) {
		var raw bson.D
		if err := cur.Decode(&raw); err != nil {
			return nil, err
		}
		info := IndexInfo{}
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
func (a *App) CreateIndex(connectionName, database, collection, keysJSON string, unique bool) error {
	var keys bson.D
	if err := bson.UnmarshalExtJSON([]byte(keysJSON), true, &keys); err != nil {
		return fmt.Errorf("invalid index keys JSON: %w", err)
	}
	if len(keys) == 0 {
		return fmt.Errorf("at least one field is required")
	}

	client, ctx, cancel, err := a.connectBrowser(connectionName)
	if err != nil {
		return err
	}
	defer cancel()
	defer client.Disconnect(context.Background())

	model := mongo.IndexModel{Keys: keys}
	if unique {
		model.Options = options.Index().SetUnique(true)
	}
	_, err = client.Database(database).Collection(collection).Indexes().CreateOne(ctx, model)
	return err
}

// DropIndex removes an index by name.
func (a *App) DropIndex(connectionName, database, collection, name string) error {
	client, ctx, cancel, err := a.connectBrowser(connectionName)
	if err != nil {
		return err
	}
	defer cancel()
	defer client.Disconnect(context.Background())

	return client.Database(database).Collection(collection).Indexes().DropOne(ctx, name)
}
