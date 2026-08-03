package cmd

import (
	"context"
	"sync"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/store"
	"github.com/IshanKulkarni02/dbhelm/internal/testmongod"
)

// TestRunBackupConcurrentCallsDoNotLoseIndexEntries is the regression test
// for #170: RunBackup did a raw store.Load-mutate-store.Save with no
// synchronization, the same unsynchronized-index race #65 fixed for
// desktop's CreateBackup by routing it through store.Update (which holds
// a package-level lock for the whole load-mutate-save span). Two
// concurrent RunBackup calls — e.g. a scheduled backup firing alongside a
// manual `dbhelm backup` invocation — could each load a stale index,
// append their own entry, and have the slower one's save silently
// clobber the faster one's already-saved entry: the archive stays on
// disk but permanently vanishes from index.json.
func TestRunBackupConcurrentCallsDoNotLoseIndexEntries(t *testing.T) {
	t.Setenv("DBHELM_CONFIG_DIR", t.TempDir())
	uri := testmongod.Start(t, "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Connections = append(cfg.Connections, config.Connection{Name: "backup-race-test", URI: uri, Engine: "mongodb"})
	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(context.Background())
	if _, err := client.Database("racedb").Collection("widgets").InsertOne(context.Background(), bson.M{"n": 1}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	const n = 6
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := RunBackup("backup-race-test", "racedb")
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("RunBackup #%d: %v", i, err)
		}
	}

	backupsDir, err := config.BackupsDir()
	if err != nil {
		t.Fatalf("BackupsDir: %v", err)
	}
	idx, err := store.Load(backupsDir)
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if len(idx.Backups) != n {
		t.Fatalf("expected %d backups after %d concurrent RunBackup calls, got %d — an entry was lost to a race", n, n, len(idx.Backups))
	}
}
