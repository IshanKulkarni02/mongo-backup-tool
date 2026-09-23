package guard

import (
	"context"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/engine/mongodb"
	"github.com/IshanKulkarni02/dbhelm/internal/testmongod"
)

func TestMongoGuardedReads(t *testing.T) {
	uri := testmongod.Start(t, "rs0")
	ctx := context.Background()

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(ctx)
	var docs []any
	for i := 1; i <= 8; i++ {
		docs = append(docs, bson.M{"_id": i, "n": i})
	}
	if _, err := client.Database("g").Collection("c").InsertMany(ctx, docs); err != nil {
		t.Fatal(err)
	}

	sess, err := (mongodb.Engine{}).Open(ctx, engine.ConnConfig{URI: uri})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close(ctx)

	// Documents: limit is clamped and truncation reported.
	page, truncated, err := ReadDocuments(ctx, sess.(engine.DocumentSession),
		engine.DocQuery{Database: "g", Namespace: "c", SortJSON: `{"_id":1}`}, Limits{MaxRows: 3})
	if err != nil || len(page.Documents) != 3 || !truncated {
		t.Fatalf("docs=%d truncated=%v err=%v", len(page.Documents), truncated, err)
	}
	if _, _, err := ReadDocuments(ctx, sess.(engine.DocumentSession),
		engine.DocQuery{Database: "g", Namespace: "c", FilterJSON: `{"$where":"true"}`}, Limits{}); err == nil {
		t.Fatal("$where filter allowed")
	}

	// Aggregation: reads work and are bounded; writes and JS are refused.
	agg := sess.(engine.AggregateSession)
	out, truncated, err := Aggregate(ctx, agg, "g", "c", `[{"$match":{"n":{"$gte":1}}}]`, Limits{MaxRows: 5})
	if err != nil || len(out) != 5 || !truncated {
		t.Fatalf("agg=%d truncated=%v err=%v", len(out), truncated, err)
	}
	for _, bad := range []string{
		`[{"$out":"stolen"}]`,
		`[{"$merge":{"into":"stolen"}}]`,
		`[{"$addFields":{"x":{"$function":{"body":"function(){return 1}","args":[],"lang":"js"}}}}]`,
		`not json`,
	} {
		if _, _, err := Aggregate(ctx, agg, "g", "c", bad, Limits{}); err == nil {
			t.Errorf("pipeline %q allowed", bad)
		}
	}
	names, _ := client.Database("g").ListCollectionNames(ctx, bson.M{})
	if strings.Contains(strings.Join(names, ","), "stolen") {
		t.Fatal("a refused pipeline still created a collection")
	}
}
