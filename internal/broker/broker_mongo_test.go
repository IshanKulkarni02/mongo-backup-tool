package broker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/IshanKulkarni02/dbhelm/internal/testmongod"
)

// A real mongod: agent reads are bounded, a snapshot can be diffed against
// the live database, and a document write only runs after approval.
func TestMongoAgentFlow(t *testing.T) {
	uri := testmongod.Start(t, "rs0")
	h := newHarness(t, false)
	if env := h.operator("connection.add", map[string]any{"name": "mg", "uri": uri, "engine": "mongodb", "agentAccess": "write"}); !env.OK {
		t.Fatalf("add: %+v", env.Error)
	}
	ctx := context.Background()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(ctx)
	coll := client.Database("shop").Collection("items")
	for i := 1; i <= 5; i++ {
		coll.InsertOne(ctx, bson.M{"_id": i, "n": i})
	}

	q := h.agent("query", map[string]any{"connection": "mg", "database": "shop", "collection": "items", "sort": `{"_id":1}`, "limit": 3})
	if !q.OK || q.Meta.Rows != 3 || !q.Meta.Truncated {
		t.Fatalf("find: %+v %+v", q.Error, q.Meta)
	}
	if env := h.agent("query", map[string]any{"connection": "mg", "database": "shop", "collection": "items", "pipeline": `[{"$out":"x"}]`}); code(env) != CodeReadOnly {
		t.Fatalf("$out allowed: %s", code(env))
	}

	snap := h.agent("snapshot.create", map[string]any{"connection": "mg", "database": "shop", "message": "before"})
	if !snap.OK {
		t.Fatalf("snapshot: %+v", snap.Error)
	}
	raw, _ := json.Marshal(snap.Data)
	var s struct{ ID string }
	json.Unmarshal(raw, &s)
	coll.InsertOne(ctx, bson.M{"_id": 6, "n": 6})
	coll.DeleteOne(ctx, bson.M{"_id": 1})

	d := h.agent("snapshot.diff", map[string]any{"connection": "mg", "database": "shop", "from": s.ID}) // live
	raw, _ = json.Marshal(d.Data)
	if !d.OK || !strings.Contains(string(raw), `"added":1`) || !strings.Contains(string(raw), `"removed":1`) {
		t.Fatalf("live diff: %+v %s", d.Error, raw)
	}

	_, cancel := h.b.Subscribe()
	defer cancel()
	res := h.asyncWrite(map[string]any{"connection": "mg", "database": "shop", "collection": "items", "op": "insert", "document": `{"_id": 7, "n": 7}`, "wait": 30})
	id := h.pendingID()
	if n, _ := coll.CountDocuments(ctx, bson.M{"_id": 7}); n != 0 {
		t.Fatal("document inserted before approval")
	}
	h.operator("approvals.decide", map[string]any{"id": id, "approve": true})
	if env := <-res; !env.OK {
		t.Fatalf("doc write: %+v", env.Error)
	}
	if n, _ := coll.CountDocuments(ctx, bson.M{"_id": 7}); n != 1 {
		t.Fatal("approved document write did not run")
	}
}
