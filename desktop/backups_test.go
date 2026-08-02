package main

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TestCreateBackupConcurrentCallsDoNotLoseIndexEntries guards against #65: a
// severe, concrete manifestation of #22's general unsynchronized
// load-modify-save race, specific to backups.go's runBackup — Wails
// dispatches each CreateBackup call as its own goroutine (jobManager.run
// already does this internally), and a Load-mutate-Save with no locking
// let a slower goroutine's stale read silently clobber a faster goroutine's
// already-saved entry, leaving that backup's archive file on disk but
// permanently absent from index.json. runBackup was fixed to route its
// index append through store.Update (which holds a package-level lock for
// the whole load-mutate-save span) as part of #22 — this proves that fix
// actually closes the race end to end through the real CreateBackup RPC
// and a real mongodump, not just at the store package's own unit-test level.
func TestCreateBackupConcurrentCallsDoNotLoseIndexEntries(t *testing.T) {
	a, uri := newTestAppWithMongoConn(t, "backup-race-test", false)
	jobs := newJobTracker(a)

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(context.Background())
	if _, err := client.Database("racedb").Collection("widgets").InsertOne(context.Background(), bson.M{"n": 1}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	const n = 6
	jobIDs := make([]string, n)
	for i := 0; i < n; i++ {
		id, err := a.CreateBackup("backup-race-test", "racedb")
		if err != nil {
			t.Fatalf("CreateBackup #%d: %v", i, err)
		}
		jobIDs[i] = id
	}

	for i, id := range jobIDs {
		job := jobs.wait(t, id)
		if job.Status != JobDone {
			t.Fatalf("backup job #%d: expected success, got status=%s message=%s", i, job.Status, job.Message)
		}
	}

	backups, err := a.ListBackups()
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(backups) != n {
		t.Fatalf("expected %d backups after %d concurrent CreateBackup calls, got %d — an entry was lost to a race", n, n, len(backups))
	}
}

// TestConcurrentCreateAndDeleteBackupDoNotCorruptIndex guards against the
// other half of #65: CreateBackup and DeleteBackup running concurrently
// (a new backup starting while an older one is being removed) must not
// lose either the newly-created entry or leave the deleted entry's ghost
// behind pointing at a file os.Remove already unlinked.
func TestConcurrentCreateAndDeleteBackupDoNotCorruptIndex(t *testing.T) {
	a, uri := newTestAppWithMongoConn(t, "backup-create-delete-race-test", false)
	jobs := newJobTracker(a)

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(context.Background())
	if _, err := client.Database("racedb").Collection("widgets").InsertOne(context.Background(), bson.M{"n": 1}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	const preexisting = 3
	var toDelete []string
	for i := 0; i < preexisting; i++ {
		id, err := a.CreateBackup("backup-create-delete-race-test", "racedb")
		if err != nil {
			t.Fatalf("seed CreateBackup #%d: %v", i, err)
		}
		job := jobs.wait(t, id)
		if job.Status != JobDone {
			t.Fatalf("seed backup job #%d: expected success, got status=%s message=%s", i, job.Status, job.Message)
		}
		backupID := job.Result.(map[string]string)["backupId"]
		toDelete = append(toDelete, backupID)
	}

	const newlyCreated = 3
	createJobIDs := make([]string, newlyCreated)
	for i := 0; i < newlyCreated; i++ {
		id, err := a.CreateBackup("backup-create-delete-race-test", "racedb")
		if err != nil {
			t.Fatalf("concurrent CreateBackup #%d: %v", i, err)
		}
		createJobIDs[i] = id
	}
	deleteErrs := make(chan error, len(toDelete))
	for _, id := range toDelete {
		id := id
		go func() {
			deleteErrs <- a.DeleteBackup(id)
		}()
	}

	for i, jobID := range createJobIDs {
		job := jobs.wait(t, jobID)
		if job.Status != JobDone {
			t.Fatalf("concurrent backup job #%d: expected success, got status=%s message=%s", i, job.Status, job.Message)
		}
	}
	for i := 0; i < len(toDelete); i++ {
		if err := <-deleteErrs; err != nil {
			t.Fatalf("DeleteBackup: %v", err)
		}
	}

	backups, err := a.ListBackups()
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(backups) != newlyCreated {
		t.Fatalf("expected exactly %d backups (the %d newly created ones, none of the %d deleted), got %d: %+v",
			newlyCreated, newlyCreated, preexisting, len(backups), backups)
	}
	deleted := make(map[string]bool, len(toDelete))
	for _, id := range toDelete {
		deleted[id] = true
	}
	for _, b := range backups {
		if deleted[b.ID] {
			t.Errorf("deleted backup %s reappeared in the index", b.ID)
		}
	}
}
