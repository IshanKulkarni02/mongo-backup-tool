// Package mongodb implements the engine.Engine/Session interfaces for
// MongoDB. Importing it (even blank) registers the engine under "mongodb".
// The document/CRUD/index logic here was moved from the desktop app's
// browser bindings so all interfaces share one implementation.
package mongodb

import (
	"context"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
)

func init() {
	engine.Register(Engine{})
}

// opTimeout bounds each individual session operation, matching the 30s
// budget the desktop browser used per call before sessions were cached.
const opTimeout = 30 * time.Second

// connectTimeout bounds Open: server selection is capped well below
// opTimeout so an unreachable host fails fast instead of caching a dead
// session.
const connectTimeout = 10 * time.Second

// Engine is the MongoDB engine factory.
type Engine struct{}

func (Engine) ID() string { return "mongodb" }

func (Engine) Capabilities() engine.Caps {
	return engine.Caps{
		Documents:   true,
		Aggregation: true,
		Snapshots:   true,
	}
}

func (Engine) Open(ctx context.Context, cfg engine.ConnConfig) (engine.Session, error) {
	client, err := mongo.Connect(options.Client().
		ApplyURI(cfg.URI).
		SetServerSelectionTimeout(8 * time.Second))
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		client.Disconnect(context.Background())
		return nil, err
	}
	return &Session{client: client}, nil
}

// Session is a live MongoDB connection. mongo.Client is safe for
// concurrent use, so one Session serves overlapping callers.
type Session struct {
	client *mongo.Client
}

func opCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, opTimeout)
}

func (s *Session) Ping(ctx context.Context) error {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	return s.client.Ping(ctx, nil)
}

func (s *Session) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

// ListDatabases returns user database names, hiding MongoDB's internal
// admin/config/local databases (same filtering as mongotools.TestConnection).
func (s *Session) ListDatabases(ctx context.Context) ([]string, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	names, err := s.client.ListDatabaseNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n == "admin" || n == "config" || n == "local" {
			continue
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

// ListNamespaces returns every collection in a database with its document
// count and storage size.
func (s *Session) ListNamespaces(ctx context.Context, database string) ([]engine.NamespaceInfo, error) {
	ctx, cancel := opCtx(ctx)
	defer cancel()

	db := s.client.Database(database)
	names, err := db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}

	out := make([]engine.NamespaceInfo, 0, len(names))
	for _, name := range names {
		var stats bson.M
		if err := db.RunCommand(ctx, bson.D{{Key: "collStats", Value: name}}).Decode(&stats); err != nil {
			out = append(out, engine.NamespaceInfo{Name: name})
			continue
		}
		info := engine.NamespaceInfo{Name: name}
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

// CreateNamespace explicitly creates an empty collection (Mongo normally
// creates collections implicitly on first insert; this is for when a user
// wants an empty one to exist, e.g. before setting up indexes).
func (s *Session) CreateNamespace(ctx context.Context, database, namespace string) error {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	return s.client.Database(database).CreateCollection(ctx, namespace)
}

// DropNamespace permanently deletes a collection and all its documents.
func (s *Session) DropNamespace(ctx context.Context, database, namespace string) error {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	return s.client.Database(database).Collection(namespace).Drop(ctx)
}
