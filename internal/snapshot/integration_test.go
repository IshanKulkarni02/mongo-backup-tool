package snapshot

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/testmongod"
)

// seedDocs inserts n documents into database/collection, returning nothing —
// callers verify by reading back through the snapshot engine, not by
// tracking documents separately, so the test exercises the real path.
func seedDocs(t *testing.T, ctx context.Context, uri, database, collection string, n int) {
	t.Helper()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	docs := make([]interface{}, n)
	for i := 0; i < n; i++ {
		docs[i] = bson.D{{Key: "n", Value: i}, {Key: "label", Value: fmt.Sprintf("doc-%d", i)}}
	}
	if _, err := client.Database(database).Collection(collection).InsertMany(ctx, docs); err != nil {
		t.Fatalf("seeding %d docs: %v", n, err)
	}
}

func countDocs(t *testing.T, ctx context.Context, uri, database, collection string) int64 {
	t.Helper()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(context.Background())
	n, err := client.Database(database).Collection(collection).CountDocuments(ctx, bson.D{})
	if err != nil {
		t.Fatalf("counting docs: %v", err)
	}
	return n
}

// withTestScope points snapshot.scopeDir at a fresh temp config dir for the
// duration of one test, so integration tests never touch the real
// ~/.mongobak store and never collide with each other or a real CLI/desktop
// session running concurrently.
func withTestScope(t *testing.T) {
	t.Helper()
	t.Setenv("MONGOBAK_CONFIG_DIR", t.TempDir())
}

