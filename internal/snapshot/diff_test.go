package snapshot

import (
	"context"
	"fmt"
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

	diff, err := Compare(context.Background(), from, fromSrc, to, toSrc)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	cd, ok := diff.Collections["widgets"]
	if !ok {
		t.Fatalf("expected a diff entry for widgets")
	}

	if cd.AddedCount != 1 {
		t.Errorf("AddedCount = %d, want 1", cd.AddedCount)
	}
	if cd.RemovedCount != 1 {
		t.Errorf("RemovedCount = %d, want 1", cd.RemovedCount)
	}
	if cd.ModifiedCount != 1 {
		t.Errorf("ModifiedCount = %d, want 1", cd.ModifiedCount)
	}
}

func TestCompareIdenticalIsEmpty(t *testing.T) {
	from, fromSrc := manifestWith("s1", map[string][]DocRef{
		"widgets": {{ID: "a", Hash: "h-a"}},
	})
	to, toSrc := manifestWith("s2", map[string][]DocRef{
		"widgets": {{ID: "a", Hash: "h-a"}},
	})

	diff, err := Compare(context.Background(), from, fromSrc, to, toSrc)
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

	diff, err := Compare(context.Background(), from, fromSrc, to, toSrc)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if diff.Empty() {
		t.Fatalf("expected non-empty diff across disjoint collections")
	}
	if diff.Collections["old_coll"].RemovedCount != 1 {
		t.Errorf("old_coll.RemovedCount = %d, want 1", diff.Collections["old_coll"].RemovedCount)
	}
	if diff.Collections["new_coll"].AddedCount != 1 {
		t.Errorf("new_coll.AddedCount = %d, want 1", diff.Collections["new_coll"].AddedCount)
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

	diff, err := Compare(context.Background(), from, fromSrc, to, toSrc)
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
	if cd.RemovedCount != wantRemoved {
		t.Errorf("RemovedCount = %d, want %d", cd.RemovedCount, wantRemoved)
	}
	if cd.ModifiedCount != wantModified {
		t.Errorf("ModifiedCount = %d, want %d", cd.ModifiedCount, wantModified)
	}
	if cd.AddedCount != 0 {
		t.Errorf("AddedCount = %d, want 0", cd.AddedCount)
	}
}

func TestCompareRespectsCancellation(t *testing.T) {
	from, fromSrc := manifestWith("s1", map[string][]DocRef{
		"widgets": {{ID: "a", Hash: "h-a"}, {ID: "b", Hash: "h-b"}},
	})
	to, toSrc := manifestWith("s2", map[string][]DocRef{
		"widgets": {{ID: "a", Hash: "h-a-new"}, {ID: "b", Hash: "h-b-new"}},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Compare(ctx, from, fromSrc, to, toSrc)
	if err == nil {
		t.Fatalf("expected Compare to fail with a cancelled context")
	}
}

func TestStreamDiffVisitsEveryChange(t *testing.T) {
	from, fromSrc := manifestWith("s1", map[string][]DocRef{
		"widgets": {
			{ID: "a", Hash: "h-a"},
			{ID: "b", Hash: "h-b"},
			{ID: "c", Hash: "h-c"},
		},
	})
	to, toSrc := manifestWith("s2", map[string][]DocRef{
		"widgets": {
			{ID: "a", Hash: "h-a"},
			{ID: "b", Hash: "h-b-new"},
			{ID: "d", Hash: "h-d"},
		},
	})

	type change struct {
		collection string
		ct         ChangeType
		id         string
	}
	var got []change
	err := StreamDiff(context.Background(), from, fromSrc, to, toSrc, func(collection string, ct ChangeType, id string) error {
		got = append(got, change{collection, ct, id})
		return nil
	})
	if err != nil {
		t.Fatalf("StreamDiff: %v", err)
	}

	want := map[string]bool{
		"widgets/added/d":    true,
		"widgets/removed/c":  true,
		"widgets/modified/b": true,
	}
	if len(got) != len(want) {
		t.Fatalf("StreamDiff produced %d changes, want %d: %+v", len(got), len(want), got)
	}
	for _, c := range got {
		key := fmt.Sprintf("%s/%s/%s", c.collection, c.ct, c.id)
		if !want[key] {
			t.Errorf("unexpected change %s", key)
		}
	}
}

// TestDiffCollectionPageBoundaryInputs is a table-driven IPC-boundary test:
// DiffCollectionPage must never panic and must always return a sane
// (possibly empty) page for out-of-range offset/limit values, matching what
// a malformed or adversarial Wails IPC call could send.
func TestDiffCollectionPageBoundaryInputs(t *testing.T) {
	_, fromSrc := manifestWith("s1", map[string][]DocRef{
		"widgets": {{ID: "a", Hash: "1"}, {ID: "b", Hash: "1"}, {ID: "c", Hash: "1"}},
	})
	_, toSrc := manifestWith("s2", map[string][]DocRef{
		"widgets": {{ID: "a", Hash: "2"}, {ID: "b", Hash: "2"}, {ID: "c", Hash: "2"}}, // all modified
	})

	cases := []struct {
		name       string
		offset     int
		limit      int
		wantTotal  int
		wantIDsLen int
	}{
		{"negative offset clamps to 0", -100, 10, 3, 3},
		{"negative limit clamps to default cap", 0, -5, 3, 3},
		{"zero limit clamps to default cap", 0, 0, 3, 3},
		{"huge limit is capped", 0, 1_000_000, 3, 3},
		{"offset beyond total returns empty page", 1000, 10, 3, 0},
		{"negative offset and negative limit together", -1, -1, 3, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ids, total, err := DiffCollectionPage(context.Background(), fromSrc, toSrc, "widgets", Modified, tc.offset, tc.limit)
			if err != nil {
				t.Fatalf("DiffCollectionPage: %v", err)
			}
			if total != tc.wantTotal {
				t.Errorf("total = %d, want %d", total, tc.wantTotal)
			}
			if len(ids) != tc.wantIDsLen {
				t.Errorf("len(ids) = %d, want %d", len(ids), tc.wantIDsLen)
			}
		})
	}
}

// TestDiffCollectionPageUnknownCollectionIsEmptyNotPanic confirms asking
// for a collection that doesn't exist on either side returns an empty page
// rather than panicking — a malformed frontend call could easily send a
// stale or typo'd collection name.
func TestDiffCollectionPageUnknownCollectionIsEmptyNotPanic(t *testing.T) {
	_, fromSrc := manifestWith("s1", map[string][]DocRef{"widgets": {{ID: "a", Hash: "1"}}})
	_, toSrc := manifestWith("s2", map[string][]DocRef{"widgets": {{ID: "a", Hash: "2"}}})

	ids, total, err := DiffCollectionPage(context.Background(), fromSrc, toSrc, "does-not-exist", Modified, 0, 10)
	if err != nil {
		t.Fatalf("DiffCollectionPage: %v", err)
	}
	if total != 0 || len(ids) != 0 {
		t.Errorf("DiffCollectionPage on unknown collection = (%v, %d), want (nil, 0)", ids, total)
	}
}

// TestCompareEmptyManifestsIsEmpty confirms comparing two manifests with no
// collections at all (a malformed/empty snapshot) doesn't panic.
func TestCompareEmptyManifestsIsEmpty(t *testing.T) {
	from := &Manifest{ID: "empty1", Collections: map[string]CollectionManifest{}}
	to := &Manifest{ID: "empty2", Collections: map[string]CollectionManifest{}}
	noSource := func(string) (docRefIterator, error) { return newSliceDocRefIterator(nil), nil }

	diff, err := Compare(context.Background(), from, noSource, to, noSource)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if !diff.Empty() {
		t.Errorf("expected empty diff for two empty manifests, got %+v", diff)
	}
}

func TestDiffCollectionPagePaginatesWithoutFullList(t *testing.T) {
	const n = 1000
	fromRefs := make([]DocRef, n)
	toRefs := make([]DocRef, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("id%04d", i)
		fromRefs[i] = DocRef{ID: id, Hash: "same"}
		toRefs[i] = DocRef{ID: id, Hash: fmt.Sprintf("modified-%d", i)} // every doc modified
	}

	_, fromSrc := manifestWith("s1", map[string][]DocRef{"big": fromRefs})
	_, toSrc := manifestWith("s2", map[string][]DocRef{"big": toRefs})

	ids, total, err := DiffCollectionPage(context.Background(), fromSrc, toSrc, "big", Modified, 10, 5)
	if err != nil {
		t.Fatalf("DiffCollectionPage: %v", err)
	}
	if total != n {
		t.Errorf("total = %d, want %d", total, n)
	}
	if len(ids) != 5 {
		t.Fatalf("got %d ids, want 5", len(ids))
	}
	want := []string{"id0010", "id0011", "id0012", "id0013", "id0014"}
	sort.Strings(ids)
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("ids[%d] = %s, want %s", i, ids[i], want[i])
		}
	}
}

func TestDiffCollectionPageBeyondEndIsEmpty(t *testing.T) {
	_, fromSrc := manifestWith("s1", map[string][]DocRef{"c": {{ID: "a", Hash: "1"}}})
	_, toSrc := manifestWith("s2", map[string][]DocRef{"c": {{ID: "a", Hash: "2"}}})

	ids, total, err := DiffCollectionPage(context.Background(), fromSrc, toSrc, "c", Modified, 50, 10)
	if err != nil {
		t.Fatalf("DiffCollectionPage: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(ids) != 0 {
		t.Errorf("ids = %v, want empty", ids)
	}
}
