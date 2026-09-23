// Package guard is the boundary an untrusted caller — an AI agent reaching
// the databases through the broker — goes through for reads. It layers
// three independent defences:
//
//  1. a text pre-filter (safeguard.IsRead, and for documents a scan for
//     server-side JavaScript operators), which produces friendly errors;
//  2. database-enforced read-only execution (engine.ReadOnlySQLSession —
//     Postgres/MySQL READ ONLY transactions, SQLite query_only), which is
//     what actually stops a write that slips past the text filter; and
//  3. hard limits on rows, cell size and wall-clock time, with an explicit
//     Truncated flag so a cap is never silent.
//
// Errors from this package are *Error values carrying a stable Code, and
// their messages are passed through Scrub so a driver error can't leak a
// connection string or host to the agent.
package guard

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/engine/safeguard"
)

// Code is a stable, machine-readable failure class.
type Code string

const (
	CodeReadOnlyViolation Code = "READ_ONLY_VIOLATION"
	CodeUnsupported       Code = "UNSUPPORTED"
	CodeBadRequest        Code = "BAD_REQUEST"
	CodeQueryFailed       Code = "QUERY_FAILED"
)

// Error is a guard failure with a stable code and a hint on what to do next.
type Error struct {
	Code    Code
	Message string
	Hint    string
}

func (e *Error) Error() string { return e.Message }

func fail(code Code, hint, format string, args ...any) *Error {
	return &Error{Code: code, Message: Scrub(fmt.Sprintf(format, args...)), Hint: hint}
}

// Limits bounds one agent read.
type Limits struct {
	MaxRows      int
	MaxCellBytes int
	Timeout      time.Duration
}

// DefaultLimits are deliberately small: an agent reading a table needs a
// sample and the schema, not a bulk export.
func DefaultLimits() Limits {
	return Limits{MaxRows: 100, MaxCellBytes: 4096, Timeout: 15 * time.Second}
}

func (l Limits) normalized() Limits {
	d := DefaultLimits()
	if l.MaxRows <= 0 {
		l.MaxRows = d.MaxRows
	}
	if l.MaxCellBytes <= 0 {
		l.MaxCellBytes = d.MaxCellBytes
	}
	if l.Timeout <= 0 {
		l.Timeout = d.Timeout
	}
	return l
}

// docPageCap is the most documents the Mongo engine returns per page or
// pipeline; limits are clamped below it so its silent cap never applies.
const docPageCap = 199

// ReadSQL runs a read-only SQL statement. Anything safeguard.IsRead does not
// recognise as a pure read is refused up front; a read that gets past that
// still runs inside a database-enforced read-only transaction.
func ReadSQL(ctx context.Context, sess engine.SQLSession, database, sqlText string, lim Limits) (engine.SQLResult, error) {
	if strings.TrimSpace(sqlText) == "" {
		return engine.SQLResult{}, fail(CodeBadRequest, "send a SELECT statement", "empty statement")
	}
	if !safeguard.IsRead(sqlText) {
		class := safeguard.Classify(sqlText)
		return engine.SQLResult{}, fail(CodeReadOnlyViolation,
			"use `write` to request a change; DBHelm asks the user before running it",
			"statement is not a read (%s)", nonEmpty(class.Reason, "not recognised as SELECT/SHOW/EXPLAIN/PRAGMA"))
	}
	ro, ok := sess.(engine.ReadOnlySQLSession)
	if !ok {
		return engine.SQLResult{}, fail(CodeUnsupported, "", "this engine cannot enforce read-only execution")
	}
	lim = lim.normalized()
	res, err := ro.QueryReadOnly(ctx, database, sqlText, engine.ReadLimits{
		MaxRows: lim.MaxRows, MaxCellBytes: lim.MaxCellBytes, Timeout: lim.Timeout,
	})
	if err != nil {
		return engine.SQLResult{}, wrap(err)
	}
	return res, nil
}

// Explain returns a query plan without executing the statement. EXPLAIN
// ANALYZE really runs the wrapped statement, so it is refused outright.
func Explain(ctx context.Context, sess engine.SQLSession, database, sqlText string) (string, error) {
	if safeguard.StripExplainAnalyze(sqlText) != sqlText {
		return "", fail(CodeReadOnlyViolation, "ask for a plain EXPLAIN", "ANALYZE executes the statement and is not allowed")
	}
	if !safeguard.IsRead(sqlText) {
		return "", fail(CodeReadOnlyViolation, "explain a SELECT", "only reads can be explained")
	}
	out, err := sess.Explain(ctx, database, sqlText)
	if err != nil {
		return "", wrap(err)
	}
	return out, nil
}

// ReadDocuments runs a filtered document read with the limit clamped.
func ReadDocuments(ctx context.Context, sess engine.DocumentSession, q engine.DocQuery, lim Limits) (engine.DocPage, bool, error) {
	lim = lim.normalized()
	if err := checkJSON(q.FilterJSON); err != nil {
		return engine.DocPage{}, false, err
	}
	max := min(lim.MaxRows, docPageCap)
	if q.Limit <= 0 || q.Limit > max {
		q.Limit = max
	}
	ctx, cancel := context.WithTimeout(ctx, lim.Timeout)
	defer cancel()
	page, err := sess.QueryDocuments(ctx, q)
	if err != nil {
		return engine.DocPage{}, false, wrap(err)
	}
	truncated := page.Total > int64(page.Skip)+int64(len(page.Documents))
	page.Documents = capDocs(page.Documents, lim.MaxCellBytes)
	return page, truncated, nil
}

