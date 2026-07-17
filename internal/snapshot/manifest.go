package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// DocRef points at one document's content by its stable _id key and the
// content hash of its canonical encoding at snapshot time.
type DocRef struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}

// IndexSpec captures an index definition well enough to recreate it on restore.
type IndexSpec struct {
	Name    string `json:"name"`
	Keys    bson.D `json:"keys"`
	Options bson.M `json:"options,omitempty"`
}

// CollectionManifest is one collection's state within a snapshot. The
// document list itself (which can be huge) is NOT embedded here — it's
// stored separately via Backend.WriteDocRefs/IterDocRefs in sorted, chunked
// form, so loading a manifest to inspect its metadata never pulls a
// multi-million-entry list into memory.
type CollectionManifest struct {
	Indexes  []IndexSpec `json:"indexes,omitempty"`
	DocCount int         `json:"docCount"`
}

// Manifest is a full snapshot: one point-in-time record of every collection,
// document, and index in a database.
type Manifest struct {
	ID          string                        `json:"id"`
	Connection  string                        `json:"connection"`
	Database    string                        `json:"database"`
	Message     string                        `json:"message"`
	Tags        []string                      `json:"tags,omitempty"`
	CreatedAt   string                        `json:"createdAt"`
	ParentID    string                        `json:"parentId,omitempty"`
	Collections map[string]CollectionManifest `json:"collections"`
}

// DocCount returns the total document count across all collections.
func (m *Manifest) DocCount() int {
	n := 0
	for _, c := range m.Collections {
		n += c.DocCount
	}
	return n
}

func loadManifest(scope, id string) (*Manifest, error) {
	path := manifestPath(scope, id)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading snapshot %s: %w", id, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing snapshot %s: %w", id, err)
	}
	return &m, nil
}

func saveManifest(scope string, m *Manifest) error {
	if err := os.MkdirAll(manifestsDir(scope), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(manifestPath(scope, m.ID), data)
}

func manifestPath(scope, id string) string {
	return manifestsDir(scope) + "/" + id + ".json"
}

func deleteManifest(scope, id string) error {
	err := os.Remove(manifestPath(scope, id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Summary is the lightweight, indexed view of a snapshot used for listing
// history without reading every full manifest.
type Summary struct {
	ID         string   `json:"id"`
	Connection string   `json:"connection"`
	Database   string   `json:"database"`
	Message    string   `json:"message"`
	Tags       []string `json:"tags,omitempty"`
	CreatedAt  string   `json:"createdAt"`
	ParentID   string   `json:"parentId,omitempty"`
	DocCount   int      `json:"docCount"`
	NewObjects int      `json:"newObjects"` // objects written by this snapshot (not deduped)
}

type scopeIndex struct {
	Snapshots []Summary `json:"snapshots"`
}

func loadIndex(scope string) (*scopeIndex, error) {
	data, err := os.ReadFile(indexPath(scope))
	if os.IsNotExist(err) {
		return &scopeIndex{}, nil
	}
	if err != nil {
		return nil, err
	}
	var idx scopeIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parsing snapshot index: %w", err)
	}
	return &idx, nil
}

func saveIndex(scope string, idx *scopeIndex) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(indexPath(scope), data)
}

// writeFileAtomic writes data to a temp file in the same directory as path
// (so the final rename stays on one filesystem, which is what makes it
// atomic), fsyncs it, then renames it into place. A reader — including one
// racing a concurrent writer, or one running right after a process crash —
// only ever observes either the complete previous content or the complete
// new content, never a partially-written or truncated file. This matters
// most for index.json: unlike a manifest.json (which, if lost, just makes
// one snapshot invisible — see recoverAbandonedManifests), a corrupt
// index.json breaks every future read of the whole scope's history.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	// The rename itself is a directory-entry change, which on most POSIX
	// filesystems (ext4 etc., outside certain fully-ordered journaling
	// modes) needs its own fsync — on the directory, not the file — to
	// survive an actual power loss or kernel panic (as opposed to just a
	// killed process, which the rename's atomicity alone already protects
	// against). Best-effort: some platforms/filesystems don't support
	// fsync on a directory descriptor at all, so a failure here doesn't
	// invalidate the write that already completed.
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// Latest returns the most recently created snapshot summary, if any.
func (idx *scopeIndex) Latest() (*Summary, bool) {
	if len(idx.Snapshots) == 0 {
		return nil, false
	}
	best := 0
	for i := range idx.Snapshots {
		if idx.Snapshots[i].CreatedAt > idx.Snapshots[best].CreatedAt {
			best = i
		}
	}
	return &idx.Snapshots[best], true
}

// Find looks up a snapshot summary by ID, or by a unique prefix of it.
func (idx *scopeIndex) Find(idOrPrefix string) (*Summary, bool) {
	for i := range idx.Snapshots {
		if idx.Snapshots[i].ID == idOrPrefix {
			return &idx.Snapshots[i], true
		}
	}
	var match *Summary
	for i := range idx.Snapshots {
		if len(idx.Snapshots[i].ID) >= len(idOrPrefix) && idx.Snapshots[i].ID[:len(idOrPrefix)] == idOrPrefix {
			if match != nil {
				return nil, false // ambiguous prefix
			}
			match = &idx.Snapshots[i]
		}
	}
	if match != nil {
		return match, true
	}
	return nil, false
}

// Sorted returns snapshots ordered oldest-first.
func (idx *scopeIndex) Sorted() []Summary {
	out := make([]Summary, len(idx.Snapshots))
	copy(out, idx.Snapshots)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}
