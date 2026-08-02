package main

import (
	"context"
	"fmt"
	"time"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/engine/safeguard"
)

// sqlSession acquires the cached SQL session for a connection. The caller
// must invoke the returned release func when done.
func (a *App) sqlSession(connectionName string) (engine.SQLSession, func(), error) {
	sess, release, err := a.engines.Acquire(context.Background(), connectionName)
	if err != nil {
		return nil, nil, err
	}
	ss, ok := sess.(engine.SQLSession)
	if !ok {
		release()
		return nil, nil, fmt.Errorf("connection %q isn't a SQL database", connectionName)
	}
	return ss, release, nil
}

// TableInfo is one table's summary, shown in the SQL browser's tree.
type TableInfo struct {
	Name        string `json:"name"`
	RowCount    int64  `json:"rowCount"`
	StorageSize int64  `json:"storageSize"`
}

// ListTables returns every table in a database (for Postgres, "database"
// is the schema; see internal/engine/postgres).
func (a *App) ListTables(connectionName, database string) ([]TableInfo, error) {
	sess, release, err := a.sqlSession(connectionName)
	if err != nil {
		return nil, err
	}
	defer release()

	infos, err := sess.ListNamespaces(context.Background(), database)
	if err != nil {
		return nil, err
	}
	out := make([]TableInfo, len(infos))
	for i, ns := range infos {
		out[i] = TableInfo{Name: ns.Name, RowCount: ns.DocCount, StorageSize: ns.StorageSize}
	}
	return out, nil
}

// IncomingForeignKey is one other table's column that references a row in
// the table being inspected.
type IncomingForeignKey struct {
	Table  string `json:"table"`
	Column string `json:"column"`
}

// ListReferencingTables finds every column, in every other table in
// database, whose foreign key points at table. This composes each table's
// TableSchema (which reports outgoing FKs) rather than a per-dialect
// reverse-lookup query, trading a query-per-table for one implementation
// that works identically across Postgres, MySQL, and SQLite — the
// relationship inspector's "who references this row's table" panel.
func (a *App) ListReferencingTables(connectionName, database, table string) ([]IncomingForeignKey, error) {
	sess, release, err := a.sqlSession(connectionName)
	if err != nil {
		return nil, err
	}
	defer release()

	ctx := context.Background()
	namespaces, err := sess.ListNamespaces(ctx, database)
	if err != nil {
		return nil, err
	}

	out := []IncomingForeignKey{}
	for _, ns := range namespaces {
		if ns.Name == table {
			continue
		}
		schema, err := sess.TableSchema(ctx, database, ns.Name)
		if err != nil {
			continue // a table we can't introspect just doesn't show up
		}
		for _, fk := range schema.ForeignKeys {
			if fk.RefTable == table {
				out = append(out, IncomingForeignKey{Table: ns.Name, Column: fk.Column})
			}
		}
	}
	return out, nil
}

// GetTableSchema returns one table's columns and foreign keys.
func (a *App) GetTableSchema(connectionName, database, table string) (engine.TableSchema, error) {
	sess, release, err := a.sqlSession(connectionName)
	if err != nil {
		return engine.TableSchema{}, err
	}
	defer release()
	return sess.TableSchema(context.Background(), database, table)
}

// checkQueryStatement gates a statement arriving through a "query" RPC
// (RunSQLQuery/RunSQLQueryJob/ExplainSQL) — paths meant only for reads, but
// which reach db.QueryContext directly with no requireWritable/Classify
// check of their own. A writable CTE (e.g. "WITH x AS (DELETE ...) SELECT
// * FROM x") looks like a read to the frontend's client-side routing but
// isn't one, so read-only enforcement must not depend on which RPC the
// frontend happened to route the statement through. Statements that really
// are reads (per safeguard.IsRead) skip this entirely — a read-only
// connection must still be able to read.
func checkQueryStatement(sqlText string, requireWritable func() error) error {
	if safeguard.IsRead(sqlText) {
		return nil
	}
	if err := requireWritable(); err != nil {
		return err
	}
	if class := safeguard.Classify(sqlText); class.Risk == safeguard.RiskDangerous {
		return fmt.Errorf("dangerous statement (%s) — run it via Execute instead, with confirmation", class.Reason)
	}
	return nil
}

