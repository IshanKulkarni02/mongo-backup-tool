package snapshot

import (
	"context"
	"sort"
)

// ChangeType identifies which side of a diff a changed document ID belongs
// to.
type ChangeType int

const (
	Added ChangeType = iota
	Removed
	Modified
)

func (c ChangeType) String() string {
	switch c {
	case Added:
		return "added"
	case Removed:
		return "removed"
	case Modified:
		return "modified"
	default:
		return "unknown"
	}
}

// CollectionDiff holds only change *counts* for one collection — never the
// changed ID lists themselves, so computing (or summarizing) a diff never
// requires holding every changed ID in memory, regardless of collection
// size. Callers that need the actual IDs use DiffCollectionPage (bounded,
// paginated) or StreamDiff (bounded, visits every change via callback)
// instead of asking Compare for a full list.
type CollectionDiff struct {
	AddedCount    int `json:"addedCount"`
	RemovedCount  int `json:"removedCount"`
	ModifiedCount int `json:"modifiedCount"`
}

func (d CollectionDiff) Empty() bool {
	return d.AddedCount == 0 && d.RemovedCount == 0 && d.ModifiedCount == 0
}

// Diff is the full comparison between two manifests: per-collection change
// counts only.
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
// ID; a live scan's source wraps its bounded external-merge spill (see
// extsort.go). Either way, Compare never needs to know which — and every
// docRefSource may be called more than once for the same collection (each
// call must return a fresh, independently-consumable iterator).
type docRefSource func(collection string) (docRefIterator, error)

