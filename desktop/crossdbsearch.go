package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

// CrossSearchMatch is one hit from RunCrossDatabaseSearch.
type CrossSearchMatch struct {
	Connection string `json:"connection"`
	Engine     string `json:"engine"`
	Database   string `json:"database"`
	Namespace  string `json:"namespace"`
	Preview    string `json:"preview"`
}

// crossSearchRowLimit caps matches kept per table/collection, so one huge
// table can't crowd out every other connection's results.
const crossSearchRowLimit = 20

// crossSearchSampleLimit is how many documents are pulled to infer a
// schemaless Mongo collection's field names before the real filtered query.
const crossSearchSampleLimit = 50

// RunCrossDatabaseSearch starts a cancelable background job (type
// "cross-db-search") that searches term across every saved connection's
// databases and tables/collections. Progress streams via the existing
// "job:progress" event (phase = the connection currently being searched);
// the result ([]CrossSearchMatch) arrives via "job:update", like any other
// job. Mirrors PullOllamaModel's shape for wiring a job ID into a
// progress-reporting closure — jobManager.runCancelable doesn't expose the
// ID to its callback, so this calls jobs.start/cancels/finish directly.
func (a *App) RunCrossDatabaseSearch(term string) string {
	j := a.jobs.start("cross-db-search")
	ctx, cancel := context.WithCancel(context.Background())
	a.jobs.mu.Lock()
	a.jobs.cancels[j.ID] = cancel
	a.jobs.mu.Unlock()
	go func() {
		matches, err := a.crossDatabaseSearch(ctx, j.ID, term)
		a.jobs.mu.Lock()
		delete(a.jobs.cancels, j.ID)
		a.jobs.mu.Unlock()
		a.jobs.finish(j.ID, err, matches)
	}()
	return j.ID
}

func (a *App) crossDatabaseSearch(ctx context.Context, jobID, term string) ([]CrossSearchMatch, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	var matches []CrossSearchMatch
	total := int64(len(cfg.Connections))
	for i, conn := range cfg.Connections {
		select {
		case <-ctx.Done():
			return matches, ctx.Err()
		default:
		}
		a.jobs.progress(jobID, conn.Name, int64(i), total, "")

		sess, release, err := a.engines.Acquire(ctx, conn.Name)
		if err != nil {
			continue
		}
		databases, err := sess.ListDatabases(ctx)
		if err != nil {
			release()
			continue
		}
		for _, db := range databases {
			select {
			case <-ctx.Done():
				release()
				return matches, ctx.Err()
			default:
			}
			namespaces, err := sess.ListNamespaces(ctx, db)
			if err != nil {
				continue
			}
			if sqlSess, ok := sess.(engine.SQLSession); ok {
				matches = append(matches, searchSQLNamespaces(ctx, sqlSess, conn.EngineID(), conn.Name, db, namespaces, term)...)
			}
			if docSess, ok := sess.(engine.DocumentSession); ok {
				matches = append(matches, searchDocumentNamespaces(ctx, docSess, conn.EngineID(), conn.Name, db, namespaces, term)...)
			}
		}
		release()
	}
	a.jobs.progress(jobID, "done", total, total, "")
	return matches, nil
}

