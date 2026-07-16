package mongotools

import (
	"context"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TestConnection pings the given URI and returns the list of database names
// visible to the connecting user (excluding MongoDB's internal admin/config/local).
func TestConnection(uri string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(8 * time.Second))
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(context.Background())

	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}

	names, err := client.ListDatabaseNames(ctx, map[string]any{})
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
