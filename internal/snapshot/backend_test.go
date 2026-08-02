package snapshot

import (
	"os"
	"sync"
	"testing"
)

// TestWriteScopeBackendKindLeavesNoTempFiles guards against #50:
// writeScopeBackendKind used a direct os.WriteFile, so a crash mid-write
// could leave backend.json truncated — and unlike a corrupt manifest,
// there's no recovery path for it, so the whole scope becomes
// permanently unopenable. It now goes through writeFileAtomic like
// index.json/manifest.json; this confirms that didn't introduce a stray
// temp file into the scope directory.
func TestWriteScopeBackendKindLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	if err := writeScopeBackendKind(dir, BackendBolt); err != nil {
		t.Fatalf("writeScopeBackendKind: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "backend.json" {
		t.Fatalf("scope dir has %v after writeScopeBackendKind, want exactly backend.json (no leftover temp file)", entries)
	}

	kind, err := scopeBackendKind(dir)
	if err != nil {
		t.Fatalf("scopeBackendKind: %v", err)
	}
	if kind != BackendBolt {
		t.Fatalf("scopeBackendKind = %q, want %q", kind, BackendBolt)
	}
}

// TestOpenBackendPersistsKindAcrossReopen confirms the marker written on
// first use is honored by every later OpenBackend call, without needing
// requestedKind to be repeated.
func TestOpenBackendPersistsKindAcrossReopen(t *testing.T) {
	dir := t.TempDir()

	b1, err := OpenBackend(dir, BackendFS)
	if err != nil {
		t.Fatalf("OpenBackend (create): %v", err)
	}
	if err := b1.Close(); err != nil {
		t.Fatal(err)
	}

	// A second open with no requested kind at all must still resolve to
	// fs, per the marker written by the first call.
	b2, err := OpenBackend(dir, "")
	if err != nil {
		t.Fatalf("OpenBackend (reopen): %v", err)
	}
	defer b2.Close()
	if _, ok := b2.(*fsBackend); !ok {
		t.Fatalf("OpenBackend on reopen returned %T, want *fsBackend", b2)
	}
}

// TestOpenBackendConcurrentFirstOpenAgreesOnOneKind is the regression test
// for #50: a brand-new scope's read-decide-write of backend.json used to
// happen with no lock at all, so two processes racing to be the first to
// open a scope (e.g. `dbhelm remote init` and a scheduled `snapshot
// create` hitting a new connection+database at once) could both decide
// and each atomically write their own kind — the loser's write would
// silently overwrite the winner's, while the loser itself had already
// proceeded to open (and use) a backend of the kind it decided, not the
// kind that ended up persisted. Every concurrent first OpenBackend call
// must agree on exactly one kind, matching what's actually on disk.
// (Every goroutine requests the same kind here so this only exercises
// the decision race, not bbolt's own separate single-writer file lock —
// a different, already-handled concern.)
func TestOpenBackendConcurrentFirstOpenAgreesOnOneKind(t *testing.T) {
	dir := t.TempDir()

	const n = 8
	results := make([]BackendKind, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			b, err := OpenBackend(dir, BackendFS)
			if err != nil {
				errs[i] = err
				return
			}
			defer b.Close()
			if _, ok := b.(*fsBackend); ok {
				results[i] = BackendFS
			}
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: OpenBackend: %v", i, err)
		}
		if results[i] != BackendFS {
			t.Fatalf("goroutine %d opened a %q backend, want every concurrent first-open to agree on %q", i, results[i], BackendFS)
		}
	}

	persisted, err := scopeBackendKind(dir)
	if err != nil {
		t.Fatalf("scopeBackendKind: %v", err)
	}
	if persisted != BackendFS {
		t.Fatalf("persisted backend.json kind = %q, want %q", persisted, BackendFS)
	}
}

// TestScopeBackendKindCorruptMarkerFailsClearly confirms a truncated/
// corrupt backend.json surfaces as a clear parse error rather than being
// silently misinterpreted (e.g. as "no marker yet, use the default").
func TestScopeBackendKindCorruptMarkerFailsClearly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(markerPath(dir), []byte("not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := scopeBackendKind(dir); err == nil {
		t.Fatal("expected scopeBackendKind to fail on a corrupt backend.json, got nil error")
	}
}
