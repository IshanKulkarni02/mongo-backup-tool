package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	_ "github.com/IshanKulkarni02/dbhelm/internal/engine/sqlite"
	"github.com/IshanKulkarni02/dbhelm/internal/snapshot"
)

// jobTracker records every "job:update" a jobManager would have emitted
// to the frontend, so tests can observe a job's terminal state the same
// way the frontend does. jobManager removes a finished job's entry from
// its own map once that update is sent (see #41), so polling the map
// directly after completion no longer works.
type jobTracker struct {
	mu      sync.Mutex
	entries map[string]Job
}

func newJobTracker(a *App) *jobTracker {
	tr := &jobTracker{entries: map[string]Job{}}
	a.jobs.onUpdate = func(j Job) {
		tr.mu.Lock()
		tr.entries[j.ID] = j
		tr.mu.Unlock()
	}
	return tr
}

func (tr *jobTracker) wait(t *testing.T, id string) Job {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		tr.mu.Lock()
		j, ok := tr.entries[id]
		tr.mu.Unlock()
		if ok && j.Status != JobRunning {
			return j
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for job to finish")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func newTestAppWithSQLiteConn(t *testing.T, connName, uri string) (*App, *jobTracker) {
	t.Helper()
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Connections = append(cfg.Connections, config.Connection{
		Name:   connName,
		URI:    uri,
		Engine: "sqlite",
	})
	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	a := NewApp()
	t.Cleanup(a.engines.Close)
	return a, newJobTracker(a)
}

// TestCreateSnapshotDispatchesToSQLForSQLiteConnection exercises the
// engine-capability dispatch added in CreateSnapshot: a SQL connection
// (sqlite here, no Docker needed) must go through snapshot.CreateSQL, not
// the Mongo-only snapshot.Create, which would fail parsing a non-mongodb URI.
func TestCreateSnapshotDispatchesToSQLForSQLiteConnection(t *testing.T) {
	a, jobs := newTestAppWithSQLiteConn(t, "sqlite-dispatch-test", "file::memory:?cache=private")

	sess, release, err := a.sqlSession("sqlite-dispatch-test")
	if err != nil {
		t.Fatalf("sqlSession: %v", err)
	}
	if _, err := sess.Execute(context.Background(), "main", `CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	if _, err := sess.Execute(context.Background(), "main", `INSERT INTO users (email) VALUES ('a@example.com'), ('b@example.com')`); err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	release()

	jobID, err := a.CreateSnapshot("sqlite-dispatch-test", "main", "initial")
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	job := jobs.wait(t, jobID)
	if job.Status != JobDone {
		t.Fatalf("expected job to succeed, got status=%s message=%s", job.Status, job.Message)
	}
	res, ok := job.Result.(*snapshot.CreateResult)
	if !ok {
		t.Fatalf("expected job result to be a *snapshot.CreateResult, got %T", job.Result)
	}
	if res.Summary.DocCount != 2 {
		t.Fatalf("expected 2 rows captured, got %d", res.Summary.DocCount)
	}
	if !res.Consistent {
		t.Fatal("expected a SQL snapshot to always report Consistent=true")
	}
}

// TestRestoreSnapshotDispatchesToSQLForSQLiteConnection exercises the
// RestoreSnapshot dispatch: create a SQL snapshot, mutate the live table,
// restore it, and confirm the original data comes back — proving the
// desktop wire-up correctly reaches snapshot.RestoreSQLWithSafety for a SQL
// connection instead of the Mongo-only RestoreWithSafety.
func TestRestoreSnapshotDispatchesToSQLForSQLiteConnection(t *testing.T) {
	a, jobs := newTestAppWithSQLiteConn(t, "sqlite-restore-dispatch-test", "file::memory:?cache=private")

	sess, release, err := a.sqlSession("sqlite-restore-dispatch-test")
	if err != nil {
		t.Fatalf("sqlSession: %v", err)
	}
	if _, err := sess.Execute(context.Background(), "main", `CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	if _, err := sess.Execute(context.Background(), "main", `INSERT INTO users (id, email) VALUES (1, 'original@example.com')`); err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	release()

	createJobID, err := a.CreateSnapshot("sqlite-restore-dispatch-test", "main", "before change")
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	createJob := jobs.wait(t, createJobID)
	if createJob.Status != JobDone {
		t.Fatalf("expected create job to succeed, got status=%s message=%s", createJob.Status, createJob.Message)
	}
	snapID := createJob.Result.(*snapshot.CreateResult).Summary.ID

	sess, release, err = a.sqlSession("sqlite-restore-dispatch-test")
	if err != nil {
		t.Fatalf("sqlSession: %v", err)
	}
	if _, err := sess.Execute(context.Background(), "main", `UPDATE users SET email = 'changed@example.com' WHERE id = 1`); err != nil {
		t.Fatalf("UPDATE: %v", err)
	}
	release()

	restoreJobID, err := a.RestoreSnapshot("sqlite-restore-dispatch-test", "main", snapID)
	if err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}
	restoreJob := jobs.wait(t, restoreJobID)
	if restoreJob.Status != JobDone {
		t.Fatalf("expected restore job to succeed, got status=%s message=%s", restoreJob.Status, restoreJob.Message)
	}

	sess, release, err = a.sqlSession("sqlite-restore-dispatch-test")
	if err != nil {
		t.Fatalf("sqlSession: %v", err)
	}
	defer release()
	res, err := sess.Query(context.Background(), "main", "SELECT email FROM users WHERE id = 1")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Rows) != 1 || res.Rows[0]["email"].Display != "original@example.com" {
		t.Fatalf("expected the restored email to be the original, got %+v", res.Rows)
	}
}