// Aggregate runs a read-only aggregation pipeline. Pipelines that persist
// data ($out, $merge) or execute server-side JavaScript are refused, and a
// $limit stage is appended so the server stops early.
func Aggregate(ctx context.Context, sess engine.AggregateSession, database, collection, pipelineJSON string, lim Limits) ([]string, bool, error) {
	lim = lim.normalized()
	var stages []bson.D
	if err := bson.UnmarshalExtJSON([]byte(pipelineJSON), true, &stages); err != nil {
		return nil, false, fail(CodeBadRequest, "send a JSON array of stage documents", "invalid pipeline: %v", err)
	}
	for _, stage := range stages {
		for _, e := range stage {
			if e.Key == "$out" || e.Key == "$merge" {
				return nil, false, fail(CodeReadOnlyViolation, "use `write` to request a change", "stage %s writes data", e.Key)
			}
		}
		if key, bad := findForbiddenKey(stage); bad {
			return nil, false, fail(CodeReadOnlyViolation, "rewrite without server-side JavaScript", "operator %s is not allowed", key)
		}
	}
	max := min(lim.MaxRows, docPageCap)
	stages = append(stages, bson.D{{Key: "$limit", Value: int64(max + 1)}})
	parts := make([]string, len(stages))
	for i, st := range stages {
		b, err := bson.MarshalExtJSON(st, true, false)
		if err != nil {
			return nil, false, fail(CodeBadRequest, "", "could not re-encode pipeline: %v", err)
		}
		parts[i] = string(b)
	}
	bounded := "[" + strings.Join(parts, ",") + "]"
	ctx, cancel := context.WithTimeout(ctx, lim.Timeout)
	defer cancel()
	docs, err := sess.Aggregate(ctx, database, collection, bounded)
	if err != nil {
		return nil, false, wrap(err)
	}
	truncated := false
	if len(docs) > max {
		docs, truncated = docs[:max], true
	}
	return capDocs(docs, lim.MaxCellBytes), truncated, nil
}

// forbiddenOperators run arbitrary JavaScript on the server.
var forbiddenOperators = map[string]bool{"$where": true, "$function": true, "$accumulator": true}

func checkJSON(filterJSON string) error {
	if strings.TrimSpace(filterJSON) == "" {
		return nil
	}
	var doc bson.D
	if err := bson.UnmarshalExtJSON([]byte(filterJSON), true, &doc); err != nil {
		return fail(CodeBadRequest, "send a JSON object", "invalid filter: %v", err)
	}
	if key, bad := findForbiddenKey(doc); bad {
		return fail(CodeReadOnlyViolation, "rewrite without server-side JavaScript", "operator %s is not allowed", key)
	}
	return nil
}

// findForbiddenKey walks a parsed BSON value looking for a JavaScript operator.
func findForbiddenKey(v any) (string, bool) {
	switch t := v.(type) {
	case bson.D:
		for _, e := range t {
			if forbiddenOperators[e.Key] {
				return e.Key, true
			}
			if k, ok := findForbiddenKey(e.Value); ok {
				return k, true
			}
		}
	case bson.A:
		for _, e := range t {
			if k, ok := findForbiddenKey(e); ok {
				return k, true
			}
		}
	case bson.M:
		for key, e := range t {
			if forbiddenOperators[key] {
				return key, true
			}
			if k, ok := findForbiddenKey(e); ok {
				return k, true
			}
		}
	}
	return "", false
}

func capDocs(docs []string, maxBytes int) []string {
	for i, d := range docs {
		if maxBytes > 0 && len(d) > maxBytes {
			docs[i] = d[:maxBytes] + fmt.Sprintf("… [document truncated: %d bytes]", len(d))
		}
	}
	return docs
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func wrap(err error) *Error {
	if ge, ok := err.(*Error); ok {
		return ge
	}
	return &Error{Code: CodeQueryFailed, Message: Scrub(err.Error())}
}

var (
	uriRe      = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.\-]*://[^\s"'<>]+`)
	mysqlDSNRe = regexp.MustCompile(`[^\s@/"']+:[^\s@/"']*@(?:tcp|unix)\([^)]*\)[^\s"']*`)
	secretKVRe = regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|apikey|api_key|sslpassword)=[^\s&;"']+`)
	hostPortRe = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}:\d{2,5}\b`)
)

// Scrub removes connection strings, DSNs, credential key=value pairs and
// IP:port pairs from text, so an error from a database driver can be shown
// to an agent without revealing where or how DBHelm connects.
func Scrub(s string) string {
	s = uriRe.ReplaceAllString(s, "<redacted-uri>")
	s = mysqlDSNRe.ReplaceAllString(s, "<redacted-dsn>")
	s = secretKVRe.ReplaceAllString(s, "$1=<redacted>")
	s = hostPortRe.ReplaceAllString(s, "<redacted-host>")
	return s
}
