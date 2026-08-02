package mongotools

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// testConnectionTimeout bounds the whole TestConnection call. A var (not
// a const) so tests can shrink it instead of waiting out the real
// default.
var testConnectionTimeout = 10 * time.Second

// connectFunc builds a *mongo.Client from a URI — the step that, for a
// mongodb+srv:// URI, performs synchronous DNS SRV/TXT resolution with no
// context of its own. A var so tests can substitute a deliberately slow
// implementation, proving TestConnection's timeout bounds this step
// without needing real network/DNS access (which resolves failures for a
// bogus host quickly in most environments, making the actual slow-DNS
// failure mode impractical to reproduce hermetically otherwise).
var connectFunc = func(uri string) (*mongo.Client, error) {
	return mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(8 * time.Second))
}

// TestConnection pings the given URI and returns the list of database names
// visible to the connecting user (excluding MongoDB's internal admin/config/local).
//
// The whole operation runs on its own goroutine, bounded by ctx here rather
// than left to run unbounded: for a mongodb+srv:// URI,
// options.Client().ApplyURI performs DNS SRV/TXT resolution synchronously
// via the OS resolver, with no context and no deadline of its own. Without
// this, a slow, unreachable, or misconfigured DNS resolver could block well
// past the intended 10-second test budget — this code previously only
// applied the timeout to the Ping/ListDatabaseNames calls that came after
// ApplyURI had already returned.
func TestConnection(uri string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), testConnectionTimeout)
	defer cancel()

	type result struct {
		names []string
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		names, err := testConnection(ctx, uri)
		ch <- result{names, err}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("connection test timed out after %s (this includes DNS resolution for mongodb+srv:// URIs): %w", testConnectionTimeout, ctx.Err())
	case r := <-ch:
		return r.names, r.err
	}
}

func testConnection(ctx context.Context, uri string) ([]string, error) {
	client, err := connectFunc(uri)
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
