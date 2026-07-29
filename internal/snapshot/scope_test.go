package snapshot

import (
	"os"
	"path/filepath"
	"strings"
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

func configDirForTest(t *testing.T) (string, error) {
	t.Helper()
	return os.Getenv("MONGOBAK_CONFIG_DIR"), nil
}
