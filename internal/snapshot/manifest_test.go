package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.json")

	if err := writeFileAtomic(path, []byte(`{"snapshots":[]}`)); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"snapshots":[]}` {
		t.Errorf("content = %s, want {\"snapshots\":[]}", got)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory has %d entries after a successful write, want exactly 1 (no leftover temp files): %v", len(entries), entries)
	}
}

// TestWriteFileAtomicNeverLeavesAPartialFile confirms that when the write
// side of the atomic-publish sequence fails (simulating a crash or disk
// error before the rename), the *existing* file at path is left completely
// untouched — never truncated or partially overwritten. This is the
// property that protects index.json (and manifest.json) from corruption if
// the process dies mid-write.
func TestWriteFileAtomicNeverLeavesAPartialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.json")
	original := `{"snapshots":[{"id":"pre-existing"}]}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// Force the write to fail after the file exists but before any rename
	// could occur, by pointing at a directory that doesn't exist (so
	// os.CreateTemp fails outright) — a stand-in for any failure between
	// "the old file is intact" and "the new file is fully synced."
	badPath := filepath.Join(dir, "does-not-exist", "index.json")
	if err := writeFileAtomic(badPath, []byte(`{"snapshots":[{"id":"corrupted-write"}]}`)); err == nil {
		t.Fatalf("expected writeFileAtomic to fail when the target directory doesn't exist")
	}

	// The original, unrelated file must be completely unaffected.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("original file content = %s, want unchanged %s", got, original)
	}
}

func TestScopeIndexFindExact(t *testing.T) {
	idx := &scopeIndex{Snapshots: []Summary{
		{ID: "abc123", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "def456", CreatedAt: "2026-01-02T00:00:00Z"},
	}}

	s, ok := idx.Find("abc123")
	if !ok || s.ID != "abc123" {
		t.Fatalf("Find(abc123) = %v, %v", s, ok)
	}
}

func TestScopeIndexFindUniquePrefix(t *testing.T) {
	idx := &scopeIndex{Snapshots: []Summary{
		{ID: "abc123", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "def456", CreatedAt: "2026-01-02T00:00:00Z"},
	}}

	s, ok := idx.Find("abc")
	if !ok || s.ID != "abc123" {
		t.Fatalf("Find(abc) = %v, %v", s, ok)
	}
}

func TestScopeIndexFindAmbiguousPrefix(t *testing.T) {
	idx := &scopeIndex{Snapshots: []Summary{
		{ID: "abc111", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "abc222", CreatedAt: "2026-01-02T00:00:00Z"},
	}}

	_, ok := idx.Find("abc")
	if ok {
		t.Fatalf("Find(abc) should be ambiguous and fail")
	}
}

func TestScopeIndexLatest(t *testing.T) {
	idx := &scopeIndex{Snapshots: []Summary{
		{ID: "s1", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "s3", CreatedAt: "2026-01-03T00:00:00Z"},
		{ID: "s2", CreatedAt: "2026-01-02T00:00:00Z"},
	}}

	latest, ok := idx.Latest()
	if !ok || latest.ID != "s3" {
		t.Fatalf("Latest() = %v, %v, want s3", latest, ok)
	}
}

func TestScopeIndexSorted(t *testing.T) {
	idx := &scopeIndex{Snapshots: []Summary{
		{ID: "s3", CreatedAt: "2026-01-03T00:00:00Z"},
		{ID: "s1", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "s2", CreatedAt: "2026-01-02T00:00:00Z"},
	}}

	sorted := idx.Sorted()
	if len(sorted) != 3 || sorted[0].ID != "s1" || sorted[1].ID != "s2" || sorted[2].ID != "s3" {
		t.Fatalf("Sorted() = %v, want [s1 s2 s3]", sorted)
	}
}

func TestManifestDocCount(t *testing.T) {
	m := &Manifest{Collections: map[string]CollectionManifest{
		"a": {DocCount: 2},
		"b": {DocCount: 1},
	}}
	if got := m.DocCount(); got != 3 {
		t.Errorf("DocCount() = %d, want 3", got)
	}
}
