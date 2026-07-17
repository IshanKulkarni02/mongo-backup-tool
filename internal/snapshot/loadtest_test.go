package snapshot

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/testmongod"
)

// TestLoadOneMillionDocuments is the Phase-1-style release gate: seed 1M
// documents, snapshot, mutate a subset, snapshot again, diff, and confirm
// both correctness (exact add/modify/remove counts) and bounded memory (peak
// RSS during the diff stays well under a threshold that would only be
// crossed by materializing the full change set in memory).
//
// This is deliberately opt-in and separate from the regular test suite —
// it's slow (real I/O against a real mongod, ~1M documents) and gated behind
// an explicit env var so `go test ./...` stays fast by default:
//
//	MONGOBAK_LOAD_TEST=1 go test ./internal/snapshot/... -run TestLoadOneMillionDocuments -v -timeout 20m
func TestLoadOneMillionDocuments(t *testing.T) {
	if os.Getenv("MONGOBAK_LOAD_TEST") == "" {
		t.Skip("set MONGOBAK_LOAD_TEST=1 to run the 1M-document load test (slow; requires mongod)")
	}
	withTestScope(t)
	uri := testmongod.Start(t, "rs0")

	const n = 1_000_000
	const connName = "load-test"
	const dbName = "loaddb"

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	coll := client.Database(dbName).Collection("widgets")

	seedStart := time.Now()
	const batchSize = 5000
	batch := make([]interface{}, 0, batchSize)
	for i := 0; i < n; i++ {
		batch = append(batch, bson.D{{Key: "n", Value: i}, {Key: "label", Value: fmt.Sprintf("doc-%d", i)}})
		if len(batch) == batchSize {
			if _, err := coll.InsertMany(ctx, batch); err != nil {
				t.Fatalf("seeding batch at doc %d: %v", i, err)
			}
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if _, err := coll.InsertMany(ctx, batch); err != nil {
			t.Fatalf("seeding final batch: %v", err)
		}
	}
	if _, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "n", Value: 1}}}); err != nil {
		t.Fatalf("indexing n: %v", err)
	}
	client.Disconnect(context.Background())
	t.Logf("seeded %d documents in %s", n, time.Since(seedStart))

	snap1Start := time.Now()
	snap1, err := Create(CreateOptions{Connection: connName, URI: uri, Database: dbName, Message: "1M baseline"})
	if err != nil {
		t.Fatalf("Create snap1: %v", err)
	}
	snap1Elapsed := time.Since(snap1Start)
	snap1RSS := peakRSSBytes()
	t.Logf("snap1: %d docs in %s, consistent=%v, peak RSS so far %.1f MB", snap1.Summary.DocCount, snap1Elapsed, snap1.Consistent, float64(snap1RSS)/1e6)
	if snap1.Summary.DocCount != n {
		t.Fatalf("snap1 DocCount = %d, want %d", snap1.Summary.DocCount, n)
	}
	if !snap1.Consistent {
		t.Errorf("snap1.Consistent = false against a replica set, want true")
	}

	// Mutate: modify 15%, delete 5%, insert 2% new — the same shape of
	// mutation the original Phase-1 load test used.
	client2, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	coll2 := client2.Database(dbName).Collection("widgets")

	const modifyCount = n * 15 / 100
	const deleteCount = n * 5 / 100
	const insertCount = n * 2 / 100

	mutateStart := time.Now()
	for i := 0; i < modifyCount; i += batchSize {
		end := i + batchSize
		if end > modifyCount {
			end = modifyCount
		}
		models := make([]mongo.WriteModel, 0, end-i)
		for j := i; j < end; j++ {
			models = append(models, mongo.NewUpdateOneModel().
				SetFilter(bson.D{{Key: "n", Value: j}}).
				SetUpdate(bson.D{{Key: "$set", Value: bson.D{{Key: "label", Value: "MUTATED"}}}}))
		}
		if _, err := coll2.BulkWrite(ctx, models); err != nil {
			t.Fatalf("bulk modify at %d: %v", i, err)
		}
	}
	for i := modifyCount; i < modifyCount+deleteCount; i += batchSize {
		end := i + batchSize
		if end > modifyCount+deleteCount {
			end = modifyCount + deleteCount
		}
		ids := make(bson.A, 0, end-i)
		for j := i; j < end; j++ {
			ids = append(ids, j)
		}
		if _, err := coll2.DeleteMany(ctx, bson.D{{Key: "n", Value: bson.D{{Key: "$in", Value: ids}}}}); err != nil {
			t.Fatalf("bulk delete at %d: %v", i, err)
		}
	}
	newBatch := make([]interface{}, 0, batchSize)
	for i := 0; i < insertCount; i++ {
		newBatch = append(newBatch, bson.D{{Key: "n", Value: n + i}, {Key: "label", Value: "NEW"}})
		if len(newBatch) == batchSize {
			if _, err := coll2.InsertMany(ctx, newBatch); err != nil {
				t.Fatalf("bulk insert at %d: %v", i, err)
			}
			newBatch = newBatch[:0]
		}
	}
	if len(newBatch) > 0 {
		if _, err := coll2.InsertMany(ctx, newBatch); err != nil {
			t.Fatalf("bulk insert final: %v", err)
		}
	}
	client2.Disconnect(context.Background())
	t.Logf("mutated %d modified, %d deleted, %d inserted in %s", modifyCount, deleteCount, insertCount, time.Since(mutateStart))

	snap2Start := time.Now()
	snap2, err := Create(CreateOptions{Connection: connName, URI: uri, Database: dbName, Message: "1M mutated"})
	if err != nil {
		t.Fatalf("Create snap2: %v", err)
	}
	snap2Elapsed := time.Since(snap2Start)
	t.Logf("snap2: %d docs in %s, %d new objects (deduped against snap1)", snap2.Summary.DocCount, snap2Elapsed, snap2.Summary.NewObjects)
	wantDocCount := n - deleteCount + insertCount
	if snap2.Summary.DocCount != wantDocCount {
		t.Errorf("snap2 DocCount = %d, want %d", snap2.Summary.DocCount, wantDocCount)
	}
	// Only genuinely new content should be newly written: modified docs'
	// new label text plus inserted docs. Unmodified/deleted docs' content
	// was already stored by snap1 and must dedup, not double-write.
	wantNewObjects := modifyCount + insertCount
	if snap2.Summary.NewObjects != wantNewObjects {
		t.Errorf("snap2 NewObjects = %d, want %d (dedup should skip unchanged content)", snap2.Summary.NewObjects, wantNewObjects)
	}

	scope, err := OpenScope(connName, dbName)
	if err != nil {
		t.Fatalf("OpenScope: %v", err)
	}
	defer scope.Close()

	from, err := Get(connName, dbName, snap1.Summary.ID)
	if err != nil {
		t.Fatalf("Get snap1: %v", err)
	}
	to, err := Get(connName, dbName, snap2.Summary.ID)
	if err != nil {
		t.Fatalf("Get snap2: %v", err)
	}

	diffStart := time.Now()
	diff, err := Compare(ctx, from, scope.Source(from.ID), to, scope.Source(to.ID))
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	diffElapsed := time.Since(diffStart)
	diffRSS := peakRSSBytes()
	t.Logf("diff: %s, peak RSS so far %.1f MB", diffElapsed, float64(diffRSS)/1e6)

	cd := diff.Collections["widgets"]
	if cd.AddedCount != insertCount {
		t.Errorf("AddedCount = %d, want %d", cd.AddedCount, insertCount)
	}
	if cd.RemovedCount != deleteCount {
		t.Errorf("RemovedCount = %d, want %d", cd.RemovedCount, deleteCount)
	}
	if cd.ModifiedCount != modifyCount {
		t.Errorf("ModifiedCount = %d, want %d", cd.ModifiedCount, modifyCount)
	}

	// Bounded-memory assertion: an absolute RSS ceiling is the wrong
	// measure here — snapshot creation's own baseline (worker-pool zstd
	// codecs, driver connection/cursor buffers, Go's GC headroom under
	// allocation pressure) varies with GOMAXPROCS and Go version, and isn't
	// what's under test. What matters is that comparing two 1M-document
	// snapshots (220,000 total changes: 150,000 modified + 50,000 removed +
	// 20,000 added) doesn't meaningfully grow RSS beyond snap1's own
	// baseline — if Compare (or the snap2 pipeline) regressed to holding
	// full per-collection ID slices instead of streaming through bounded
	// chunks/runs, the delta would be hundreds of MB to low GB (three
	// 220,000-entry string slices alone are tens of MB just for the string
	// headers, before counting the actual ID bytes and the doubling/copying
	// growth pattern of repeated append()); a few hundred MB of headroom is
	// normal GC/runtime variance, not a regression signal.
	if snap1RSS > 0 && diffRSS > 0 {
		delta := diffRSS - snap1RSS
		const deltaCeiling = 400 * 1024 * 1024
		t.Logf("RSS delta from snap1 baseline to after diff: %.1f MB", float64(delta)/1e6)
		if delta > deltaCeiling {
			t.Errorf("RSS grew %.1f MB from snap1 baseline through snap2+diff, exceeding the %.0f MB bounded-memory delta ceiling — may have regressed to full in-memory materialization", float64(delta)/1e6, float64(deltaCeiling)/1e6)
		}
	} else {
		t.Log("peak RSS measurement unavailable on this platform — skipping the memory-boundedness assertion")
	}
}
