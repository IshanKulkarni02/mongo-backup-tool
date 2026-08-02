package snapshot

import (
	"os"
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
