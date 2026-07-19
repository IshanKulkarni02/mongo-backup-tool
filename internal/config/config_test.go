package config

import (
	"os"
	"path/filepath"
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
