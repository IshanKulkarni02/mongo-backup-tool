// Package dashboard persists saved SQL queries and the chart widgets built
// on top of them — a JSON-file index alongside connection config, the same
// pattern internal/store uses for the backup catalog (index.json).
package dashboard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SavedQuery is a named SQL query against one connection/database, kept so
// it can be re-run later or backed by a dashboard widget. Scoped to SQL
// engines for now — charting a MongoDB aggregation's nested documents
// needs a flattening step this phase doesn't build yet.
type SavedQuery struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Connection string `json:"connection"`
	Database   string `json:"database"`
	SQLText    string `json:"sqlText"`
	CreatedAt  string `json:"createdAt"`
}

// ChartType is a widget's rendering.
type ChartType string

const (
	ChartBar     ChartType = "bar"
	ChartLine    ChartType = "line"
	ChartScatter ChartType = "scatter"
	ChartPie     ChartType = "pie"
)

// Widget is a saved query rendered as a chart on the dashboard.
type Widget struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	QueryID   string    `json:"queryId"`
	ChartType ChartType `json:"chartType"`
	XColumn   string    `json:"xColumn"`
	YColumns  []string  `json:"yColumns"`
	CreatedAt string    `json:"createdAt"`
}

// Store is the on-disk saved-query/widget catalog, stored as
// dashboard.json in mongobak's config directory.
type Store struct {
	SavedQueries []SavedQuery `json:"savedQueries"`
	Widgets      []Widget     `json:"widgets"`
}

func indexPath(configDir string) string {
	return filepath.Join(configDir, "dashboard.json")
}

// Load reads the store, returning an empty Store if it doesn't exist yet.
func Load(configDir string) (*Store, error) {
	path := indexPath(configDir)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Store{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading dashboard store %s: %w", path, err)
	}
	var s Store
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing dashboard store %s: %w", path, err)
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

// FindQuery looks up a saved query by ID.
func (s *Store) FindQuery(id string) (*SavedQuery, bool) {
	for i := range s.SavedQueries {
		if s.SavedQueries[i].ID == id {
			return &s.SavedQueries[i], true
		}
	}
	return nil, false
}

// UpsertQuery adds a saved query or replaces the existing one with the
// same ID.
func (s *Store) UpsertQuery(q SavedQuery) {
	for i := range s.SavedQueries {
		if s.SavedQueries[i].ID == q.ID {
			s.SavedQueries[i] = q
			return
		}
	}
	s.SavedQueries = append(s.SavedQueries, q)
}

// RemoveQuery deletes a saved query (and every widget built on it — a
// dangling QueryID would just error at render time, so widgets are pruned
// alongside it), reporting whether the query existed.
func (s *Store) RemoveQuery(id string) bool {
	for i := range s.SavedQueries {
		if s.SavedQueries[i].ID == id {
			s.SavedQueries = append(s.SavedQueries[:i], s.SavedQueries[i+1:]...)
			kept := s.Widgets[:0]
			for _, w := range s.Widgets {
				if w.QueryID != id {
					kept = append(kept, w)
				}
			}
			s.Widgets = kept
			return true
		}
	}
	return false
}

// UpsertWidget adds a dashboard widget or replaces the existing one with
// the same ID.
func (s *Store) UpsertWidget(w Widget) {
	for i := range s.Widgets {
		if s.Widgets[i].ID == w.ID {
			s.Widgets[i] = w
			return
		}
	}
	s.Widgets = append(s.Widgets, w)
}

// RemoveWidget deletes a widget by ID, reporting whether it existed.
func (s *Store) RemoveWidget(id string) bool {
	for i := range s.Widgets {
		if s.Widgets[i].ID == id {
			s.Widgets = append(s.Widgets[:i], s.Widgets[i+1:]...)
			return true
		}
	}
	return false
}
