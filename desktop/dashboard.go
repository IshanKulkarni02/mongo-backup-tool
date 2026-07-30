package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/dashboard"
	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/engine/safeguard"
)

func dashboardStore() (*dashboard.Store, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	return dashboard.Load(dir)
}

func saveDashboardStore(s *dashboard.Store) error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	return dashboard.Save(dir, s)
}

// ListSavedQueries returns every saved query.
func (a *App) ListSavedQueries() ([]dashboard.SavedQuery, error) {
	s, err := dashboardStore()
	if err != nil {
		return nil, err
	}
	if s.SavedQueries == nil {
		return []dashboard.SavedQuery{}, nil
	}
	return s.SavedQueries, nil
}

// SaveQuery saves (or replaces, if id is non-empty and matches an
// existing one) a named SQL query. Returns the saved query's ID.
func (a *App) SaveQuery(id, name, connectionName, database, sqlText string) (string, error) {
	if name == "" || sqlText == "" {
		return "", fmt.Errorf("both a name and a query are required")
	}
	s, err := dashboardStore()
	if err != nil {
		return "", err
	}
	if id == "" {
		id = uuid.NewString()
	}
	s.UpsertQuery(dashboard.SavedQuery{
		ID: id, Name: name, Connection: connectionName, Database: database,
		SQLText: sqlText, CreatedAt: time.Now().Format(time.RFC3339),
	})
	if err := saveDashboardStore(s); err != nil {
		return "", err
	}
	return id, nil
}

// DeleteSavedQuery removes a saved query and any widgets built on it.
func (a *App) DeleteSavedQuery(id string) error {
	s, err := dashboardStore()
	if err != nil {
		return err
	}
	if !s.RemoveQuery(id) {
		return fmt.Errorf("no saved query %q", id)
	}
	return saveDashboardStore(s)
}

// RunSavedQuery re-runs a saved query and returns its current result. A
// saved query isn't guaranteed to be a read (SaveQuery stores whatever SQL
// the editor held at save time), so anything that doesn't look like a read
// goes through the same requireWritable/Classify gating as RunSQLExecute —
// read-only connections refuse it outright, and a dangerous statement
// (DROP, TRUNCATE, ALTER, or an unqualified DELETE/UPDATE) is refused too,
// since this path has no "type the database name" confirmation UI.
func (a *App) RunSavedQuery(id string) (engine.SQLResult, error) {
	s, err := dashboardStore()
	if err != nil {
		return engine.SQLResult{}, err
	}
	q, ok := s.FindQuery(id)
	if !ok {
		return engine.SQLResult{}, fmt.Errorf("no saved query %q", id)
	}
	if !safeguard.IsRead(q.SQLText) {
		if err := a.requireWritable(q.Connection); err != nil {
			return engine.SQLResult{}, err
		}
		if class := safeguard.Classify(q.SQLText); class.Risk == safeguard.RiskDangerous {
			return engine.SQLResult{}, fmt.Errorf("dangerous statement (%s) — open it in the SQL editor to run it with confirmation", class.Reason)
		}
	}
	sess, release, err := a.sqlSession(q.Connection)
	if err != nil {
		return engine.SQLResult{}, err
	}
	defer release()
	return sess.Query(context.Background(), q.Database, q.SQLText)
}

// ListWidgets returns every dashboard widget.
func (a *App) ListWidgets() ([]dashboard.Widget, error) {
	s, err := dashboardStore()
	if err != nil {
		return nil, err
	}
	if s.Widgets == nil {
		return []dashboard.Widget{}, nil
	}
	return s.Widgets, nil
}

// SaveWidget saves (or replaces) a chart widget built on a saved query.
// Returns the widget's ID.
func (a *App) SaveWidget(id, title, queryID, chartType, xColumn string, yColumns []string) (string, error) {
	if title == "" || queryID == "" || xColumn == "" || len(yColumns) == 0 {
		return "", fmt.Errorf("title, query, x column, and at least one y column are required")
	}
	s, err := dashboardStore()
	if err != nil {
		return "", err
	}
	if _, ok := s.FindQuery(queryID); !ok {
		return "", fmt.Errorf("no saved query %q", queryID)
	}
	if id == "" {
		id = uuid.NewString()
	}
	s.UpsertWidget(dashboard.Widget{
		ID: id, Title: title, QueryID: queryID, ChartType: dashboard.ChartType(chartType),
		XColumn: xColumn, YColumns: yColumns, CreatedAt: time.Now().Format(time.RFC3339),
	})
	if err := saveDashboardStore(s); err != nil {
		return "", err
	}
	return id, nil
}

// DeleteWidget removes a dashboard widget.
func (a *App) DeleteWidget(id string) error {
	s, err := dashboardStore()
	if err != nil {
		return err
	}
	if !s.RemoveWidget(id) {
		return fmt.Errorf("no widget %q", id)
	}
	return saveDashboardStore(s)
}
