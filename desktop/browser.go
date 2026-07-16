package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// CollectionInfo is one collection's summary, shown in the browser's tree.
type CollectionInfo struct {
	Name        string `json:"name"`
	DocCount    int64  `json:"docCount"`
	StorageSize int64  `json:"storageSize"`
}

func (a *App) connectBrowser(connectionName string) (*mongo.Client, context.Context, context.CancelFunc, error) {
	conn, err := a.resolveConn(connectionName)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	client, err := mongo.Connect(options.Client().ApplyURI(conn.URI))
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return client, ctx, cancel, nil
}

// ListCollections returns every collection in a database with its document
// count and storage size.
func (a *App) ListCollections(connectionName, database string) ([]CollectionInfo, error) {
	client, ctx, cancel, err := a.connectBrowser(connectionName)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer client.Disconnect(context.Background())

	db := client.Database(database)
	names, err := db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}

	out := make([]CollectionInfo, 0, len(names))
	for _, name := range names {
		var stats bson.M
		if err := db.RunCommand(ctx, bson.D{{Key: "collStats", Value: name}}).Decode(&stats); err != nil {
			out = append(out, CollectionInfo{Name: name})
			continue
		}
		info := CollectionInfo{Name: name}
		if v, ok := stats["count"]; ok {
			info.DocCount = toInt64(v)
		}
		if v, ok := stats["storageSize"]; ok {
			info.StorageSize = toInt64(v)
		}
		out = append(out, info)
	}
	return out, nil
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int32:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return 0
}

// QueryResult is a page of documents, each rendered as relaxed Extended
// JSON (human-readable — plain numbers/strings where unambiguous, unlike
// the snapshot engine's canonical mode which favors deterministic hashing
// over readability).
type QueryResult struct {
	Documents []string `json:"documents"`
	Total     int64    `json:"total"`
	Skip      int      `json:"skip"`
	Limit     int      `json:"limit"`
}

const queryMaxLimit = 200

// QueryDocuments runs a filtered, sorted, paginated find. filterJSON and
// sortJSON are Extended JSON text (or empty for "match everything" /
// "no explicit sort").
func (a *App) QueryDocuments(connectionName, database, collection, filterJSON, sortJSON string, skip, limit int) (QueryResult, error) {
	if limit <= 0 || limit > queryMaxLimit {
		limit = 50
	}
	if skip < 0 {
		skip = 0
	}

	filter := bson.D{}
	if strings.TrimSpace(filterJSON) != "" {
		if err := bson.UnmarshalExtJSON([]byte(filterJSON), true, &filter); err != nil {
			return QueryResult{}, fmt.Errorf("invalid filter JSON: %w", err)
		}
	}
	var sortDoc bson.D
	if strings.TrimSpace(sortJSON) != "" {
		if err := bson.UnmarshalExtJSON([]byte(sortJSON), true, &sortDoc); err != nil {
			return QueryResult{}, fmt.Errorf("invalid sort JSON: %w", err)
		}
	}

	client, ctx, cancel, err := a.connectBrowser(connectionName)
	if err != nil {
		return QueryResult{}, err
	}
	defer cancel()
	defer client.Disconnect(context.Background())

	coll := client.Database(database).Collection(collection)

	total, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return QueryResult{}, fmt.Errorf("counting documents: %w", err)
	}

	findOpts := options.Find().SetSkip(int64(skip)).SetLimit(int64(limit))
	if len(sortDoc) > 0 {
		findOpts.SetSort(sortDoc)
	}
	cur, err := coll.Find(ctx, filter, findOpts)
	if err != nil {
		return QueryResult{}, fmt.Errorf("querying: %w", err)
	}
	defer cur.Close(ctx)

	docs := []string{}
	for cur.Next(ctx) {
		var doc bson.D
		if err := cur.Decode(&doc); err != nil {
			return QueryResult{}, err
		}
		b, err := bson.MarshalExtJSONIndent(doc, false, false, "", "  ")
		if err != nil {
			return QueryResult{}, err
		}
		docs = append(docs, string(b))
	}
	if err := cur.Err(); err != nil {
		return QueryResult{}, err
	}

	return QueryResult{Documents: docs, Total: total, Skip: skip, Limit: limit}, nil
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
func (a *App) InsertDocument(connectionName, database, collection, docJSON string) error {
	var doc bson.D
	if err := bson.UnmarshalExtJSON([]byte(docJSON), true, &doc); err != nil {
		return fmt.Errorf("invalid document JSON: %w", err)
	}

	client, ctx, cancel, err := a.connectBrowser(connectionName)
	if err != nil {
		return err
	}
	defer cancel()
	defer client.Disconnect(context.Background())

	_, err = client.Database(database).Collection(collection).InsertOne(ctx, doc)
	return err
}

// UpdateDocument replaces a document, matched by its original _id (captured
// before editing, so the edit can't accidentally retarget itself by
// changing _id in the textarea).
func (a *App) UpdateDocument(connectionName, database, collection, originalIDJSON, docJSON string) error {
	idFilter, err := parseIDFilter(originalIDJSON)
	if err != nil {
		return err
	}
	var doc bson.D
	if err := bson.UnmarshalExtJSON([]byte(docJSON), true, &doc); err != nil {
		return fmt.Errorf("invalid document JSON: %w", err)
	}

	client, ctx, cancel, err := a.connectBrowser(connectionName)
	if err != nil {
		return err
	}
	defer cancel()
	defer client.Disconnect(context.Background())

	res, err := client.Database(database).Collection(collection).ReplaceOne(ctx, idFilter, doc)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("document not found (it may have been modified or deleted since this page loaded)")
	}
	return nil
}

// DeleteDocument removes one document by _id.
func (a *App) DeleteDocument(connectionName, database, collection, idJSON string) error {
	idFilter, err := parseIDFilter(idJSON)
	if err != nil {
		return err
	}

	client, ctx, cancel, err := a.connectBrowser(connectionName)
	if err != nil {
		return err
	}
	defer cancel()
	defer client.Disconnect(context.Background())

	res, err := client.Database(database).Collection(collection).DeleteOne(ctx, idFilter)
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return fmt.Errorf("document not found")
	}
	return nil
}

// DropCollection permanently deletes a collection and all its documents.
func (a *App) DropCollection(connectionName, database, collection string) error {
	client, ctx, cancel, err := a.connectBrowser(connectionName)
	if err != nil {
		return err
	}
	defer cancel()
	defer client.Disconnect(context.Background())

	return client.Database(database).Collection(collection).Drop(ctx)
}

// CreateCollection explicitly creates an empty collection (Mongo normally
// creates collections implicitly on first insert; this is for when a user
// wants an empty one to exist, e.g. before setting up indexes).
func (a *App) CreateCollection(connectionName, database, collection string) error {
	client, ctx, cancel, err := a.connectBrowser(connectionName)
	if err != nil {
		return err
	}
	defer cancel()
	defer client.Disconnect(context.Background())

	return client.Database(database).CreateCollection(ctx, collection)
}