// TestDiffSnapshotsAgainstLiveDatabaseDoesNotPanic guards against #14:
// DiffSnapshots dereferenced to.ID before checking whether live != nil, so
// the documented "diff against the live database" path (toID == "", which
// openDiffScope always returns as to == nil, live != nil) panicked with a
// nil pointer dereference instead of performing the diff.
func TestDiffSnapshotsAgainstLiveDatabaseDoesNotPanic(t *testing.T) {
	a, uri := newTestAppWithMongoConn(t, "diff-live-test", false)
	jobs := newJobTracker(a)

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(context.Background())
	coll := client.Database("diffdb").Collection("widgets")
	if _, err := coll.InsertOne(context.Background(), bson.M{"n": 1}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	jobID, err := a.CreateSnapshot("diff-live-test", "diffdb", "before change")
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	job := jobs.wait(t, jobID)
	if job.Status != JobDone {
		t.Fatalf("expected create job to succeed, got status=%s message=%s", job.Status, job.Message)
	}
	snapID := job.Result.(*snapshot.CreateResult).Summary.ID

	// Change the live database after the snapshot, so the diff has
	// something real to report.
	if _, err := coll.InsertOne(context.Background(), bson.M{"n": 2}); err != nil {
		t.Fatalf("second insert: %v", err)
	}

	result, err := a.DiffSnapshots("diff-live-test", "diffdb", snapID, "")
	if err != nil {
		t.Fatalf("DiffSnapshots against live database: %v", err)
	}
	if len(result.Collections) != 1 || result.Collections[0].AddedCount != 1 {
		t.Fatalf("expected 1 added doc in widgets, got %+v", result.Collections)
	}

	page, err := a.DiffCollectionChanges("diff-live-test", "diffdb", snapID, "", "widgets", "added", 0, 10)
	if err != nil {
		t.Fatalf("DiffCollectionChanges against live database: %v", err)
	}
	if page.Total != 1 || len(page.IDs) != 1 {
		t.Fatalf("expected 1 added id in widgets, got %+v", page)
	}
}
