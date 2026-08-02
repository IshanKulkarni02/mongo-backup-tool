package store

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	idx := &Index{Backups: []Backup{{ID: "1", FileName: "a.archive.gz"}}}
	if err := Save(dir, idx); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Backups) != 1 || loaded.Backups[0].FileName != "a.archive.gz" {
		t.Fatalf("unexpected loaded backups: %+v", loaded.Backups)
	}
}

func TestLoadMissingFileReturnsEmptyIndex(t *testing.T) {
	dir := t.TempDir()
	idx, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(idx.Backups) != 0 {
		t.Fatalf("expected empty index, got %+v", idx.Backups)
	}
}

// TestUpdateSerializesConcurrentWrites guards against #22: a plain
// Load-mutate-Save per call site (as backup creation and deletion used to
// do) lets two goroutines racing on the same index.json silently lose
// one's write. Update must serialize the whole sequence so every
// concurrent caller's entry survives.
func TestUpdateSerializesConcurrentWrites(t *testing.T) {
	dir := t.TempDir()

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := Update(dir, func(idx *Index) error {
				idx.Backups = append(idx.Backups, Backup{ID: fmt.Sprintf("backup-%d", i), FileName: fmt.Sprintf("b%d.archive.gz", i)})
				return nil
			})
			if err != nil {
				t.Errorf("Update: %v", err)
			}
		}(i)
	}
	wg.Wait()

	idx, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(idx.Backups) != n {
		t.Fatalf("expected %d backups after %d concurrent Updates, got %d", n, n, len(idx.Backups))
	}
	for i := 0; i < n; i++ {
		if _, ok := idx.Find(fmt.Sprintf("backup-%d", i)); !ok {
			t.Errorf("backup-%d is missing — its Update was lost to a race", i)
		}
	}
}

func TestUpdateSkipsSaveOnErrNoChange(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, &Index{Backups: []Backup{{ID: "seed"}}}); err != nil {
		t.Fatalf("seeding Save: %v", err)
	}
	before, err := os.ReadFile(indexPath(dir))
	if err != nil {
		t.Fatal(err)
	}

	if err := Update(dir, func(idx *Index) error { return errNoChange }); err != nil {
		t.Fatalf("Update with errNoChange should not return an error, got: %v", err)
	}

	after, err := os.ReadFile(indexPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("expected the file to be untouched when mutate returns errNoChange")
	}
}

// TestSaveWriteIsAtomic guards against #22's other half: the original
// Save used a direct os.WriteFile, so a crash mid-write could corrupt
// index.json into something the next Load can't parse, breaking backup
// listing/restore/delete app-wide. Save must go through a temp file +
// rename, leaving no stray temp file behind.
func TestSaveWriteIsAtomic(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, &Index{Backups: []Backup{{ID: "a"}}}); err != nil {
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
