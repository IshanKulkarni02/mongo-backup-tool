package snapshot

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestScopeDirNoCollisionForDifferentUnsafeChars(t *testing.T) {
	withTestScope(t)

	dirA, err := scopeDir("prod/a", "db")
	if err != nil {
		t.Fatal(err)
	}
	dirB, err := scopeDir("prod:a", "db")
	if err != nil {
		t.Fatal(err)
	}
	if dirA == dirB {
		t.Fatalf("scopeDir(\"prod/a\", ...) and scopeDir(\"prod:a\", ...) collided at %s", dirA)
	}
}

func TestScopeDirStableAndIdempotent(t *testing.T) {
	withTestScope(t)

	first, err := scopeDir("conn", "db")
	if err != nil {
		t.Fatal(err)
	}
	second, err := scopeDir("conn", "db")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("scopeDir is not stable across calls: %s vs %s", first, second)
	}
}

func TestScopeDirWritesVerifiableIdentity(t *testing.T) {
	withTestScope(t)

	dir, err := scopeDir("myconn", "mydb")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(identityPath(dir))
	if err != nil {
		t.Fatalf("reading identity.json: %v", err)
	}
	if !strings.Contains(string(data), "myconn") || !strings.Contains(string(data), "mydb") {
		t.Errorf("identity.json = %s, want it to record connection=myconn database=mydb", data)
	}
}

// TestScopeDirMigratesLegacyDirectory confirms a pre-existing scope
// directory created under the old (collision-prone) naming scheme is
// migrated in place — its data isn't silently abandoned in favor of a fresh,
// empty new-style directory.
func TestScopeDirMigratesLegacyDirectory(t *testing.T) {
	withTestScope(t)

	base, err := configDirForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "snapshots")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyDir := filepath.Join(root, "legacyconn__legacydb")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(legacyDir, "index.json")
	if err := os.WriteFile(marker, []byte(`{"snapshots":[{"id":"pre-migration-snapshot"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	dir, err := scopeDir("legacyconn", "legacydb")
	if err != nil {
		t.Fatal(err)
	}
	if dir == legacyDir {
		t.Fatalf("expected migration to the new hash-suffixed name, got the legacy path unchanged: %s", dir)
	}
	migratedMarker, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatalf("expected the legacy directory's index.json to survive migration: %v", err)
	}
	if !strings.Contains(string(migratedMarker), "pre-migration-snapshot") {
		t.Errorf("migrated index.json = %s, want it to still contain pre-migration-snapshot", migratedMarker)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Errorf("expected the legacy directory to no longer exist after migration (renamed, not copied)")
	}
}

// TestScopeDirConcurrentMigrationDoesNotError is the regression test for
// #51: two processes racing to migrate the same not-yet-migrated legacy
// scope directory used to make the loser's os.Rename fail with "no such
// file or directory" (the winner already renamed it away) and
// scopeDir/scopeDirName's caller would then get a hard error even though
// the scope is now perfectly usable at the new path. Simulated here with
// concurrent goroutines racing scopeDir against the same legacy
// directory — the race is on the filesystem, not in-process state, so
// this exercises the exact same code path multiple real processes would.
func TestScopeDirConcurrentMigrationDoesNotError(t *testing.T) {
	withTestScope(t)

	base, err := configDirForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "snapshots")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyDir := filepath.Join(root, "raceconn__racedb")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(legacyDir, "index.json")
	if err := os.WriteFile(marker, []byte(`{"snapshots":[{"id":"pre-migration-snapshot"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	dirs := make(chan string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dir, err := scopeDir("raceconn", "racedb")
			errs <- err
			dirs <- dir
		}()
	}
	wg.Wait()
	close(errs)
	close(dirs)

	for err := range errs {
		if err != nil {
			t.Errorf("scopeDir: %v", err)
		}
	}

	seen := map[string]bool{}
	for dir := range dirs {
		if dir != "" {
			seen[dir] = true
		}
	}
	if len(seen) != 1 {
		t.Fatalf("scopeDir returned %d distinct directories across %d concurrent calls, want exactly 1: %v", len(seen), n, seen)
	}

	var migratedDir string
	for dir := range seen {
		migratedDir = dir
	}
	migratedMarker, err := os.ReadFile(filepath.Join(migratedDir, "index.json"))
	if err != nil {
		t.Fatalf("expected the legacy directory's index.json to survive migration: %v", err)
	}
	if !strings.Contains(string(migratedMarker), "pre-migration-snapshot") {
		t.Errorf("migrated index.json = %s, want it to still contain pre-migration-snapshot", migratedMarker)
	}
}

func configDirForTest(t *testing.T) (string, error) {
	t.Helper()
	return os.Getenv("DBHELM_CONFIG_DIR"), nil
}
