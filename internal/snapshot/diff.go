package snapshot

// CollectionDiff lists document-level changes within one collection between
// two snapshots (or a snapshot and a live scan).
type CollectionDiff struct {
	Added    []string `json:"added,omitempty"`
	Removed  []string `json:"removed,omitempty"`
	Modified []string `json:"modified,omitempty"`
}

func (d CollectionDiff) Empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Modified) == 0
}

// Diff is the full comparison between two manifests.
type Diff struct {
	FromID      string                    `json:"fromId"`
	ToID        string                    `json:"toId"`
	Collections map[string]CollectionDiff `json:"collections"`
}

// Empty reports whether the two manifests were identical.
func (d Diff) Empty() bool {
	for _, c := range d.Collections {
		if !c.Empty() {
			return false
		}
	}
	return true
}

// docRefSource supplies a docRefIterator for a named collection. A
// persisted snapshot's source is backend.IterDocRefs bound to its manifest
// ID; a live scan's source wraps the in-memory map ScanLive returns. Either
// way, Compare never needs to know which.
type docRefSource func(collection string) (docRefIterator, error)

// Compare computes the difference from one snapshot state to another as a
// streaming merge-join over each collection's sorted DocRef list: memory use
// is bounded by one chunk per side, not by collection size, so diffing two
// million-document collections doesn't require holding either one fully in
// memory.
func Compare(from *Manifest, fromSource docRefSource, to *Manifest, toSource docRefSource) (Diff, error) {
	names := map[string]bool{}
	for name := range from.Collections {
		names[name] = true
	}
	for name := range to.Collections {
		names[name] = true
	}

	collections := make(map[string]CollectionDiff, len(names))
	for name := range names {
		fromIt, err := fromSource(name)
		if err != nil {
			return Diff{}, err
		}
		toIt, err := toSource(name)
		if err != nil {
			fromIt.Close()
			return Diff{}, err
		}

		cd, err := diffCollection(fromIt, toIt)
		if err != nil {
			return Diff{}, err
		}
		if !cd.Empty() {
			collections[name] = cd
		}
	}

	return Diff{FromID: from.ID, ToID: to.ID, Collections: collections}, nil
}

// diffCollection merge-joins two ID-sorted DocRef iterators, classic
// sorted-diff style: advance whichever side has the smaller current ID
// (that side's doc is unique to it), or compare hashes on a tie (same ID
// present on both sides).
func diffCollection(fromIt, toIt docRefIterator) (CollectionDiff, error) {
	defer fromIt.Close()
	defer toIt.Close()

	var cd CollectionDiff

	fRef, fOK, err := fromIt.Next()
	if err != nil {
		return cd, err
	}
	tRef, tOK, err := toIt.Next()
	if err != nil {
		return cd, err
	}

	for fOK && tOK {
		switch {
		case fRef.ID < tRef.ID:
			cd.Removed = append(cd.Removed, fRef.ID)
			fRef, fOK, err = fromIt.Next()
		case fRef.ID > tRef.ID:
			cd.Added = append(cd.Added, tRef.ID)
			tRef, tOK, err = toIt.Next()
		default:
			if fRef.Hash != tRef.Hash {
				cd.Modified = append(cd.Modified, fRef.ID)
			}
			fRef, fOK, err = fromIt.Next()
			if err == nil {
				tRef, tOK, err = toIt.Next()
			}
		}
		if err != nil {
			return cd, err
		}
	}
	for fOK {
		cd.Removed = append(cd.Removed, fRef.ID)
		if fRef, fOK, err = fromIt.Next(); err != nil {
			return cd, err
		}
	}
	for tOK {
		cd.Added = append(cd.Added, tRef.ID)
		if tRef, tOK, err = toIt.Next(); err != nil {
			return cd, err
		}
	}

	return cd, nil
}
