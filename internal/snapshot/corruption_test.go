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

// TestFSBackendDocRefsCollisionResistant is the regression test for #48:
// sanitize() maps every character outside [A-Za-z0-9._-] to "_", so two
// differently-named collections can sanitize to the same string (e.g.
// Mongo collections "a$b" and "a!b" both become "a_b"). Without a
// collision guard, the second WriteDocRefs call would silently clobber
// the first collection's doc-ref file.
func TestFSBackendDocRefsCollisionResistant(t *testing.T) {
	dir := t.TempDir()
	b, err := newFSBackend(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if sanitize("a$b") != sanitize("a!b") {
		t.Fatalf("test setup invalid: %q and %q don't actually sanitize to the same string", "a$b", "a!b")
	}

	if err := b.WriteDocRefs("m1", "a$b", newSliceDocRefIterator([]DocRef{{ID: "1", Hash: "hash-dollar"}})); err != nil {
		t.Fatalf("WriteDocRefs(a$b): %v", err)
	}
	if err := b.WriteDocRefs("m1", "a!b", newSliceDocRefIterator([]DocRef{{ID: "2", Hash: "hash-bang"}})); err != nil {
		t.Fatalf("WriteDocRefs(a!b): %v", err)
	}

	itDollar, err := b.IterDocRefs("m1", "a$b")
	if err != nil {
		t.Fatal(err)
	}
	gotDollar, err := drainDocRefIterator(itDollar)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotDollar) != 1 || gotDollar[0].Hash != "hash-dollar" {
		t.Fatalf("a$b's doc-refs = %+v, want [{1 hash-dollar}] — got clobbered by a!b's write", gotDollar)
	}

	itBang, err := b.IterDocRefs("m1", "a!b")
	if err != nil {
		t.Fatal(err)
	}
	gotBang, err := drainDocRefIterator(itBang)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotBang) != 1 || gotBang[0].Hash != "hash-bang" {
		t.Fatalf("a!b's doc-refs = %+v, want [{2 hash-bang}]", gotBang)
	}

	if b.docRefsPath("m1", "a$b") == b.docRefsPath("m1", "a!b") {
		t.Fatal("docRefsPath produced the same file path for two differently-named collections")
	}
}

// TestFSBackendIterDocRefsFallsBackToLegacyPath confirms doc-ref data
// written by an older dbhelm build (before the #48 collision fix added a
// hash suffix to the file name) is still readable after upgrading,
// rather than silently appearing as an empty collection.
func TestFSBackendIterDocRefsFallsBackToLegacyPath(t *testing.T) {
	dir := t.TempDir()
	b, err := newFSBackend(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if err := os.MkdirAll(b.docRefsDir("m1"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacyPath := b.legacyDocRefsPath("m1", "widgets")
	legacyContent := `{"id":"a","hash":"legacy-hash"}` + "\n"
	if err := os.WriteFile(legacyPath, []byte(legacyContent), 0o644); err != nil {
		t.Fatal(err)
	}

	it, err := b.IterDocRefs("m1", "widgets")
	if err != nil {
		t.Fatalf("IterDocRefs: %v", err)
	}
	got, err := drainDocRefIterator(it)
	if err != nil {
		t.Fatalf("draining iterator: %v", err)
	}
	if len(got) != 1 || got[0].Hash != "legacy-hash" {
		t.Fatalf("got %+v, want a single ref with hash \"legacy-hash\" read from the legacy path", got)
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

// TestGCPartialDeletionFailureLeavesRecoverableAbandonedManifest is the
// regression test for #53: gcLocked used to delete a pruned snapshot's
// manifest+doc-refs *before* saving the updated index, so a failure
// partway through left the index still claiming the (now
// partially-deleted) snapshot was valid — a state
// recoverAbandonedManifests's unindexed-only check could never clean up.
// gcLocked now saves the index (with the snapshot already removed)
// *before* attempting any file deletion, so a failure here instead
// leaves an unindexed-but-still-present manifest — exactly the shape
// recoverAbandonedManifests already knows how to finish cleaning up on
// the next GC pass.
func TestGCPartialDeletionFailureLeavesRecoverableAbandonedManifest(t *testing.T) {
	withTestScope(t)
	scope, err := scopeDir("gc-partial-fail", "gcdb4")
	if err != nil {
		t.Fatal(err)
	}
	// The fs backend (not bolt) so the doc-ref/manifest deletion failure
	// can be injected via filesystem permissions.
	backend, err := OpenBackend(scope, BackendFS)
	if err != nil {
		t.Fatal(err)
	}

	h, _, err := putOne(backend, []byte(`{"v":"pruned"}`))
	if err != nil {
		t.Fatal(err)
	}
	m := &Manifest{ID: "snap-pruned", Connection: "gc-partial-fail", Database: "gcdb4", CreatedAt: "2026-01-01T00:00:00Z",
		Collections: map[string]CollectionManifest{"widgets": {DocCount: 1}}}
	if err := backend.WriteDocRefs(m.ID, "widgets", newSliceDocRefIterator([]DocRef{{ID: "a", Hash: h}})); err != nil {
		t.Fatal(err)
	}
	if err := saveManifest(scope, m); err != nil {
		t.Fatal(err)
	}
	idx := &scopeIndex{Snapshots: []Summary{{ID: "snap-pruned", CreatedAt: m.CreatedAt, DocCount: 1}}}
	if err := saveIndex(scope, idx); err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}

	// Make the manifests directory read-only so the manifest/doc-ref
	// deletion inside gcLocked fails partway through (permission denied
	// removing entries from it), simulating a mid-deletion failure —
	// disk full, a permissions problem, or (closer to the issue's real
	// motivation) a crash between the two deletion syscalls.
	mdir := manifestsDir(scope)
	if err := os.Chmod(mdir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(mdir, 0o755) }) // restore so t.TempDir() cleanup can remove it

	// KeepLast: 0 prunes the only (untagged) snapshot.
	_, err = GC(GCOptions{Connection: "gc-partial-fail", Database: "gcdb4", KeepLast: 0})
	if err == nil {
		t.Fatal("expected GC to fail deleting from a read-only manifests directory")
	}

	idxAfterFailure, err := loadIndex(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(idxAfterFailure.Snapshots) != 0 {
		t.Fatalf("expected the index to already have dropped the pruned snapshot despite the deletion failure, got %+v", idxAfterFailure.Snapshots)
	}
	if _, err := os.Stat(manifestPath(scope, "snap-pruned")); err != nil {
		t.Fatalf("expected the manifest file to still be present after the failed deletion attempt: %v", err)
	}

	// Restore write access (as if the disk-full/permissions problem
	// resolved itself, or a fresh process starts with normal
	// permissions) and let a subsequent GC pass's abandoned-manifest
	// recovery finish the job.
	if err := os.Chmod(mdir, 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := GC(GCOptions{Connection: "gc-partial-fail", Database: "gcdb4", KeepLast: 0})
	if err != nil {
		t.Fatalf("follow-up GC: %v", err)
	}
	if result.AbandonedRecovered != 1 {
		t.Fatalf("AbandonedRecovered = %d, want 1 (the leftover manifest from the earlier partial failure)", result.AbandonedRecovered)
	}
	if _, err := os.Stat(manifestPath(scope, "snap-pruned")); !os.IsNotExist(err) {
		t.Errorf("expected the leftover manifest to finally be removed, stat err = %v", err)
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