func TestStandaloneFallbackDegradesGracefully(t *testing.T) {
	withTestScope(t)
	uri := testmongod.Start(t, "") // no replica set: standalone

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	seedDocs(t, ctx, uri, "testdb", "widgets", 50)

	res, err := Create(CreateOptions{
		Connection: "standalone-test",
		URI:        uri,
		Database:   "testdb",
		Message:    "standalone snapshot",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.Consistent {
		t.Errorf("Consistent = true against a standalone mongod, want false (degraded scan)")
	}
	if res.Summary.DocCount != 50 {
		t.Errorf("DocCount = %d, want 50", res.Summary.DocCount)
	}
}

func TestReplicaSetSnapshotIsConsistent(t *testing.T) {
	withTestScope(t)
	uri := testmongod.Start(t, "rs0")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	seedDocs(t, ctx, uri, "testdb", "widgets", 50)

	res, err := Create(CreateOptions{
		Connection: "rs-test",
		URI:        uri,
		Database:   "testdb",
		Message:    "replica set snapshot",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !res.Consistent {
		t.Errorf("Consistent = false against a replica set, want true (readConcern:snapshot)")
	}
	if res.Summary.DocCount != 50 {
		t.Errorf("DocCount = %d, want 50", res.Summary.DocCount)
	}
}

// TestReplicaSetSnapshotIsConsistentUnderConcurrentWrites is the actual
// point-in-time isolation test: it writes to two collections concurrently,
// throughout a snapshot's entire scan window, and proves the snapshot
// reflects one consistent instant across both collections — not a rolling
// scan that could see collA's state from one moment and collB's from a
// later one.
//
// The writer goroutine strictly alternates: write generation N to collA,
// THEN write generation N to collB, repeating as fast as possible for the
// whole scan. Because collA's write for each generation always
// happens-before collB's write for the same generation in real time, a
// truly point-in-time-consistent snapshot can never observe collB's
// generation N without also observing collA's generation N (collA's write
// strictly precedes it). So maxGenSeenInCollB must never exceed
// maxGenSeenInCollA — if it ever does, the scan read the two collections at
// different instants, which is exactly the bug readConcern:snapshot exists
// to prevent. This holds regardless of exact timing/scheduling, so it's not
// a flaky race-to-catch-a-window test.
func TestReplicaSetSnapshotIsConsistentUnderConcurrentWrites(t *testing.T) {
	withTestScope(t)
	uri := testmongod.Start(t, "rs0")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const connName = "consistency-test"
	const dbName = "consistdb"

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	// A little starting bulk in each collection so the scan itself takes
	// measurable time (widening the window the writer goroutine has to
	// land concurrent writes inside).
	seedDocs(t, ctx, uri, dbName, "collA", 40000)
	seedDocs(t, ctx, uri, dbName, "collB", 40000)

	// w:1 with no journal wait — the markers' ordering only needs to be
	// visible to reads, not durable; the default write concern's
	// journal-sync latency (~100ms/write) was the bottleneck limiting how
	// many generations could land during the scan window.
	fastDB := client.Database(dbName, options.Database().SetWriteConcern(writeconcern.W1()))

	stop := make(chan struct{})
	writeErr := make(chan error, 1)
	go func() {
		collA := fastDB.Collection("collA")
		collB := fastDB.Collection("collB")
		gen := 0
		for {
			select {
			case <-stop:
				writeErr <- nil
				return
			default:
			}
			gen++
			if _, err := collA.InsertOne(ctx, bson.D{{Key: "marker", Value: true}, {Key: "gen", Value: gen}}); err != nil {
				writeErr <- fmt.Errorf("writing collA gen %d: %w", gen, err)
				return
			}
			if _, err := collB.InsertOne(ctx, bson.D{{Key: "marker", Value: true}, {Key: "gen", Value: gen}}); err != nil {
				writeErr <- fmt.Errorf("writing collB gen %d: %w", gen, err)
				return
			}
		}
	}()

	res, err := Create(CreateOptions{Connection: connName, URI: uri, Database: dbName, Message: "under concurrent writes"})
	close(stop)
	if werr := <-writeErr; werr != nil {
		t.Fatalf("writer goroutine: %v", werr)
	}
	client.Disconnect(context.Background())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !res.Consistent {
		t.Fatalf("Consistent = false against a replica set, want true — the test below is meaningless without a real snapshot read")
	}

	scope, err := OpenScope(connName, dbName)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()

	maxGenA := maxMarkerGen(t, scope, res.Summary.ID, "collA")
	maxGenB := maxMarkerGen(t, scope, res.Summary.ID, "collB")
	t.Logf("max generation seen: collA=%d collB=%d", maxGenA, maxGenB)

	if maxGenA == 0 {
		t.Fatalf("no marker writes landed in collA during the scan — widen the seed size or the test isn't exercising anything")
	}
	if maxGenB > maxGenA {
		t.Errorf("snapshot saw collB's generation %d without collA's (max %d) — the scan was NOT point-in-time consistent across collections", maxGenB, maxGenA)
	}
}

// maxMarkerGen loads every document in one collection of one snapshot and
// returns the highest "gen" field found among the marker documents
// TestReplicaSetSnapshotIsConsistentUnderConcurrentWrites writes.
func maxMarkerGen(t *testing.T, scope *Scope, manifestID, collection string) int {
	t.Helper()
	source := scope.Source(manifestID)
	it, err := source(collection)
	if err != nil {
		t.Fatalf("opening doc refs for %s: %v", collection, err)
	}
	refs, err := drainDocRefIterator(it)
	if err != nil {
		t.Fatalf("draining doc refs for %s: %v", collection, err)
	}

	max := 0
	for _, ref := range refs {
		data, err := scope.LoadDocument(ref.Hash)
		if err != nil {
			t.Fatalf("loading document %s in %s: %v", ref.ID, collection, err)
		}
		var doc struct {
			Gen int `json:"gen"`
		}
		if err := bson.UnmarshalExtJSON(data, true, &doc); err != nil {
			continue // not a marker doc (one of the seed docs) — no "gen" field
		}
		if doc.Gen > max {
			max = doc.Gen
		}
	}
	return max
}

// TestE2ECreateMutateDiffRestore exercises the full snapshot lifecycle
// against a real mongod: seed, snapshot, mutate, snapshot again, diff (both
// count-only and paginated forms), then restore the first snapshot into a
// fresh database and confirm it exactly reproduces the original content.
func TestE2ECreateMutateDiffRestore(t *testing.T) {
	withTestScope(t)
	uri := testmongod.Start(t, "")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const connName = "e2e-test"
	const dbName = "e2edb"
	seedDocs(t, ctx, uri, dbName, "widgets", 200)

	snap1, err := Create(CreateOptions{Connection: connName, URI: uri, Database: dbName, Message: "snap1"})
	if err != nil {
		t.Fatalf("Create snap1: %v", err)
	}

	// Mutate: modify 10 docs, delete 5, insert 20 new ones.
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	coll := client.Database(dbName).Collection("widgets")
	for i := 0; i < 10; i++ {
		_, err := coll.UpdateOne(ctx, bson.D{{Key: "n", Value: i}}, bson.D{{Key: "$set", Value: bson.D{{Key: "label", Value: "MUTATED"}}}})
		if err != nil {
			t.Fatalf("mutating doc %d: %v", i, err)
		}
	}
	for i := 10; i < 15; i++ {
		if _, err := coll.DeleteOne(ctx, bson.D{{Key: "n", Value: i}}); err != nil {
			t.Fatalf("deleting doc %d: %v", i, err)
		}
	}
	newDocs := make([]interface{}, 20)
	for i := range newDocs {
		newDocs[i] = bson.D{{Key: "n", Value: 1000 + i}, {Key: "label", Value: "NEW"}}
	}
	if _, err := coll.InsertMany(ctx, newDocs); err != nil {
		t.Fatalf("inserting new docs: %v", err)
	}
	client.Disconnect(context.Background())

	snap2, err := Create(CreateOptions{Connection: connName, URI: uri, Database: dbName, Message: "snap2"})
	if err != nil {
		t.Fatalf("Create snap2: %v", err)
	}
	if snap2.Summary.DocCount != 215 { // 200 - 5 + 20
		t.Errorf("snap2 DocCount = %d, want 215", snap2.Summary.DocCount)
	}

	// Diff: counts-only via Compare. The backend (bbolt) can only be open
	// once per process at a time, so this scope is explicitly closed before
	// Restore runs below (which opens its own backend handle on the same
	// scope) rather than held open for the rest of the test.
	scope, err := OpenScope(connName, dbName)
	if err != nil {
		t.Fatalf("OpenScope: %v", err)
	}

	from, err := Get(connName, dbName, snap1.Summary.ID)
	if err != nil {
		t.Fatalf("Get snap1: %v", err)
	}
	to, err := Get(connName, dbName, snap2.Summary.ID)
	if err != nil {
		t.Fatalf("Get snap2: %v", err)
	}
	diff, err := Compare(ctx, from, scope.Source(from.ID), to, scope.Source(to.ID))
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	cd := diff.Collections["widgets"]
	if cd.AddedCount != 20 {
		t.Errorf("AddedCount = %d, want 20", cd.AddedCount)
	}
	if cd.RemovedCount != 5 {
		t.Errorf("RemovedCount = %d, want 5", cd.RemovedCount)
	}
	if cd.ModifiedCount != 10 {
		t.Errorf("ModifiedCount = %d, want 10", cd.ModifiedCount)
	}

	// Diff: paginated IDs for the "added" change type.
	ids, total, err := DiffCollectionPage(ctx, scope.Source(from.ID), scope.Source(to.ID), "widgets", Added, 0, 5)
	if err != nil {
		t.Fatalf("DiffCollectionPage: %v", err)
	}
	if total != 20 {
		t.Errorf("DiffCollectionPage total = %d, want 20", total)
	}
	if len(ids) != 5 {
		t.Errorf("DiffCollectionPage page size = %d, want 5", len(ids))
	}
	if err := scope.Close(); err != nil {
		t.Fatalf("closing diff scope: %v", err)
	}

	// Restore snap1 (the pre-mutation state) into a fresh database and
	// confirm it exactly reproduces the original 200 documents, not the
	// mutated 215.
	restoreResult, err := Restore(RestoreOptions{
		SourceConnection: connName,
		SourceDatabase:   dbName,
		SnapshotID:       snap1.Summary.ID,
		TargetURI:        uri,
		TargetDatabase:   "e2edb_restored",
		Drop:             true,
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restoreResult.DocsWritten != 200 {
		t.Errorf("Restore DocsWritten = %d, want 200", restoreResult.DocsWritten)
	}
	gotCount := countDocs(t, ctx, uri, "e2edb_restored", "widgets")
	if gotCount != 200 {
		t.Errorf("restored collection has %d docs, want 200", gotCount)
	}

	// Spot-check content fidelity: doc 0 should have its ORIGINAL label
	// (not "MUTATED"), since snap1 predates the mutation.
	client2, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client2.Disconnect(context.Background())
	var doc bson.M
	if err := client2.Database("e2edb_restored").Collection("widgets").FindOne(ctx, bson.D{{Key: "n", Value: 0}}).Decode(&doc); err != nil {
		t.Fatalf("finding restored doc 0: %v", err)
	}
	if doc["label"] != "doc-0" {
		t.Errorf("restored doc 0 label = %v, want doc-0 (pre-mutation content)", doc["label"])
	}
}

// TestRestorePreservesIndexOptionsBeyondUniqueSparseTTL confirms restore
// recreates index options beyond the original unique/sparse/TTL-only
// coverage: a partial filter expression, a hidden index, and a custom
// collation. Verified against MongoDB's own reported index list after
// restore, not just "CreateOne didn't error".
func TestRestorePreservesIndexOptionsBeyondUniqueSparseTTL(t *testing.T) {
	withTestScope(t)
	uri := testmongod.Start(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const connName = "idx-test"
	const dbName = "idxdb"

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	coll := client.Database(dbName).Collection("widgets")
	if _, err := coll.InsertOne(ctx, bson.D{{Key: "status", Value: "active"}, {Key: "name", Value: "Widget"}}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	if _, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "status", Value: 1}},
		Options: options.Index().SetName("status_partial").SetPartialFilterExpression(bson.D{{Key: "status", Value: bson.D{{Key: "$eq", Value: "active"}}}}),
	}); err != nil {
		t.Fatalf("creating partial index: %v", err)
	}
	if _, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "name", Value: 1}},
		Options: options.Index().SetName("name_hidden").SetHidden(true),
	}); err != nil {
		t.Fatalf("creating hidden index: %v", err)
	}
	if _, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "name", Value: 1}},
		Options: options.Index().SetName("name_ci").SetCollation(&options.Collation{
			Locale:   "en",
			Strength: 2, // case-insensitive
		}),
	}); err != nil {
		t.Fatalf("creating collation index: %v", err)
	}
	client.Disconnect(context.Background())

	res, err := Create(CreateOptions{Connection: connName, URI: uri, Database: dbName, Message: "with custom indexes"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	restoreResult, err := Restore(RestoreOptions{
		SourceConnection: connName,
		SourceDatabase:   dbName,
		SnapshotID:       res.Summary.ID,
		TargetURI:        uri,
		TargetDatabase:   "idxdb_restored",
		Drop:             true,
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restoreResult.DocsWritten != 1 {
		t.Fatalf("DocsWritten = %d, want 1", restoreResult.DocsWritten)
	}

	client2, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client2.Disconnect(context.Background())

	cur, err := client2.Database("idxdb_restored").Collection("widgets").Indexes().List(ctx)
	if err != nil {
		t.Fatalf("listing restored indexes: %v", err)
	}
	byName := map[string]bson.M{}
	for cur.Next(ctx) {
		var idx bson.M
		if err := cur.Decode(&idx); err != nil {
			t.Fatal(err)
		}
		byName[idx["name"].(string)] = idx
	}

	partial, ok := byName["status_partial"]
	if !ok {
		t.Fatalf("status_partial index missing after restore; got %v", byName)
	}
	if _, ok := partial["partialFilterExpression"]; !ok {
		t.Errorf("status_partial index lost its partialFilterExpression after restore: %+v", partial)
	}

	hidden, ok := byName["name_hidden"]
	if !ok {
		t.Fatalf("name_hidden index missing after restore")
	}
	if v, _ := hidden["hidden"].(bool); !v {
		t.Errorf("name_hidden index lost hidden=true after restore: %+v", hidden)
	}

	collated, ok := byName["name_ci"]
	if !ok {
		t.Fatalf("name_ci index missing after restore")
	}
	collationRaw, ok := collated["collation"]
	if !ok {
		t.Fatalf("name_ci index lost its collation entirely after restore: %+v", collated)
	}
	// Decoded index-list documents can represent nested sub-documents as
	// bson.D or bson.M depending on driver internals; marshal to Extended
	// JSON rather than asserting a specific Go type, to check the actual
	// locale value survived restore.
	collationJSON, err := bson.MarshalExtJSON(collationRaw, false, false)
	if err != nil {
		t.Fatalf("marshaling restored collation: %v", err)
	}
	if !bytes.Contains(collationJSON, []byte(`"locale":"en"`)) {
		t.Errorf("restored collation = %s, want locale \"en\"", collationJSON)
	}
}

// TestRestoreWithSafetyAutoRollsBackPartialFailure forces a multi-collection
// destructive restore to fail partway through (collA succeeds, collB's
// document content is corrupted so its restore fails) and confirms
// RestoreWithSafety automatically restores from the safety snapshot,
// leaving the target exactly as it was before the restore started — not
// half-swapped to the new snapshot's content.
func TestRestoreWithSafetyAutoRollsBackPartialFailure(t *testing.T) {
	withTestScope(t)
	uri := testmongod.Start(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const connName = "rollback-test"
	const dbName = "rollbackdb"

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := client.Database(dbName).Collection("collA").InsertOne(ctx, bson.D{{Key: "v", Value: "snapshot-content-A"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Database(dbName).Collection("collB").InsertOne(ctx, bson.D{{Key: "v", Value: "snapshot-content-B"}}); err != nil {
		t.Fatal(err)
	}
	client.Disconnect(context.Background())

	snap, err := Create(CreateOptions{Connection: connName, URI: uri, Database: dbName, Message: "for rollback test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Corrupt collB's stored content so its restore fails partway through
	// (collA, alphabetically first, will have already been dropped and
	// reinserted by the time this is hit).
	scope, err := scopeDir(connName, dbName)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := OpenBackend(scope, "")
	if err != nil {
		t.Fatal(err)
	}
	it, err := backend.IterDocRefs(snap.Summary.ID, "collB")
	if err != nil {
		t.Fatal(err)
	}
	refs, err := drainDocRefIterator(it)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 doc ref in collB, got %d", len(refs))
	}
	if err := backend.Delete(refs[0].Hash); err != nil {
		t.Fatalf("corrupting collB's content: %v", err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}

	// Put ORIGINAL, pre-restore content into the target database — this is
	// what must survive the failed-then-rolled-back restore.
	client2, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client2.Database("rollbackdb_target").Collection("collA").InsertOne(ctx, bson.D{{Key: "v", Value: "ORIGINAL-pre-restore-A"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client2.Database("rollbackdb_target").Collection("collB").InsertOne(ctx, bson.D{{Key: "v", Value: "ORIGINAL-pre-restore-B"}}); err != nil {
		t.Fatal(err)
	}
	client2.Disconnect(context.Background())

	result, safety, rolledBack, err := RestoreWithSafety(RestoreOptions{
		SourceConnection: connName,
		SourceDatabase:   dbName,
		SnapshotID:       snap.Summary.ID,
		TargetURI:        uri,
		TargetDatabase:   "rollbackdb_target",
		Drop:             true,
	}, connName)
	if err == nil {
		t.Fatalf("expected the restore to fail (collB's content was corrupted), got success: %+v", result)
	}
	if safety == nil {
		t.Fatalf("expected a safety snapshot to have been taken")
	}
	if !rolledBack {
		t.Fatalf("expected rolledBack=true after a partial-failure restore; err was: %v", err)
	}
	if len(result.Collections) != 1 || result.Collections[0] != "collA" {
		t.Errorf("result.Collections = %v, want exactly [collA] (collB should have failed)", result.Collections)
	}

	// The critical assertion: collA, despite having been dropped and
	// reinserted with the snapshot's content BEFORE the failure, must be
	// back to its ORIGINAL pre-restore content after the automatic
	// rollback — not left showing "snapshot-content-A".
	client3, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	defer client3.Disconnect(context.Background())
	var docA bson.M
	if err := client3.Database("rollbackdb_target").Collection("collA").FindOne(ctx, bson.D{}).Decode(&docA); err != nil {
		t.Fatalf("finding collA after rollback: %v", err)
	}
	if docA["v"] != "ORIGINAL-pre-restore-A" {
		t.Errorf("collA content after rollback = %v, want ORIGINAL-pre-restore-A (the rollback should have undone the partial restore)", docA["v"])
	}
	var docB bson.M
	if err := client3.Database("rollbackdb_target").Collection("collB").FindOne(ctx, bson.D{}).Decode(&docB); err != nil {
		t.Fatalf("finding collB after rollback: %v", err)
	}
	if docB["v"] != "ORIGINAL-pre-restore-B" {
		t.Errorf("collB content after rollback = %v, want ORIGINAL-pre-restore-B (collB was never touched by the failed restore, so this also confirms the rollback didn't corrupt untouched data)", docB["v"])
	}
}

// TestRestoreWithSafetyRollsBackWhenFirstCollectionFails is the specific
// regression test for a bug found in review: if the very *first* collection
// processed is dropped and then fails (before it's ever appended to
// RestoreResult.Collections), a rollback-decision based on
// len(Collections) > 0 would see zero completed collections and wrongly
// skip the automatic rollback — even though that first collection's
// original content is already gone. This corrupts collA (alphabetically
// first, so it fails immediately after Drop) rather than collB, to exercise
// exactly that path.
func TestRestoreWithSafetyRollsBackWhenFirstCollectionFails(t *testing.T) {
	withTestScope(t)
	uri := testmongod.Start(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const connName = "rollback-first-test"
	const dbName = "rollbackfirstdb"

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := client.Database(dbName).Collection("collA").InsertOne(ctx, bson.D{{Key: "v", Value: "snapshot-content-A"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Database(dbName).Collection("collB").InsertOne(ctx, bson.D{{Key: "v", Value: "snapshot-content-B"}}); err != nil {
		t.Fatal(err)
	}
	client.Disconnect(context.Background())

	snap, err := Create(CreateOptions{Connection: connName, URI: uri, Database: dbName, Message: "for first-collection rollback test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Corrupt collA's stored content (not collB) — collA is alphabetically
	// first, so Restore drops it, then fails on the very next step, before
	// collA is ever appended to result.Collections.
	scope, err := scopeDir(connName, dbName)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := OpenBackend(scope, "")
	if err != nil {
		t.Fatal(err)
	}
	it, err := backend.IterDocRefs(snap.Summary.ID, "collA")
	if err != nil {
		t.Fatal(err)
	}
	refs, err := drainDocRefIterator(it)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 doc ref in collA, got %d", len(refs))
	}
	if err := backend.Delete(refs[0].Hash); err != nil {
		t.Fatalf("corrupting collA's content: %v", err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}

	client2, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client2.Database("rollbackfirstdb_target").Collection("collA").InsertOne(ctx, bson.D{{Key: "v", Value: "ORIGINAL-pre-restore-A"}}); err != nil {
		t.Fatal(err)
	}
	client2.Disconnect(context.Background())

	result, safety, rolledBack, err := RestoreWithSafety(RestoreOptions{
		SourceConnection: connName,
		SourceDatabase:   dbName,
		SnapshotID:       snap.Summary.ID,
		TargetURI:        uri,
		TargetDatabase:   "rollbackfirstdb_target",
		Drop:             true,
	}, connName)
	if err == nil {
		t.Fatalf("expected the restore to fail (collA's content was corrupted), got success: %+v", result)
	}
	if safety == nil {
		t.Fatalf("expected a safety snapshot to have been taken")
	}
	if len(result.Collections) != 0 {
		t.Fatalf("expected result.Collections to be empty (collA never fully completed), got %v — test setup assumption violated", result.Collections)
	}
	if len(result.Touched) != 1 || result.Touched[0] != "collA" {
		t.Fatalf("result.Touched = %v, want exactly [collA]", result.Touched)
	}
	if !rolledBack {
		t.Fatalf("expected rolledBack=true even though result.Collections was empty — the bug this test targets. err was: %v", err)
	}

	client3, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	defer client3.Disconnect(context.Background())
	var docA bson.M
	if err := client3.Database("rollbackfirstdb_target").Collection("collA").FindOne(ctx, bson.D{}).Decode(&docA); err != nil {
		t.Fatalf("finding collA after rollback: %v", err)
	}
	if docA["v"] != "ORIGINAL-pre-restore-A" {
		t.Errorf("collA content after rollback = %v, want ORIGINAL-pre-restore-A", docA["v"])
	}
}
