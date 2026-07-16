package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// BackendKind selects a scope's storage engine. It's chosen once, the first
// time a scope is created, and remembered via a marker file — every command
// after that (log/diff/restore/gc) opens whichever backend the scope
// already uses without the caller needing to specify it again.
type BackendKind string

const (
	// BackendBolt is the default: a single embedded KV file. Safe against
	// inode exhaustion / directory-listing slowdowns at large scale.
	BackendBolt BackendKind = "bolt"
	// BackendFS is one file per content hash, used only for the Git/Git-LFS
	// remote-sync backend (Git LFS needs individually addressable blobs).
	BackendFS BackendKind = "fs"
)

// Backend bundles a scope's object storage and doc-ref (snapshot document
// list) storage, opened together against the same underlying medium.
type Backend interface {
	ObjectStore

	WriteDocRefs(manifestID, collection string, sorted []DocRef) error
	IterDocRefs(manifestID, collection string) (docRefIterator, error)
	DeleteDocRefs(manifestID string) error
}

type backendMarker struct {
	Kind BackendKind `json:"kind"`
}

func markerPath(scope string) string { return filepath.Join(scope, "backend.json") }

// scopeBackendKind returns the backend a scope was already created with, or
// "" if the scope is brand new (no marker written yet).
func scopeBackendKind(scope string) (BackendKind, error) {
	data, err := os.ReadFile(markerPath(scope))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var m backendMarker
	if err := json.Unmarshal(data, &m); err != nil {
		return "", fmt.Errorf("parsing backend marker for %s: %w", scope, err)
	}
	return m.Kind, nil
}

func writeScopeBackendKind(scope string, kind BackendKind) error {
	data, err := json.MarshalIndent(backendMarker{Kind: kind}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(markerPath(scope), data, 0o644)
}

// OpenBackend opens the backend a scope already uses, or — for a brand-new
// scope — creates one using requestedKind (defaulting to BackendBolt if
// requestedKind is empty).
func OpenBackend(scope string, requestedKind BackendKind) (Backend, error) {
	kind, err := scopeBackendKind(scope)
	if err != nil {
		return nil, err
	}
	if kind == "" {
		kind = requestedKind
		if kind == "" {
			kind = BackendBolt
		}
		if err := writeScopeBackendKind(scope, kind); err != nil {
			return nil, err
		}
	}

	switch kind {
	case BackendFS:
		return newFSBackend(scope)
	case BackendBolt:
		return newBoltBackend(scope)
	default:
		return nil, fmt.Errorf("unknown snapshot backend %q for scope %s", kind, scope)
	}
}

// OpenBackendForRemote ensures a scope uses the fs backend (required for
// Git/Git-LFS remote sync, since LFS needs individually addressable blob
// files) and returns it open. For a brand-new scope this selects fs as the
// backend; for a scope that already uses bolt, it returns a clear error
// rather than attempting a conversion — pick a fresh connection+database
// pairing for a git-backed scope instead.
func OpenBackendForRemote(scope string) (Backend, error) {
	existing, err := scopeBackendKind(scope)
	if err != nil {
		return nil, err
	}
	if existing == BackendBolt {
		return nil, fmt.Errorf("this connection+database's snapshot store already uses the bolt backend, which isn't compatible with Git remote sync — use a fresh connection+database pairing for a git-backed scope")
	}
	return OpenBackend(scope, BackendFS)
}
