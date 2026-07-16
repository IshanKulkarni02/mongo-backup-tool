// Package store manages the index of local backup archives (metadata about
// files living in the backups directory managed by internal/config).
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// Save writes the backup index.
func Save(backupsDir string, idx *Index) error {
	path := indexPath(backupsDir)
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
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
