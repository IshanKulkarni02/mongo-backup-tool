package snapshot

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
)

func manifestWith(id string, docs map[string][]DocRef) (*Manifest, docRefSource) {
	cols := make(map[string]CollectionManifest, len(docs))
	refsByCollection := make(map[string][]DocRef, len(docs))
	for name, refs := range docs {
		cols[name] = CollectionManifest{DocCount: len(refs)}
		refsByCollection[name] = refs
	}
	m := &Manifest{ID: id, Collections: cols}
	source := func(collection string) (docRefIterator, error) {
		return newSliceDocRefIterator(refsByCollection[collection]), nil
	}
	return m, source
}

func TestCompareAddedRemovedModified(t *testing.T) {
	from, fromSrc := manifestWith("s1", map[string][]DocRef{
		"widgets": {
			{ID: "a", Hash: "h-a"},
			{ID: "b", Hash: "h-b"},
			{ID: "c", Hash: "h-c"},
		},
	})
	to, toSrc := manifestWith("s2", map[string][]DocRef{
		"widgets": {
			{ID: "a", Hash: "h-a"},     // unchanged
			{ID: "b", Hash: "h-b-new"}, // modified
			{ID: "d", Hash: "h-d"},     // added
			// "c" removed
		},
	})

	diff, err := Compare(from, fromSrc, to, toSrc)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	cd, ok := diff.Collections["widgets"]
	if !ok {
		t.Fatalf("expected a diff entry for widgets")
	}

	sort.Strings(cd.Added)
	sort.Strings(cd.Removed)
	sort.Strings(cd.Modified)

	if !reflect.DeepEqual(cd.Added, []string{"d"}) {
		t.Errorf("Added = %v, want [d]", cd.Added)
	}
	if !reflect.DeepEqual(cd.Removed, []string{"c"}) {
		t.Errorf("Removed = %v, want [c]", cd.Removed)
	}
	if !reflect.DeepEqual(cd.Modified, []string{"b"}) {
		t.Errorf("Modified = %v, want [b]", cd.Modified)
	}
}

func TestCompareIdenticalIsEmpty(t *testing.T) {
	from, fromSrc := manifestWith("s1", map[string][]DocRef{
		"widgets": {{ID: "a", Hash: "h-a"}},
	})
	to, toSrc := manifestWith("s2", map[string][]DocRef{
		"widgets": {{ID: "a", Hash: "h-a"}},
	})

	diff, err := Compare(from, fromSrc, to, toSrc)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if !diff.Empty() {
		t.Fatalf("expected identical manifests to diff as empty, got %+v", diff)
	}
}

func TestCompareCollectionOnlyOnOneSide(t *testing.T) {
	from, fromSrc := manifestWith("s1", map[string][]DocRef{
		"old_coll": {{ID: "a", Hash: "h-a"}},
	})
	to, toSrc := manifestWith("s2", map[string][]DocRef{
		"new_coll": {{ID: "x", Hash: "h-x"}},
	})

	diff, err := Compare(from, fromSrc, to, toSrc)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if diff.Empty() {
		t.Fatalf("expected non-empty diff across disjoint collections")
	}
	if !reflect.DeepEqual(diff.Collections["old_coll"].Removed, []string{"a"}) {
		t.Errorf("old_coll.Removed = %v, want [a]", diff.Collections["old_coll"].Removed)
	}
	if !reflect.DeepEqual(diff.Collections["new_coll"].Added, []string{"x"}) {
		t.Errorf("new_coll.Added = %v, want [x]", diff.Collections["new_coll"].Added)
	}
}

func TestCompareLargeSortedInputs(t *testing.T) {
	// Exercises the merge-join across many chunk-sized boundaries. IDs are
	// zero-padded so insertion order already matches lexicographic sort —
	// diffCollection's merge-join requires sorted input, same precondition
	// real snapshots satisfy (scanCollectionDocs sorts before writing).
	const n = 3 * docRefChunkSize
	fromRefs := make([]DocRef, 0, n)
	toRefs := make([]DocRef, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("id%06d", i)
		fromRefs = append(fromRefs, DocRef{ID: id, Hash: fmt.Sprintf("h%d", i)})
		if i%7 == 0 {
			continue // dropped in "to"
		}
		hash := fmt.Sprintf("h%d", i)
		if i%5 == 0 {
			hash = fmt.Sprintf("modified-%d", i)
		}
		toRefs = append(toRefs, DocRef{ID: id, Hash: hash})
	}

	from, fromSrc := manifestWith("s1", map[string][]DocRef{"big": fromRefs})
	to, toSrc := manifestWith("s2", map[string][]DocRef{"big": toRefs})

	diff, err := Compare(from, fromSrc, to, toSrc)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	cd := diff.Collections["big"]

	wantRemoved := 0
	wantModified := 0
	for i := 0; i < n; i++ {
		if i%7 == 0 {
			wantRemoved++
			continue
		}
		if i%5 == 0 {
			wantModified++
		}
	}
	if len(cd.Removed) != wantRemoved {
		t.Errorf("Removed count = %d, want %d", len(cd.Removed), wantRemoved)
	}
	if len(cd.Modified) != wantModified {
		t.Errorf("Modified count = %d, want %d", len(cd.Modified), wantModified)
	}
	if len(cd.Added) != 0 {
		t.Errorf("Added count = %d, want 0", len(cd.Added))
	}
}
