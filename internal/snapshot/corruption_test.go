package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBoltBackendRejectsCorruptStoreFile confirms that a truncated/garbage
// store.bolt file fails to open with a clear error, rather than silently
// returning empty/corrupted data or panicking.
func TestBoltBackendRejectsCorruptStoreFile(t *testing.T) {
	dir := t.TempDir()
	// A valid bbolt file starts with a specific page-size-aligned header;
	// a handful of garbage bytes is enough to make it unrecognizable.
	if err := os.WriteFile(filepath.Join(dir, "store.bolt"), []byte("not a real bolt file, just garbage bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := newBoltBackend(dir)
	if err == nil {
		t.Fatal("expected newBoltBackend to fail on a corrupt store file, got nil error")
	}
}

// TestBoltBackendRecoversAfterCleanClose confirms a store can be reopened
// (simulating a process restart after a clean shutdown) and still serves
// previously-written content correctly.
func TestBoltBackendRecoversAfterCleanClose(t *testing.T) {
	dir := t.TempDir()

	b1, err := newBoltBackend(dir)
	if err != nil {
		t.Fatal(err)
	}
	hash, _, err := putOne(b1, []byte(`{"_id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := b1.Close(); err != nil {
		t.Fatal(err)
	}

	b2, err := newBoltBackend(dir)
	if err != nil {
		t.Fatalf("reopening store after clean close: %v", err)
	}
	defer b2.Close()
	got, err := b2.Get(hash)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if string(got) != `{"_id":1}` {
		t.Errorf("Get after reopen = %q, want %q", got, `{"_id":1}`)
	}
}

// TestFSBackendCorruptDocRefLineFailsClearly confirms that a corrupted
// (non-JSON) line in an fs-backend doc-ref file surfaces as an iterator
// error rather than silently skipping the entry or panicking.
func TestFSBackendCorruptDocRefLineFailsClearly(t *testing.T) {
	dir := t.TempDir()
	b, err := newFSBackend(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if err := b.WriteDocRefs("m1", "widgets", newSliceDocRefIterator([]DocRef{{ID: "a", Hash: "h"}})); err != nil {
		t.Fatal(err)
	}

	// Corrupt the written file directly.
	path := b.docRefsPath("m1", "widgets")
	if err := os.WriteFile(path, []byte("not valid json\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	it, err := b.IterDocRefs("m1", "widgets")
	if err != nil {
		t.Fatal(err)
	}
	_, err = drainDocRefIterator(it)
	if err == nil {
		t.Fatal("expected an error reading a corrupted doc-ref line, got nil")
	}
}

// TestGCRemovesUnreferencedObjectsOnly confirms GC deletes only objects no
// longer referenced by any kept snapshot, keeps tagged snapshots regardless
// of KeepLast, and prunes old untagged manifests correctly.
func TestGCRemovesUnreferencedObjectsOnly(t *testing.T) {
	withTestScope(t)
	scope, err := scopeDir("gc-test", "gcdb")
	if err != nil {
		t.Fatal(err)
	}
	backend, err := OpenBackend(scope, BackendBolt)
	if err != nil {
		t.Fatal(err)
	}

	// snapshot 1: doc "a" only
	h1, _, err := putOne(backend, []byte(`{"v":"a"}`))
	if err != nil {
		t.Fatal(err)
	}
	m1 := &Manifest{ID: "snap1", Connection: "gc-test", Database: "gcdb", CreatedAt: "2026-01-01T00:00:00Z",
		Collections: map[string]CollectionManifest{"widgets": {DocCount: 1}}}
	if err := backend.WriteDocRefs(m1.ID, "widgets", newSliceDocRefIterator([]DocRef{{ID: "a", Hash: h1}})); err != nil {
		t.Fatal(err)
	}
	if err := saveManifest(scope, m1); err != nil {
		t.Fatal(err)
	}

	// snapshot 2: doc "a" replaced by doc "b" (h1 becomes unreferenced)
	h2, _, err := putOne(backend, []byte(`{"v":"b"}`))
	if err != nil {
		t.Fatal(err)
	}
	m2 := &Manifest{ID: "snap2", Connection: "gc-test", Database: "gcdb", CreatedAt: "2026-01-02T00:00:00Z", ParentID: "snap1",
		Collections: map[string]CollectionManifest{"widgets": {DocCount: 1}}}
	if err := backend.WriteDocRefs(m2.ID, "widgets", newSliceDocRefIterator([]DocRef{{ID: "a", Hash: h2}})); err != nil {
		t.Fatal(err)
	}
	if err := saveManifest(scope, m2); err != nil {
		t.Fatal(err)
	}

	idx := &scopeIndex{Snapshots: []Summary{
		{ID: "snap1", CreatedAt: m1.CreatedAt, DocCount: 1},
		{ID: "snap2", CreatedAt: m2.CreatedAt, DocCount: 1},
	}}
	if err := saveIndex(scope, idx); err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}

	// KeepLast=1 should prune snap1 (older, untagged) and keep snap2.
	result, err := GC(GCOptions{Connection: "gc-test", Database: "gcdb", KeepLast: 1})
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if result.ManifestsDeleted != 1 {
		t.Errorf("ManifestsDeleted = %d, want 1", result.ManifestsDeleted)
	}
	if result.ObjectsDeleted != 1 {
		t.Errorf("ObjectsDeleted = %d, want 1 (h1, no longer referenced)", result.ObjectsDeleted)
	}

	backend2, err := OpenBackend(scope, "")
	if err != nil {
		t.Fatal(err)
	}
	defer backend2.Close()
	if backend2.Exists(h1) {
		t.Errorf("h1 should have been GC'd (unreferenced after snap1 was pruned)")
	}
	if !backend2.Exists(h2) {
		t.Errorf("h2 should still exist (referenced by kept snap2)")
	}

	idx2, err := loadIndex(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx2.Snapshots) != 1 || idx2.Snapshots[0].ID != "snap2" {
		t.Errorf("index after GC = %+v, want only snap2", idx2.Snapshots)
	}
}

// TestGCRecoversAbandonedManifest simulates a Create() interrupted after
// WriteDocRefs+saveManifest succeeded but before the index was updated
// (e.g. a crash or kill mid-Create) — an orphaned manifest.json plus its
// doc-ref data on disk, referenced by nothing. GC must clean it up without
// disturbing indexed (real) snapshots.
func TestGCRecoversAbandonedManifest(t *testing.T) {
	withTestScope(t)
	scope, err := scopeDir("gc-abandoned", "gcdb3")
	if err != nil {
		t.Fatal(err)
	}
	backend, err := OpenBackend(scope, BackendBolt)
	if err != nil {
		t.Fatal(err)
	}

	// A real, indexed snapshot — must survive.
	hReal, _, err := putOne(backend, []byte(`{"v":"real"}`))
	if err != nil {
		t.Fatal(err)
	}
	real := &Manifest{ID: "snap-real", Connection: "gc-abandoned", Database: "gcdb3", CreatedAt: "2026-01-01T00:00:00Z",
		Collections: map[string]CollectionManifest{"widgets": {DocCount: 1}}}
	if err := backend.WriteDocRefs(real.ID, "widgets", newSliceDocRefIterator([]DocRef{{ID: "a", Hash: hReal}})); err != nil {
		t.Fatal(err)
	}
	if err := saveManifest(scope, real); err != nil {
		t.Fatal(err)
	}
	idx := &scopeIndex{Snapshots: []Summary{{ID: "snap-real", CreatedAt: real.CreatedAt, DocCount: 1}}}
	if err := saveIndex(scope, idx); err != nil {
		t.Fatal(err)
	}

	// An abandoned snapshot: manifest + doc-refs written, but never
	// reflected in the index — simulating the crash window.
	hAbandoned, _, err := putOne(backend, []byte(`{"v":"abandoned"}`))
	if err != nil {
		t.Fatal(err)
	}
	abandoned := &Manifest{ID: "snap-abandoned", Connection: "gc-abandoned", Database: "gcdb3", CreatedAt: "2026-01-02T00:00:00Z",
		Collections: map[string]CollectionManifest{"widgets": {DocCount: 1}}}
	if err := backend.WriteDocRefs(abandoned.ID, "widgets", newSliceDocRefIterator([]DocRef{{ID: "b", Hash: hAbandoned}})); err != nil {
		t.Fatal(err)
	}
	if err := saveManifest(scope, abandoned); err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}

	// Sanity: the abandoned manifest is genuinely invisible via normal
	// lookups before GC runs, same as it would be for a real crash.
	if _, err := Get("gc-abandoned", "gcdb3", "snap-abandoned"); err == nil {
		t.Fatalf("expected the abandoned (unindexed) snapshot to be unreachable via Get before GC")
	}

	result, err := GC(GCOptions{Connection: "gc-abandoned", Database: "gcdb3", KeepLast: 10})
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if result.AbandonedRecovered != 1 {
		t.Errorf("AbandonedRecovered = %d, want 1", result.AbandonedRecovered)
	}
	if result.ManifestsDeleted != 0 {
		t.Errorf("ManifestsDeleted = %d, want 0 (the real snapshot must survive, KeepLast=10)", result.ManifestsDeleted)
	}

	if _, err := os.Stat(manifestPath(scope, "snap-abandoned")); !os.IsNotExist(err) {
		t.Errorf("expected the abandoned manifest.json to be removed, stat err = %v", err)
	}
	if _, err := os.Stat(manifestPath(scope, "snap-real")); err != nil {
		t.Errorf("expected the real manifest.json to survive: %v", err)
	}

	// The real snapshot must still be fully readable after GC.
	m, err := Get("gc-abandoned", "gcdb3", "snap-real")
	if err != nil {
		t.Fatalf("Get(snap-real) after GC: %v", err)
	}
	if m.DocCount() != 1 {
		t.Errorf("real snapshot DocCount after GC = %d, want 1", m.DocCount())
	}
}

// TestGCKeepsTaggedSnapshotsRegardlessOfKeepLast confirms tagged snapshots
// survive GC even when KeepLast would otherwise prune them.
func TestGCKeepsTaggedSnapshotsRegardlessOfKeepLast(t *testing.T) {
	withTestScope(t)
	scope, err := scopeDir("gc-test2", "gcdb2")
	if err != nil {
		t.Fatal(err)
	}
	backend, err := OpenBackend(scope, BackendBolt)
	if err != nil {
		t.Fatal(err)
	}
	h, _, err := putOne(backend, []byte(`{"v":"tagged"}`))
	if err != nil {
		t.Fatal(err)
	}
	m := &Manifest{ID: "snap-tagged", Connection: "gc-test2", Database: "gcdb2", CreatedAt: "2026-01-01T00:00:00Z", Tags: []string{"v1.0"},
		Collections: map[string]CollectionManifest{"widgets": {DocCount: 1}}}
	if err := backend.WriteDocRefs(m.ID, "widgets", newSliceDocRefIterator([]DocRef{{ID: "a", Hash: h}})); err != nil {
		t.Fatal(err)
	}
	if err := saveManifest(scope, m); err != nil {
		t.Fatal(err)
	}
	idx := &scopeIndex{Snapshots: []Summary{{ID: "snap-tagged", CreatedAt: m.CreatedAt, Tags: []string{"v1.0"}, DocCount: 1}}}
	if err := saveIndex(scope, idx); err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}

	// KeepLast=0 would prune everything untagged; the tagged snapshot must survive.
	result, err := GC(GCOptions{Connection: "gc-test2", Database: "gcdb2", KeepLast: 0})
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if result.ManifestsDeleted != 0 {
		t.Errorf("ManifestsDeleted = %d, want 0 (tagged snapshot must be kept)", result.ManifestsDeleted)
	}

	idx2, err := loadIndex(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx2.Snapshots) != 1 {
		t.Errorf("expected the tagged snapshot to survive GC, index = %+v", idx2.Snapshots)
	}
}
