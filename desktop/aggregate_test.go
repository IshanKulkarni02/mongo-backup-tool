package main

import (
	"context"
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/testmongod"
)

// newTestAppWithMongoConn registers a connection named connName pointing at
// a real (test) mongod instance, for tests that need RunAggregation's
// actual engine.AggregateSession rather than just the writesData helper.
func newTestAppWithMongoConn(t *testing.T, connName string, readOnly bool) (*App, string) {
	t.Helper()
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
	uri := testmongod.Start(t, "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Connections = append(cfg.Connections, config.Connection{
		Name: connName, URI: uri, Engine: "mongodb", ReadOnly: readOnly,
	})
	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	a := NewApp()
	t.Cleanup(a.engines.Close)
	return a, uri
}

// TestRunAggregationBlocksUnicodeEscapedMergeOnReadOnlyConnection guards
// against #10: a pipeline stage key written with a unicode escape (e.g.
// "\u0024merge", which parses to the real key $merge) must still be
// recognized and blocked as a write on a read-only connection, end to end
// through RunAggregation against a real mongod, not just at the
// writesData unit level.
func TestRunAggregationBlocksUnicodeEscapedMergeOnReadOnlyConnection(t *testing.T) {
	a, uri := newTestAppWithMongoConn(t, "agg-ro-test", true)

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(context.Background())
	if _, err := client.Database("aggdb").Collection("src").InsertOne(context.Background(), bson.M{"n": 1}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	pipeline := `[{"\u0024merge":{"into":"victim"}}]`
	if _, err := a.RunAggregation("agg-ro-test", "aggdb", "src", pipeline); !errors.Is(err, engine.ErrReadOnly) {
		t.Fatalf("RunAggregation = %v, want engine.ErrReadOnly", err)
	}

	count, err := client.Database("aggdb").Collection("victim").CountDocuments(context.Background(), bson.D{})
	if err != nil {
		t.Fatalf("CountDocuments: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected the \u0024merge to be blocked, but victim has %d docs", count)
	}
}

// TestRunAggregationAllowsReadOnReadOnlyConnection confirms the #10 fix
// doesn't regress the legitimate case: a pipeline with no $out/$merge
// stage must still run fine against a read-only connection.
func TestRunAggregationAllowsReadOnReadOnlyConnection(t *testing.T) {
	a, uri := newTestAppWithMongoConn(t, "agg-ro-read-test", true)

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(context.Background())
	if _, err := client.Database("aggdb").Collection("src").InsertOne(context.Background(), bson.M{"n": 1}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	docs, err := a.RunAggregation("agg-ro-read-test", "aggdb", "src", `[{"$match":{}}]`)
	if err != nil {
		t.Fatalf("RunAggregation: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
}

func TestWritesData(t *testing.T) {
	cases := []struct {
		name     string
		pipeline string
		want     bool
	}{
		{"match only", `[{"$match":{"active":true}}]`, false},
		{"group and sort", `[{"$group":{"_id":"$user"}},{"$sort":{"count":-1}}]`, false},
		{"out stage", `[{"$match":{}},{"$out":"summary"}]`, true},
		{"merge stage", `[{"$merge":{"into":"summary"}}]`, true},
		{"empty pipeline", `[]`, false},

		// Regression cases for #10: a unicode-escaped stage key must still
		// be recognized as a write once parsed, even though its raw JSON
		// text contains neither literal substring "$out" nor "$merge".
		{"unicode-escaped merge key", `[{"\u0024merge":{"into":"summary"}}]`, true},
		{"unicode-escaped out key", `[{"\u0024out":"summary"}]`, true},

		// $out/$merge appearing only as an ordinary data value (not a
		// stage key) must NOT be treated as a write.
		{"out as a data value, not a key", `[{"$match":{"tag":"$out"}}]`, false},
		{"merge as a data value, not a key", `[{"$match":{"tag":"$merge"}}]`, false},

		// An unparsable pipeline isn't a write here; Aggregate itself
		// surfaces the parse error.
		{"invalid JSON", `not json`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := writesData(c.pipeline); got != c.want {
				t.Errorf("writesData(%q) = %v, want %v", c.pipeline, got, c.want)
			}
		})
	}
}
