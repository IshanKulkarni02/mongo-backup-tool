package snapshot

import "fmt"

// ScopeDir returns the on-disk directory holding a connection+database's
// snapshot data, creating it if needed. Exposed for the remote-sync CLI,
// which operates on the directory directly (git init/add/commit/push).
func ScopeDir(connection, database string) (string, error) {
	return scopeDir(connection, database)
}

// Log returns every snapshot for a connection+database, oldest first.
func Log(connection, database string) ([]Summary, error) {
	scope, err := scopeDir(connection, database)
	if err != nil {
		return nil, err
	}
	idx, err := loadIndex(scope)
	if err != nil {
		return nil, err
	}
	return idx.Sorted(), nil
}

// Get loads a full manifest by snapshot ID (or unique ID prefix).
func Get(connection, database, idOrPrefix string) (*Manifest, error) {
	scope, err := scopeDir(connection, database)
	if err != nil {
		return nil, err
	}
	idx, err := loadIndex(scope)
	if err != nil {
		return nil, err
	}
	summary, ok := idx.Find(idOrPrefix)
	if !ok {
		return nil, fmt.Errorf("no snapshot matching %q for %s/%s", idOrPrefix, connection, database)
	}
	return loadManifest(scope, summary.ID)
}

// Tag adds a label to an existing snapshot. Tagged snapshots are protected
// from GC. Tag never opens a Backend (unlike Create/GC), so — unlike those
// — it has no implicit protection from bbolt's own file lock even for
// bolt-backed scopes; the scope lock below is what actually serializes it
// against a concurrent Create/GC/Tag on the same scope.
func Tag(connection, database, idOrPrefix, tag string) error {
	scope, err := scopeDir(connection, database)
	if err != nil {
		return err
	}
	return withScopeLock(scope, func() error {
		idx, err := loadIndex(scope)
		if err != nil {
			return err
		}
		summary, ok := idx.Find(idOrPrefix)
		if !ok {
			return fmt.Errorf("no snapshot matching %q for %s/%s", idOrPrefix, connection, database)
		}
		for _, t := range summary.Tags {
			if t == tag {
				return nil
			}
		}
		summary.Tags = append(summary.Tags, tag)

		m, err := loadManifest(scope, summary.ID)
		if err != nil {
			return err
		}
		m.Tags = summary.Tags
		if err := saveManifest(scope, m); err != nil {
			return err
		}
		return saveIndex(scope, idx)
	})
}

// Scope opens a connection+database's snapshot backend for reading —
// building docRefSources for Compare, or loading document content by hash.
// A backend (in particular the bbolt one) can only be opened once per
// process at a time, so when diffing two snapshots from the *same* scope
// (the common case), callers must open one Scope and derive both sides from
// it rather than opening a backend per snapshot — that would try to open
// the same bbolt file twice in-process and deadlock on its file lock.
type Scope struct {
	backend Backend
}

// OpenScope opens the backend for one connection+database. Callers must
// Close() it when done.
func OpenScope(connection, database string) (*Scope, error) {
	dir, err := scopeDir(connection, database)
	if err != nil {
		return nil, err
	}
	backend, err := OpenBackend(dir, "")
	if err != nil {
		return nil, err
	}
	return &Scope{backend: backend}, nil
}

func (s *Scope) Close() error { return s.backend.Close() }

// Source returns a docRefSource (for Compare) over one snapshot's stored
// doc-ref lists.
func (s *Scope) Source(manifestID string) docRefSource {
	return func(collection string) (docRefIterator, error) {
		return s.backend.IterDocRefs(manifestID, collection)
	}
}

// LoadDocument retrieves one document's canonical Extended JSON content by
// its content hash, for content-level diff display.
func (s *Scope) LoadDocument(hash string) ([]byte, error) {
	return s.backend.Get(hash)
}
