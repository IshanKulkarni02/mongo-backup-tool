package queryhistory

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &Store{}
	s.Append(Entry{ID: "1", Connection: "local", SQLText: "SELECT 1", Success: true, RanAt: "t1"})
	if err := Save(dir, s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Entries) != 1 || loaded.Entries[0].SQLText != "SELECT 1" {
		t.Fatalf("unexpected loaded entries: %+v", loaded.Entries)
	}
}

func TestLoadMissingFileReturnsEmptyStore(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.Entries) != 0 {
		t.Fatalf("expected empty store, got %+v", s.Entries)
	}
}

func TestAppendCapsAtMaxEntries(t *testing.T) {
	s := &Store{}
	for i := 0; i < maxEntries+1; i++ {
		s.Append(Entry{ID: strconv.Itoa(i), SQLText: "SELECT " + strconv.Itoa(i)})
	}
	if len(s.Entries) != maxEntries {
		t.Fatalf("expected %d entries after overflow, got %d", maxEntries, len(s.Entries))
	}
	// Most recent (last appended, id=maxEntries) should be first; the
	// oldest (id=0) should have been dropped.
	if s.Entries[0].ID != strconv.Itoa(maxEntries) {
		t.Fatalf("expected newest entry first, got %+v", s.Entries[0])
	}
	if _, ok := s.Find(strconv.Itoa(0)); ok {
		t.Fatal("expected the oldest entry to have been dropped")
	}
}

func TestFind(t *testing.T) {
	s := &Store{}
	s.Append(Entry{ID: "a", SQLText: "SELECT a"})
	s.Append(Entry{ID: "b", SQLText: "SELECT b"})

	e, ok := s.Find("b")
	if !ok || e.SQLText != "SELECT b" {
		t.Fatalf("expected to find entry b, got %+v ok=%v", e, ok)
	}
	if _, ok := s.Find("nope"); ok {
		t.Fatal("expected not to find a nonexistent entry")
	}
}

func TestClear(t *testing.T) {
	s := &Store{}
	s.Append(Entry{ID: "a"})
	s.Clear()
	if len(s.Entries) != 0 {
		t.Fatalf("expected empty entries after Clear, got %+v", s.Entries)
	}
}

// TestUpdateSerializesConcurrentAppends guards against #22: recording
// query history for two queries that finish close together (each
// dispatched in its own Wails RPC goroutine) used to race on a plain
// Load-mutate-Save, silently dropping one entry. Update must serialize
// the whole sequence so every concurrent append survives.
func TestUpdateSerializesConcurrentAppends(t *testing.T) {
	dir := t.TempDir()

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := Update(dir, func(s *Store) error {
				s.Append(Entry{ID: strconv.Itoa(i), SQLText: "SELECT " + strconv.Itoa(i)})
				return nil
			})
			if err != nil {
				t.Errorf("Update: %v", err)
			}
		}(i)
	}
	wg.Wait()

	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.Entries) != n {
		t.Fatalf("expected %d entries after %d concurrent Updates, got %d", n, n, len(s.Entries))
	}
	for i := 0; i < n; i++ {
		if _, ok := s.Find(strconv.Itoa(i)); !ok {
			t.Errorf("entry %d is missing — its Update was lost to a race", i)
		}
	}
}

// TestSaveWriteIsAtomic guards against #22's other half: the original
// Save used a direct os.WriteFile, so a crash mid-write could corrupt
// queryhistory.json into something the next Load can't parse. Save must
// go through a temp file + rename, leaving no stray temp file behind.
func TestSaveWriteIsAtomic(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, &Store{Entries: []Entry{{ID: "a"}}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("stray temp file left behind after Save: %s", e.Name())
		}
	}
}

func TestListFiltersByConnectionAndLimit(t *testing.T) {
	s := &Store{}
	s.Append(Entry{ID: "1", Connection: "local", SQLText: "q1"})
	s.Append(Entry{ID: "2", Connection: "prod", SQLText: "q2"})
	s.Append(Entry{ID: "3", Connection: "local", SQLText: "q3"})

	local := s.List("local", 0)
	if len(local) != 2 {
		t.Fatalf("expected 2 local entries, got %d: %+v", len(local), local)
	}
	// Newest first: entry "3" was appended most recently among "local".
	if local[0].ID != "3" {
		t.Fatalf("expected newest-first order, got %+v", local)
	}

	limited := s.List("", 2)
	if len(limited) != 2 {
		t.Fatalf("expected limit to cap at 2, got %d", len(limited))
	}
}
