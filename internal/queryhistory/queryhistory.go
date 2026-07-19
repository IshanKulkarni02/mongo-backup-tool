// Package queryhistory persists an automatic log of every SQL query run
// from the desktop app — a JSON-file index alongside connection config,
// the same pattern internal/dashboard uses for saved queries. Distinct
// from internal/dashboard's SavedQuery: this is an automatic record of
// what actually ran, not a user-curated list of queries worth keeping.
package queryhistory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// maxEntries caps how many history entries are kept, oldest dropped first,
// so the file doesn't grow unbounded across a long-lived install.
const maxEntries = 200

// Entry is one query run, successful or not.
type Entry struct {
	ID           string `json:"id"`
	Connection   string `json:"connection"`
	Database     string `json:"database"`
	SQLText      string `json:"sqlText"`
	RowCount     int    `json:"rowCount"`
	DurationMs   int64  `json:"durationMs"`
	Success      bool   `json:"success"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	RanAt        string `json:"ranAt"`
}

// Store is the on-disk query-history log, stored as queryhistory.json in
// dbhelm's config directory.
type Store struct {
	Entries []Entry `json:"entries"`
}

func indexPath(configDir string) string {
	return filepath.Join(configDir, "queryhistory.json")
}

// Load reads the store, returning an empty Store if it doesn't exist yet.
func Load(configDir string) (*Store, error) {
	path := indexPath(configDir)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Store{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading query history %s: %w", path, err)
	}
	var s Store
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing query history %s: %w", path, err)
	}
	return &s, nil
}

// Save writes the store.
func Save(configDir string, s *Store) error {
	path := indexPath(configDir)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Append prepends a new entry (most recent first) and truncates to
// maxEntries, dropping the oldest.
func (s *Store) Append(e Entry) {
	s.Entries = append([]Entry{e}, s.Entries...)
	if len(s.Entries) > maxEntries {
		s.Entries = s.Entries[:maxEntries]
	}
}

// Find looks up an entry by ID.
func (s *Store) Find(id string) (*Entry, bool) {
	for i := range s.Entries {
		if s.Entries[i].ID == id {
			return &s.Entries[i], true
		}
	}
	return nil, false
}

// Clear empties the log.
func (s *Store) Clear() {
	s.Entries = nil
}

// List returns the most recent entries for a connection (or every
// connection, if connection is empty), newest first, capped at limit (0
// means no cap).
func (s *Store) List(connection string, limit int) []Entry {
	var out []Entry
	for _, e := range s.Entries {
		if connection != "" && e.Connection != connection {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}
