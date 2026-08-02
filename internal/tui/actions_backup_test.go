package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCleanupOrphanedArchiveRemovesFileAndWrapsCause is the regression
// test for #56: a failed backup-index save after a successful dump used
// to leave the just-written archive on disk with no index entry —
// invisible to Backup: list/restore, but permanently consuming space.
// cleanupOrphanedArchive must remove that file and report the original
// cause.
func TestCleanupOrphanedArchiveRemovesFileAndWrapsCause(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "conn_all_20260101-000000.archive.gz")
	if err := os.WriteFile(archivePath, []byte("fake archive contents"), 0o644); err != nil {
		t.Fatalf("failed to create fake archive: %v", err)
	}

	cause := errors.New("disk full writing backups/index.json")
	err := cleanupOrphanedArchive(archivePath, cause)
	if err == nil {
		t.Fatal("expected cleanupOrphanedArchive to return a non-nil error")
	}
	if !errors.Is(err, cause) {
		t.Fatalf("expected the returned error to wrap the original cause, got: %v", err)
	}
	if !strings.Contains(err.Error(), archivePath) {
		t.Fatalf("expected the error to mention the archive path %q, got: %v", archivePath, err)
	}

	if _, statErr := os.Stat(archivePath); !os.IsNotExist(statErr) {
		t.Fatalf("expected the orphaned archive to be removed, stat error: %v", statErr)
	}
}

// TestCleanupOrphanedArchiveToleratesAlreadyMissingFile confirms a file
// that's already gone (e.g. removed out-of-band) isn't itself treated as
// a cleanup failure — only the original cause should be reported.
func TestCleanupOrphanedArchiveToleratesAlreadyMissingFile(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "does-not-exist.archive.gz")

	cause := errors.New("index save failed")
	err := cleanupOrphanedArchive(archivePath, cause)
	if err == nil {
		t.Fatal("expected a non-nil error")
	}
	if !errors.Is(err, cause) {
		t.Fatalf("expected the returned error to wrap the original cause, got: %v", err)
	}
	if strings.Contains(err.Error(), "failed to remove") {
		t.Fatalf("expected no removal-failure wording for an already-missing file, got: %v", err)
	}
}

// TestCleanupOrphanedArchiveReportsRemovalFailure confirms that when the
// archive can't be removed either, both the original cause and the
// removal failure are surfaced — never just silently dropped.
func TestCleanupOrphanedArchiveReportsRemovalFailure(t *testing.T) {
	dir := t.TempDir()
	// os.Remove refuses to remove a non-empty directory, which is a
	// convenient way to force a removal failure without relying on
	// filesystem permissions (which behave inconsistently across CI
	// environments and platforms).
	archivePath := filepath.Join(dir, "not-a-file.archive.gz")
	if err := os.Mkdir(archivePath, 0o755); err != nil {
		t.Fatalf("failed to create directory standing in for the archive: %v", err)
	}
	if err := os.WriteFile(filepath.Join(archivePath, "child"), []byte("x"), 0o644); err != nil {
		t.Fatalf("failed to create child file: %v", err)
	}

	cause := errors.New("index save failed")
	err := cleanupOrphanedArchive(archivePath, cause)
	if err == nil {
		t.Fatal("expected a non-nil error")
	}
	if !errors.Is(err, cause) {
		t.Fatalf("expected the returned error to still wrap the original cause, got: %v", err)
	}
	if !strings.Contains(err.Error(), "failed to remove") {
		t.Fatalf("expected the error to mention the removal failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), archivePath) {
		t.Fatalf("expected the error to mention the archive path %q, got: %v", archivePath, err)
	}
}
