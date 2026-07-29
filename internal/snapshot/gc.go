package snapshot

import (
	"os"
	"path/filepath"
	"strings"
)

// GCOptions configures pruning old snapshots for one connection+database.
type GCOptions struct {
	Connection string
	Database   string
	KeepLast   int // keep the N most recent snapshots regardless of tags; 0 keeps only tagged ones
}

// GCResult reports what a GC pass removed.
type GCResult struct {
	ManifestsDeleted   int `json:"manifestsDeleted"`
	ObjectsDeleted     int `json:"objectsDeleted"`
	AbandonedRecovered int `json:"abandonedRecovered"` // orphaned manifests from an interrupted Create(), cleaned up
}

// recoverAbandonedManifests removes manifest.json files (and their
// corresponding doc-ref data) that exist on disk but aren't referenced by
// the index — the signature of a Create() interrupted (crash, kill,
// power loss) after WriteDocRefs and saveManifest succeeded but before the
// index was updated to include the new snapshot. Such a manifest was never
// visible through Log/Get/Find (those only ever consult the index), so this
// is pure space reclamation, not a correctness fix — but left unswept
// indefinitely, a machine that crashes mid-snapshot repeatedly would
// accumulate orphaned data forever.
func recoverAbandonedManifests(scope string, backend Backend, idx *scopeIndex) (int, error) {
	indexed := make(map[string]bool, len(idx.Snapshots))
	for _, s := range idx.Snapshots {
		indexed[s.ID] = true
	}

	entries, err := os.ReadDir(manifestsDir(scope))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	recovered := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if indexed[id] {
			continue
		}
		if err := os.Remove(filepath.Join(manifestsDir(scope), e.Name())); err != nil && !os.IsNotExist(err) {
			return recovered, err
		}
		if err := backend.DeleteDocRefs(id); err != nil {
			return recovered, err
		}
		recovered++
	}
	return recovered, nil
}

// GC prunes untagged snapshots beyond KeepLast, then sweeps the object store
// for content no longer referenced by any remaining snapshot. Tagged
// snapshots are always kept. It also recovers abandoned staging data left
// behind by an interrupted Create() (see recoverAbandonedManifests) before
// doing either.
//
// The whole pass runs under the scope's cross-process lock (see
// scopelock.go): GC reads the index, decides what to keep, deletes pruned
// manifests/doc-refs/objects, and writes the index back — a concurrent
// Create() or another GC publishing against a stale copy of that same index
// in between would silently lose one or the other's change, so this must
// run as one uninterrupted critical section, not just protect the final
// write.
func GC(opts GCOptions) (*GCResult, error) {
	scope, err := scopeDir(opts.Connection, opts.Database)
	if err != nil {
		return nil, err
	}
	backend, err := OpenBackend(scope, "")
	if err != nil {
		return nil, err
	}
	defer backend.Close()

	var result *GCResult
	err = withScopeLock(scope, func() error {
		result, err = gcLocked(scope, backend, opts)
		return err
	})
	return result, err
}

func gcLocked(scope string, backend Backend, opts GCOptions) (*GCResult, error) {
	idx, err := loadIndex(scope)
	if err != nil {
		return nil, err
	}

	abandonedRecovered, err := recoverAbandonedManifests(scope, backend, idx)
	if err != nil {
		return nil, err
	}

	sorted := idx.Sorted() // oldest first

	keep := map[string]bool{}
	for _, s := range sorted {
		if len(s.Tags) > 0 {
			keep[s.ID] = true
		}
	}
	if opts.KeepLast > 0 {
		start := len(sorted) - opts.KeepLast
		if start < 0 {
			start = 0
		}
		for _, s := range sorted[start:] {
			keep[s.ID] = true
		}
	}

	result := &GCResult{AbandonedRecovered: abandonedRecovered}
	var kept []Summary
	for _, s := range idx.Snapshots {
		if keep[s.ID] {
			kept = append(kept, s)
			continue
		}
		if err := deleteManifest(scope, s.ID); err != nil {
			return nil, err
		}
		if err := backend.DeleteDocRefs(s.ID); err != nil {
			return nil, err
		}
		result.ManifestsDeleted++
	}
	idx.Snapshots = kept
	if err := saveIndex(scope, idx); err != nil {
		return nil, err
	}

	// Mark every hash still referenced by a kept snapshot. Streamed via the
	// doc-ref iterator (not a full in-memory document list per collection),
	// so this stays bounded even for very large collections; the resulting
	// hash set is the one piece of state genuinely proportional to total
	// document count, which is unavoidable for an exact GC sweep.
	referenced := map[string]bool{}
	for _, s := range kept {
		m, err := loadManifest(scope, s.ID)
		if err != nil {
			return nil, err
		}
		for name := range m.Collections {
			it, err := backend.IterDocRefs(s.ID, name)
			if err != nil {
				return nil, err
			}
			for {
				ref, ok, err := it.Next()
				if err != nil {
					it.Close()
					return nil, err
				}
				if !ok {
					break
				}
				referenced[ref.Hash] = true
			}
			it.Close()
		}
	}

	allHashes, err := backend.AllHashes()
	if err != nil {
		return nil, err
	}
	for _, h := range allHashes {
		if !referenced[h] {
			if err := backend.Delete(h); err != nil {
				return nil, err
			}
			result.ObjectsDeleted++
		}
	}

	return result, nil
}