// RunSQLQuery runs a read query and returns a typed result page. Used by
// the bounded, fast table-browser path (TableView); the ad-hoc SQL editor
// uses the cancelable RunSQLQueryJob instead, since arbitrary user SQL can
// run arbitrarily long.
func (a *App) RunSQLQuery(connectionName, database, sqlText string) (engine.SQLResult, error) {
	if err := checkQueryStatement(sqlText, func() error { return a.requireWritable(connectionName) }); err != nil {
		return engine.SQLResult{}, err
	}
	sess, release, err := a.sqlSession(connectionName)
	if err != nil {
		return engine.SQLResult{}, err
	}
	defer release()
	start := time.Now()
	result, err := sess.Query(context.Background(), database, sqlText)
	recordQueryHistory(connectionName, database, sqlText, len(result.Rows), time.Since(start), err)
	return result, err
}

// RunSQLQueryJob starts a query as a cancelable background job and returns
// its ID immediately; the result (an engine.SQLResult, or an error
// message) arrives via the "job:update" event with job type "sql-query".
// Call CancelJob(id) to abort a long-running query.
func (a *App) RunSQLQueryJob(connectionName, database, sqlText string) string {
	return a.jobs.runCancelable("sql-query", func(ctx context.Context) (any, error) {
		if err := checkQueryStatement(sqlText, func() error { return a.requireWritable(connectionName) }); err != nil {
			return nil, err
		}
		sess, release, err := a.sqlSession(connectionName)
		if err != nil {
			return nil, err
		}
		defer release()
		start := time.Now()
		result, err := sess.Query(ctx, database, sqlText)
		recordQueryHistory(connectionName, database, sqlText, len(result.Rows), time.Since(start), err)
		return result, err
	})
}

// ClassifySQL returns a statement's Safe Mode risk classification, so the
// frontend can render an appropriate confirmation before calling
// RunSQLExecute.
func (a *App) ClassifySQL(sqlText string) safeguard.Classification {
	return safeguard.Classify(sqlText)
}

// RunSQLExecute runs a data-modifying statement and returns rows affected.
// A statement classified as safeguard.RiskDangerous (DROP, TRUNCATE, ALTER,
// or an unqualified DELETE/UPDATE) is refused unless confirmDatabaseName
// exactly matches database — the UI's "type the database name to confirm"
// gate, enforced here rather than only in the frontend. Read-only
// connections refuse every write regardless of risk level.
func (a *App) RunSQLExecute(connectionName, database, sqlText, confirmDatabaseName string) (int64, error) {
	if err := a.requireWritable(connectionName); err != nil {
		return 0, err
	}
	class := safeguard.Classify(sqlText)
	if class.Risk == safeguard.RiskDangerous && confirmDatabaseName != database {
		return 0, fmt.Errorf("dangerous statement (%s) — type the database name %q to confirm", class.Reason, database)
	}
	sess, release, err := a.sqlSession(connectionName)
	if err != nil {
		return 0, err
	}
	defer release()
	start := time.Now()
	rows, err := sess.Execute(context.Background(), database, sqlText)
	recordQueryHistory(connectionName, database, sqlText, int(rows), time.Since(start), err)
	return rows, err
}

// ExplainSQL returns the database's query-plan text for sqlText. On
// Postgres, a leading "ANALYZE " modifier makes the engine's Explain
// genuinely execute the wrapped statement (SQLSession.Explain builds
// "EXPLAIN "+sqlText) — e.g. ExplainSQL(..., "ANALYZE DELETE FROM t") runs
// EXPLAIN ANALYZE DELETE FROM t, which really deletes rows. That modifier
// is stripped before classifying so a genuine "ANALYZE SELECT ..." still
// reads as a read (no gating) while "ANALYZE DELETE ..." does not.
func (a *App) ExplainSQL(connectionName, database, sqlText string) (string, error) {
	inner := safeguard.StripExplainAnalyze(sqlText)
	if err := checkQueryStatement(inner, func() error { return a.requireWritable(connectionName) }); err != nil {
		return "", err
	}
	sess, release, err := a.sqlSession(connectionName)
	if err != nil {
		return "", err
	}
	defer release()
	return sess.Explain(context.Background(), database, sqlText)
}
