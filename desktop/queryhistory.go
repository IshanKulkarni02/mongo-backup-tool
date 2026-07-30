package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/queryhistory"
)

func queryHistoryStore() (*queryhistory.Store, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	return queryhistory.Load(dir)
}

func saveQueryHistoryStore(s *queryhistory.Store) error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	return queryhistory.Save(dir, s)
}

// recordQueryHistory appends a query-history entry. Best-effort: a failure
// to load/save the history log is swallowed rather than surfaced, since it
// must never make an otherwise-successful query call fail.
func recordQueryHistory(connectionName, database, sqlText string, rowCount int, dur time.Duration, runErr error) {
	s, err := queryHistoryStore()
	if err != nil {
		return
	}
	entry := queryhistory.Entry{
		ID:         uuid.NewString(),
		Connection: connectionName,
		Database:   database,
		SQLText:    sqlText,
		RowCount:   rowCount,
		DurationMs: dur.Milliseconds(),
		Success:    runErr == nil,
		RanAt:      time.Now().Format(time.RFC3339),
	}
	if runErr != nil {
		entry.ErrorMessage = runErr.Error()
	}
	s.Append(entry)
	_ = saveQueryHistoryStore(s)
}

// ListQueryHistory returns the most recent query-history entries for a
// connection (or every connection, if connectionName is empty).
func (a *App) ListQueryHistory(connectionName string, limit int) ([]queryhistory.Entry, error) {
	s, err := queryHistoryStore()
	if err != nil {
		return nil, err
	}
	return s.List(connectionName, limit), nil
}

// ClearQueryHistory empties the query-history log.
func (a *App) ClearQueryHistory() error {
	s, err := queryHistoryStore()
	if err != nil {
		return err
	}
	s.Clear()
	return saveQueryHistoryStore(s)
}

// RerunFromHistory re-runs a history entry's stored SQL text and returns
// its current result.
func (a *App) RerunFromHistory(entryID string) (engine.SQLResult, error) {
	s, err := queryHistoryStore()
	if err != nil {
		return engine.SQLResult{}, err
	}
	e, ok := s.Find(entryID)
	if !ok {
		return engine.SQLResult{}, fmt.Errorf("no query history entry %q", entryID)
	}
	sess, release, err := a.sqlSession(e.Connection)
	if err != nil {
		return engine.SQLResult{}, err
	}
	defer release()
	return sess.Query(context.Background(), e.Database, e.SQLText)
}