func searchSQLNamespaces(ctx context.Context, sess engine.SQLSession, engineID, connName, database string, namespaces []engine.NamespaceInfo, term string) []CrossSearchMatch {
	var out []CrossSearchMatch
	pattern := "'%" + strings.ReplaceAll(term, "'", "''") + "%'"
	for _, ns := range namespaces {
		select {
		case <-ctx.Done():
			return out
		default:
		}
		schema, err := sess.TableSchema(ctx, database, ns.Name)
		if err != nil || len(schema.Columns) == 0 {
			continue
		}
		ident := importQuoteIdent(engineID, ns.Name)
		conds := make([]string, 0, len(schema.Columns))
		for _, c := range schema.Columns {
			colIdent := importQuoteIdent(engineID, c.Name)
			if engineID == "postgres" {
				// Postgres requires an explicit cast for LIKE against a
				// non-text column; MySQL/SQLite coerce implicitly.
				conds = append(conds, colIdent+"::text LIKE "+pattern)
			} else {
				conds = append(conds, colIdent+" LIKE "+pattern)
			}
		}
		sqlText := fmt.Sprintf("SELECT * FROM %s WHERE %s LIMIT %d", ident, strings.Join(conds, " OR "), crossSearchRowLimit)
		result, err := sess.Query(ctx, database, sqlText)
		if err != nil {
			continue
		}
		for _, row := range result.Rows {
			out = append(out, CrossSearchMatch{
				Connection: connName,
				Engine:     engineID,
				Database:   database,
				Namespace:  ns.Name,
				Preview:    previewRow(row, schema.Columns),
			})
		}
	}
	return out
}

func previewRow(row map[string]engine.Cell, columns []engine.Column) string {
	parts := make([]string, 0, len(columns))
	for _, c := range columns {
		cell, ok := row[c.Name]
		if !ok {
			continue
		}
		display := cell.Display
		if len(display) > 40 {
			display = display[:40] + "…"
		}
		parts = append(parts, c.Name+"="+display)
		if len(parts) >= 5 {
			break
		}
	}
	return strings.Join(parts, ", ")
}

// searchDocumentNamespaces handles Mongo's schemaless collections: there's
// no TableSchema equivalent for documents, so a small sample page infers
// the field names actually present before building the real $or/$regex
// filter — pushing the match down to MongoDB's query engine per
// collection, rather than paginating through every document and grepping
// client-side.
func searchDocumentNamespaces(ctx context.Context, sess engine.DocumentSession, engineID, connName, database string, namespaces []engine.NamespaceInfo, term string) []CrossSearchMatch {
	var out []CrossSearchMatch
	escapedTerm := regexp.QuoteMeta(term)
	for _, ns := range namespaces {
		select {
		case <-ctx.Done():
			return out
		default:
		}
		sample, err := sess.QueryDocuments(ctx, engine.DocQuery{Database: database, Namespace: ns.Name, Limit: crossSearchSampleLimit})
		if err != nil || len(sample.Documents) == 0 {
			continue
		}
		fields := inferFieldNames(sample.Documents)
		if len(fields) == 0 {
			continue
		}
		orClauses := make([]string, 0, len(fields))
		for _, f := range fields {
			fieldJSON, _ := json.Marshal(f)
			patternJSON, _ := json.Marshal(escapedTerm)
			orClauses = append(orClauses, fmt.Sprintf(`{%s: {"$regex": %s, "$options": "i"}}`, fieldJSON, patternJSON))
		}
		filterJSON := `{"$or": [` + strings.Join(orClauses, ",") + `]}`
		page, err := sess.QueryDocuments(ctx, engine.DocQuery{Database: database, Namespace: ns.Name, FilterJSON: filterJSON, Limit: crossSearchRowLimit})
		if err != nil {
			continue
		}
		for _, doc := range page.Documents {
			out = append(out, CrossSearchMatch{
				Connection: connName,
				Engine:     engineID,
				Database:   database,
				Namespace:  ns.Name,
				Preview:    previewDoc(doc),
			})
		}
	}
	return out
}

// inferFieldNames collects the union of top-level keys across a sample of
// documents, capped so a very wide/varied collection doesn't blow up the
// $or filter into something impractically large.
func inferFieldNames(docs []string) []string {
	const maxFields = 20
	seen := make(map[string]bool)
	var fields []string
	for _, d := range docs {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(d), &obj); err != nil {
			continue
		}
		for k := range obj {
			if seen[k] {
				continue
			}
			seen[k] = true
			fields = append(fields, k)
			if len(fields) >= maxFields {
				return fields
			}
		}
	}
	return fields
}

func previewDoc(doc string) string {
	if len(doc) > 200 {
		return doc[:200] + "…"
	}
	return doc
}
