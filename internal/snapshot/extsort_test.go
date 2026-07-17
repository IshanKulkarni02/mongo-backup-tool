package snapshot

import (
	"fmt"
	"math/rand"
	"os"
	"sort"
	"testing"
)

func TestExtSortSpillSmallStaysInMemory(t *testing.T) {
	spill, err := newExtSortSpill()
	if err != nil {
		t.Fatal(err)
	}
	defer spill.Cleanup()

	refs := []DocRef{{ID: "c", Hash: "3"}, {ID: "a", Hash: "1"}, {ID: "b", Hash: "2"}}
	for _, r := range refs {
		if err := spill.Add(r); err != nil {
			t.Fatal(err)
		}
	}
	if len(spill.runPaths) != 0 {
		t.Fatalf("expected no runs spilled to disk for a small input, got %d", len(spill.runPaths))
	}

	it, err := spill.NewIterator()
	if err != nil {
		t.Fatal(err)
	}
	got, err := drainDocRefIterator(it)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %d refs, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("got[%d].ID = %s, want %s", i, got[i].ID, id)
		}
	}
}

func TestExtSortSpillSpillsAndMergesLargeInput(t *testing.T) {
	orig := extSortRunSize
	extSortRunSize = 100 // force many small runs without a slow test
	defer func() { extSortRunSize = orig }()

	spill, err := newExtSortSpill()
	if err != nil {
		t.Fatal(err)
	}
	defer spill.Cleanup()

	const n = 2350 // spans 23 full runs plus a partial tail
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("id%05d", i)
	}
	// Add in shuffled order — the spill must sort, not assume sorted input.
	shuffled := append([]string(nil), ids...)
	rand.New(rand.NewSource(1)).Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	for _, id := range shuffled {
		if err := spill.Add(DocRef{ID: id, Hash: "h-" + id}); err != nil {
			t.Fatal(err)
		}
	}
	if len(spill.runPaths) < 2 {
		t.Fatalf("expected multiple spilled runs for %d entries at run size %d, got %d", n, extSortRunSize, len(spill.runPaths))
	}

	it, err := spill.NewIterator()
	if err != nil {
		t.Fatal(err)
	}
	got, err := drainDocRefIterator(it)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Fatalf("got %d refs, want %d", len(got), n)
	}
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i].ID < got[j].ID }) {
		t.Fatalf("merged output is not sorted by ID")
	}
	for i, id := range ids {
		if got[i].ID != id {
			t.Fatalf("got[%d].ID = %s, want %s", i, got[i].ID, id)
		}
		if got[i].Hash != "h-"+id {
			t.Errorf("got[%d].Hash = %s, want h-%s", i, got[i].Hash, id)
		}
	}
}

func TestExtSortSpillIteratesMultipleTimes(t *testing.T) {
	orig := extSortRunSize
	extSortRunSize = 10
	defer func() { extSortRunSize = orig }()

	spill, err := newExtSortSpill()
	if err != nil {
		t.Fatal(err)
	}
	defer spill.Cleanup()

	for i := 0; i < 55; i++ {
		if err := spill.Add(DocRef{ID: fmt.Sprintf("id%03d", i), Hash: "h"}); err != nil {
			t.Fatal(err)
		}
	}

	for pass := 0; pass < 2; pass++ {
		it, err := spill.NewIterator()
		if err != nil {
			t.Fatalf("pass %d: NewIterator: %v", pass, err)
		}
		got, err := drainDocRefIterator(it)
		if err != nil {
			t.Fatalf("pass %d: drain: %v", pass, err)
		}
		if len(got) != 55 {
			t.Fatalf("pass %d: got %d refs, want 55", pass, len(got))
		}
	}
}

func TestExtSortSpillCleanupRemovesTempFiles(t *testing.T) {
	orig := extSortRunSize
	extSortRunSize = 10
	defer func() { extSortRunSize = orig }()

	spill, err := newExtSortSpill()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		if err := spill.Add(DocRef{ID: fmt.Sprintf("id%03d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	dir := spill.dir
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected spill dir to exist before Cleanup: %v", err)
	}
	if err := spill.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected spill dir to be removed after Cleanup, stat err = %v", err)
	}
}

func TestExtSortSpillEmpty(t *testing.T) {
	spill, err := newExtSortSpill()
	if err != nil {
		t.Fatal(err)
	}
	defer spill.Cleanup()

	it, err := spill.NewIterator()
	if err != nil {
		t.Fatal(err)
	}
	got, err := drainDocRefIterator(it)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no refs from an empty spill, got %d", len(got))
	}
}
