// Package store manages the index of local backup archives (metadata about
// files living in the backups directory managed by internal/config).
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Backup describes one backup archive on disk.
type Backup struct {
	ID         string `json:"id"`
	Connection string `json:"connection"`
	Database   string `json:"database"` // empty means "all databases"
	FileName   string `json:"fileName"`
	SizeBytes  int64  `json:"sizeBytes"`
	CreatedAt  string `json:"createdAt"`
}

// Index is the on-disk backup catalog, stored as index.json in the backups directory.
type Index struct {
	Backups []Backup `json:"backups"`
}

func indexPath(backupsDir string) string {
	return filepath.Join(backupsDir, "index.json")
}

// Load reads the backup index, returning an empty Index if it doesn't exist yet.
func Load(backupsDir string) (*Index, error) {
	path := indexPath(backupsDir)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Index{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading backup index %s: %w", path, err)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parsing backup index %s: %w", path, err)
	}
	return &idx, nil
}

// Save writes the backup index via a temp file + rename, so a crash mid-
// write can never corrupt index.json into something the next Load can't
// parse (which would otherwise break backup listing/restore/delete
// app-wide until manually fixed).
func Save(backupsDir string, idx *Index) error {
	path := indexPath(backupsDir)
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o644)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// errNoChange is a sentinel an Update mutate function can return to skip
// the save without Update treating it as a real error.
var errNoChange = errors.New("store: no change")

// updateMu serializes Update calls so a full load-mutate-save sequence on
// index.json can't interleave with another one — e.g. a backup job and a
// delete finishing close together, where a plain Load-then-Save per call
// site lets whichever Save lands second silently drop the other's entry.
var updateMu sync.Mutex

// Update loads the backup index, applies mutate to it, and saves the
// result, holding a package-level lock for the whole sequence. mutate can
// return errNoChange to abort without saving (not treated as an error).
func Update(backupsDir string, mutate func(*Index) error) error {
	updateMu.Lock()
	defer updateMu.Unlock()
	idx, err := Load(backupsDir)
	if err != nil {
		return err
	}
	if err := mutate(idx); err != nil {
		if errors.Is(err, errNoChange) {
			return nil
		}
		return err
	}
	return Save(backupsDir, idx)
}

// Find looks up a backup by ID.
func (idx *Index) Find(id string) (*Backup, bool) {
	for i := range idx.Backups {
		if idx.Backups[i].ID == id {
			return &idx.Backups[i], true
		}
	}
	return nil, false
}

// Remove deletes a backup entry by ID, reporting whether it existed.
func (idx *Index) Remove(id string) bool {
	for i := range idx.Backups {
		if idx.Backups[i].ID == id {
			idx.Backups = append(idx.Backups[:i], idx.Backups[i+1:]...)
			return true
		}
	}
	return false
}
