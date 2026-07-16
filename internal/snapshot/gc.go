package snapshot

// GCOptions configures pruning old snapshots for one connection+database.
type GCOptions struct {
	Connection string
	Database   string
	KeepLast   int // keep the N most recent snapshots regardless of tags; 0 keeps only tagged ones
}

// GCResult reports what a GC pass removed.
type GCResult struct {
	ManifestsDeleted int
	ObjectsDeleted   int
}

// GC prunes untagged snapshots beyond KeepLast, then sweeps the object store
// for content no longer referenced by any remaining snapshot. Tagged
// snapshots are always kept.
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

	idx, err := loadIndex(scope)
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

	result := &GCResult{}
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