// collectionNames returns the union of both manifests' collection names.
func collectionNames(from, to *Manifest) []string {
	names := map[string]bool{}
	for name := range from.Collections {
		names[name] = true
	}
	for name := range to.Collections {
		names[name] = true
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Compare computes the difference from one snapshot state to another as a
// streaming merge-join over each collection's sorted DocRef list: memory use
// is bounded by one chunk per side (not by collection or diff size), so
// diffing two million-document collections doesn't require holding either
// one — or the resulting change lists — fully in memory. ctx may cancel a
// long-running comparison; a cancellation is returned as ctx.Err().
func Compare(ctx context.Context, from *Manifest, fromSource docRefSource, to *Manifest, toSource docRefSource) (Diff, error) {
	collections := make(map[string]CollectionDiff)
	for _, name := range collectionNames(from, to) {
		fromIt, err := fromSource(name)
		if err != nil {
			return Diff{}, err
		}
		toIt, err := toSource(name)
		if err != nil {
			fromIt.Close()
			return Diff{}, err
		}

		cd, err := diffCollection(ctx, fromIt, toIt)
		if err != nil {
			return Diff{}, err
		}
		if !cd.Empty() {
			collections[name] = cd
		}
	}

	return Diff{FromID: from.ID, ToID: to.ID, Collections: collections}, nil
}

// diffCollection merge-joins two ID-sorted DocRef iterators and counts
// changes only — it never appends to a growing list, so its memory use is
// O(1) regardless of how many documents changed.
func diffCollection(ctx context.Context, fromIt, toIt docRefIterator) (CollectionDiff, error) {
	defer fromIt.Close()
	defer toIt.Close()

	var cd CollectionDiff
	err := mergeJoin(ctx, fromIt, toIt, func(ct ChangeType, _ string) error {
		switch ct {
		case Added:
			cd.AddedCount++
		case Removed:
			cd.RemovedCount++
		case Modified:
			cd.ModifiedCount++
		}
		return nil
	})
	return cd, err
}

// mergeJoin is the shared core merge-join: classic sorted-diff style,
// advancing whichever side has the smaller current ID (that side's doc is
// unique to it) or comparing hashes on a tie (same ID present on both
// sides), invoking onChange for every change found. Callers control memory
// use entirely via what onChange does — counting (diffCollection), filtered
// collection (DiffCollectionPage), or side-effecting output (StreamDiff).
func mergeJoin(ctx context.Context, fromIt, toIt docRefIterator, onChange func(ChangeType, string) error) error {
	fRef, fOK, err := fromIt.Next()
	if err != nil {
		return err
	}
	tRef, tOK, err := toIt.Next()
	if err != nil {
		return err
	}

	for fOK && tOK {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch {
		case fRef.ID < tRef.ID:
			if err := onChange(Removed, fRef.ID); err != nil {
				return err
			}
			fRef, fOK, err = fromIt.Next()
		case fRef.ID > tRef.ID:
			if err := onChange(Added, tRef.ID); err != nil {
				return err
			}
			tRef, tOK, err = toIt.Next()
		default:
			if fRef.Hash != tRef.Hash {
				if err := onChange(Modified, fRef.ID); err != nil {
					return err
				}
			}
			fRef, fOK, err = fromIt.Next()
			if err == nil {
				tRef, tOK, err = toIt.Next()
			}
		}
		if err != nil {
			return err
		}
	}
	for fOK {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := onChange(Removed, fRef.ID); err != nil {
			return err
		}
		if fRef, fOK, err = fromIt.Next(); err != nil {
			return err
		}
	}
	for tOK {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := onChange(Added, tRef.ID); err != nil {
			return err
		}
		if tRef, tOK, err = toIt.Next(); err != nil {
			return err
		}
	}

	return nil
}

// StreamDiff merge-joins every collection in from/to and invokes onChange
// for each changed document as it's found, in collection-name order — never
// materializing a changed-ID list for any collection, so printing (or
// otherwise fully consuming) a diff with hundreds of thousands of changes
// stays bounded in memory. ctx may cancel a long-running stream.
func StreamDiff(ctx context.Context, from *Manifest, fromSource docRefSource, to *Manifest, toSource docRefSource, onChange func(collection string, ct ChangeType, id string) error) error {
	for _, name := range collectionNames(from, to) {
		fromIt, err := fromSource(name)
		if err != nil {
			return err
		}
		toIt, err := toSource(name)
		if err != nil {
			fromIt.Close()
			return err
		}
		err = func() error {
			defer fromIt.Close()
			defer toIt.Close()
			return mergeJoin(ctx, fromIt, toIt, func(ct ChangeType, id string) error {
				return onChange(name, ct, id)
			})
		}()
		if err != nil {
			return err
		}
	}
	return nil
}

// diffPageMaxLimit caps how many IDs DiffCollectionPage will ever return in
// one call, independent of whatever limit a caller (Wails IPC, CLI, TUI)
// asks for — enforced here, at the core function, rather than trusted to
// every boundary that happens to call it.
const diffPageMaxLimit = 500

// DiffCollectionPage returns one page of one collection's changed document
// IDs, filtered to a single ChangeType, without ever materializing the full
// matched-ID list — memory is bounded by limit, not by how many documents
// changed in the collection. It re-runs the (cheap, streaming) merge-join
// for just the requested collection; ctx may cancel a long scan (e.g. a
// late offset behind many changes). offset is clamped to 0 if negative;
// limit is clamped to [1, diffPageMaxLimit] if it's outside that range —
// callers never need to pre-validate these themselves.
func DiffCollectionPage(ctx context.Context, fromSource, toSource docRefSource, collection string, changeType ChangeType, offset, limit int) (ids []string, total int, err error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > diffPageMaxLimit {
		limit = diffPageMaxLimit
	}

	fromIt, err := fromSource(collection)
	if err != nil {
		return nil, 0, err
	}
	toIt, err := toSource(collection)
	if err != nil {
		fromIt.Close()
		return nil, 0, err
	}
	defer fromIt.Close()
	defer toIt.Close()

	matchIndex := 0
	err = mergeJoin(ctx, fromIt, toIt, func(ct ChangeType, id string) error {
		if ct != changeType {
			return nil
		}
		if matchIndex >= offset && len(ids) < limit {
			ids = append(ids, id)
		}
		matchIndex++
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return ids, matchIndex, nil
}
