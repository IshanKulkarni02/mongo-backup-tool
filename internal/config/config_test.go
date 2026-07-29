package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestMigrateLegacyDirCopiesOldConfigIntoNew(t *testing.T) {
	base := t.TempDir()
	oldDir := filepath.Join(base, "mongobak")
	newDir := filepath.Join(base, "dbhelm")

	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatalf("seeding old dir: %v", err)
	}
	want := `{"connections":[{"name":"local","uri":"mongodb://localhost:27017","createdAt":"now"}]}`
	if err := os.WriteFile(filepath.Join(oldDir, "config.json"), []byte(want), 0o600); err != nil {
		t.Fatalf("seeding old config.json: %v", err)
	}

	if err := migrateLegacyDir(base, newDir); err != nil {
		t.Fatalf("migrateLegacyDir: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(newDir, "config.json"))
	if err != nil {
		t.Fatalf("reading migrated config.json: %v", err)
	}
	if string(got) != want {
		t.Fatalf("migrated content mismatch: got %q, want %q", got, want)
	}

	if _, err := os.Stat(filepath.Join(oldDir, "config.json")); err != nil {
		t.Fatalf("expected old config.json to remain as a safety net, got: %v", err)
	}
}

func TestMigrateLegacyDirNoOldDirIsNoOp(t *testing.T) {
	base := t.TempDir()
	newDir := filepath.Join(base, "dbhelm")

	if err := migrateLegacyDir(base, newDir); err != nil {
		t.Fatalf("migrateLegacyDir: %v", err)
	}
	if _, err := os.Stat(newDir); !os.IsNotExist(err) {
		t.Fatalf("expected no new dir to be created when there's nothing to migrate, stat err: %v", err)
	}
}

// TestUpdateSerializesConcurrentWrites guards against #22: a plain
// Load-mutate-Save per call site lets two goroutines racing on the same
// config.json silently lose one's write (whichever Save lands second
// wins). Update must serialize the whole load-mutate-save sequence so
// every concurrent caller's change survives.
func TestUpdateSerializesConcurrentWrites(t *testing.T) {
	withTempConfigDir(t)

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := Update(func(cfg *Config) error {
				cfg.Upsert(Connection{Name: fmt.Sprintf("conn-%d", i), URI: "mongodb://localhost", CreatedAt: "now"})
				return nil
			})
			if err != nil {
				t.Errorf("Update: %v", err)
			}
		}(i)
	}
	wg.Wait()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Connections) != n {
		t.Fatalf("expected %d connections after %d concurrent Updates, got %d", n, n, len(cfg.Connections))
	}
	for i := 0; i < n; i++ {
		if _, ok := cfg.Find(fmt.Sprintf("conn-%d", i)); !ok {
			t.Errorf("connection conn-%d is missing — its Update was lost to a race", i)
		}
	}
}

// TestUpdateSkipsSaveOnErrNoChange guards the errNoChange escape hatch
// (used by MigrateCredentials): mutate signaling "nothing to do" must not
// be surfaced as an error, and must not touch the file.
func TestUpdateSkipsSaveOnErrNoChange(t *testing.T) {
	withTempConfigDir(t)

	if err := Save(&Config{Connections: []Connection{{Name: "seed", CreatedAt: "now"}}}); err != nil {
		t.Fatalf("seeding Save: %v", err)
	}
	path, err := filePath()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := Update(func(cfg *Config) error { return errNoChange }); err != nil {
		t.Fatalf("Update with errNoChange should not return an error, got: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("expected the file to be untouched when mutate returns errNoChange")
	}
}

// TestUpdatePropagatesMutateError guards that a real error from mutate
// (e.g. "no connection named X") aborts without saving and is returned to
// the caller, not swallowed like errNoChange.
func TestUpdatePropagatesMutateError(t *testing.T) {
	withTempConfigDir(t)

	wantErr := fmt.Errorf("boom")
	err := Update(func(cfg *Config) error { return wantErr })
	if err != wantErr {
		t.Fatalf("Update = %v, want %v", err, wantErr)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Connections) != 0 {
		t.Fatalf("expected no file to have been written, got %+v", cfg.Connections)
	}
}

// TestSaveWriteIsAtomic guards against #22's other half: a direct
// os.WriteFile can leave config.json truncated/corrupt if the process
// dies mid-write. Save must go through a temp file + rename, leaving no
// stray temp file behind and always a fully-formed JSON file in place.
func TestSaveWriteIsAtomic(t *testing.T) {
	withTempConfigDir(t)

	if err := Save(&Config{Connections: []Connection{{Name: "a", CreatedAt: "now"}}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("stray temp file left behind after Save: %s", e.Name())
		}
	}
}

func TestMigrateLegacyDirSkipsWhenNewDirAlreadyExists(t *testing.T) {
	base := t.TempDir()
	oldDir := filepath.Join(base, "mongobak")
	newDir := filepath.Join(base, "dbhelm")

	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatalf("seeding old dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "config.json"), []byte(`{"old":true}`), 0o600); err != nil {
		t.Fatalf("seeding old config.json: %v", err)
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatalf("seeding new dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "config.json"), []byte(`{"new":true}`), 0o600); err != nil {
		t.Fatalf("seeding new config.json: %v", err)
	}

	if err := migrateLegacyDir(base, newDir); err != nil {
		t.Fatalf("migrateLegacyDir: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(newDir, "config.json"))
	if err != nil {
		t.Fatalf("reading new config.json: %v", err)
	}
	if string(got) != `{"new":true}` {
		t.Fatalf("expected existing new-dir content to be left untouched, got %q", got)
	}
}
