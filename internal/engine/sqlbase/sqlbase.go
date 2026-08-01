// Package sqlbase holds the logic shared by every database/sql-backed
// engine (postgres, mysql, sqlite): running a query and converting each
// row into the engine package's typed Cell envelope, and running a
// data-modifying statement. Each dialect package supplies its own
// connection/DSN handling and introspection queries; this package supplies
// the generic parts so they aren't duplicated three times.
package sqlbase

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

const QueryRowCap = 500

// RunQuery executes a read query and converts the result set into an
// engine.SQLResult, capping rows at QueryRowCap so an unbounded "SELECT *"
// against a huge table can't exhaust memory. Total is set to the number of
// rows actually returned; ad-hoc SQL isn't re-counted with a second query,
// so a full result (== cap) doesn't necessarily mean there were exactly
// that many rows.
func RunQuery(ctx context.Context, db *sql.DB, sqlText string) (engine.SQLResult, error) {
	rows, err := db.QueryContext(ctx, sqlText)
	if err != nil {
		return engine.SQLResult{}, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return engine.SQLResult{}, err
	}

	result := engine.SQLResult{Columns: cols, Rows: []map[string]engine.Cell{}}
	scanDest := make([]any, len(cols))
	scanBuf := make([]sql.RawBytes, len(cols))
	for i := range scanBuf {
		scanDest[i] = &scanBuf[i]
	}

	// RawBytes gets us the driver's raw representation. To render types
	// correctly (numbers vs strings vs JSON vs binary) we ask the driver
	// what it thinks each column is via ColumnTypes and refine from there.
	colTypes, _ := rows.ColumnTypes()

	for rows.Next() && len(result.Rows) < QueryRowCap {
		for i := range scanBuf {
			scanBuf[i] = nil
		}
		if err := rows.Scan(scanDest...); err != nil {
			return engine.SQLResult{}, err
		}
		row := make(map[string]engine.Cell, len(cols))
		for i, col := range cols {
			var dbType string
			if colTypes != nil && i < len(colTypes) {
				dbType = colTypes[i].DatabaseTypeName()
			}
			row[col] = cellFromRaw(scanBuf[i], dbType)
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return engine.SQLResult{}, err
	}
	result.Total = int64(len(result.Rows))
	return result, nil
}

// StreamRows runs sqlText (expected to be an unfiltered "SELECT * FROM
// table" read of one table) against an already-open transaction and calls
// onRow once per row, with no row cap and no in-memory buffering of the
// full result set — the low-level read path SQL snapshots use to scan a
// table's complete contents, as opposed to RunQuery's capped, display-
// oriented Cell envelope used for interactive browsing.
func StreamRows(ctx context.Context, tx *sql.Tx, sqlText string, onRow func(row map[string]any) error) error {
	rows, err := tx.QueryContext(ctx, sqlText)
	if err != nil {
		return err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	colTypes, _ := rows.ColumnTypes()

	scanBuf := make([]any, len(cols))
	scanDest := make([]any, len(cols))
	for i := range scanBuf {
		scanDest[i] = &scanBuf[i]
	}

	for rows.Next() {
		if err := rows.Scan(scanDest...); err != nil {
			return err
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			var dbType string
			if colTypes != nil && i < len(colTypes) {
				dbType = colTypes[i].DatabaseTypeName()
			}
			row[col] = normalizeStreamedValue(scanBuf[i], dbType)
		}
		if err := onRow(row); err != nil {
			return err
		}
	}
	return rows.Err()
}

// normalizeStreamedValue coerces a driver-scanned value for type-faithful
// JSON round-tripping: binary-typed columns keep their raw []byte (so
// json.Marshal base64-encodes them, and a snapshot restore knows — from
// the column's declared type — to decode them back). Every other column
// that happened to scan as []byte (many database/sql drivers return raw
// bytes for text columns, not string) is converted to string so it
// round-trips as ordinary JSON text instead of being spuriously treated
// as binary.
func normalizeStreamedValue(v any, dbType string) any {
	b, ok := v.([]byte)
	if !ok {
		return v
	}
	if isBinaryDBType(dbType) {
		return b
	}
	return string(b)
}

func isBinaryDBType(dbType string) bool {
	switch dbType {
	case "BYTEA", "BLOB", "BINARY", "VARBINARY":
		return true
	}
	return false
}

// FormatExplainRows runs an EXPLAIN-family query and renders every row as
// plain text, one line per row with columns joined by " | ". Works
// uniformly whether the dialect returns one text column (Postgres'
// default EXPLAIN) or several (MySQL's EXPLAIN, SQLite's EXPLAIN QUERY
// PLAN).
func FormatExplainRows(ctx context.Context, db *sql.DB, explainQuery string) (string, error) {
	rows, err := db.QueryContext(ctx, explainQuery)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}
	scanBuf := make([]sql.RawBytes, len(cols))
	scanDest := make([]any, len(cols))
	for i := range scanBuf {
		scanDest[i] = &scanBuf[i]
	}

	var out string
	for rows.Next() {
		for i := range scanBuf {
			scanBuf[i] = nil
		}
		if err := rows.Scan(scanDest...); err != nil {
			return "", err
		}
		parts := make([]string, len(cols))
		for i, b := range scanBuf {
			if b == nil {
				parts[i] = "NULL"
			} else {
				parts[i] = string(b)
			}
		}
		out += joinPipe(parts) + "\n"
	}
	return out, rows.Err()
}

func joinPipe(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " | "
		}
		out += p
	}
	return out
}

// RunExec executes a data-modifying statement and returns rows affected.
func RunExec(ctx context.Context, db *sql.DB, sqlText string) (int64, error) {
	res, err := db.ExecContext(ctx, sqlText)
	if err != nil {
		return 0, err
	}
	// Not every driver/statement reports affected rows (e.g. DDL); treat
	// that as "unknown" (0) rather than an error.
	n, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return n, nil
}

func cellFromRaw(raw sql.RawBytes, dbType string) engine.Cell {
	if raw == nil {
		return engine.Cell{Type: engine.CellNull, Display: "null"}
	}
	if isBinaryDBType(dbType) {
		return engine.Cell{Type: engine.CellBinary, Display: fmt.Sprintf("<%d bytes>", len(raw))}
	}
	s := string(raw)
	switch dbType {
	case "JSON", "JSONB":
		var v any
		if json.Unmarshal(raw, &v) == nil {
			return engine.Cell{Type: engine.CellJSON, Display: s, Raw: v}
		}
	case "BOOL", "BOOLEAN":
		switch s {
		case "1", "t", "true", "TRUE":
			return engine.Cell{Type: engine.CellBool, Display: "true", Raw: true}
		case "0", "f", "false", "FALSE":
			return engine.Cell{Type: engine.CellBool, Display: "false", Raw: false}
		}
	case "TIME":
		// A bare TIME value (e.g. "14:23:01") has no date component, so
		// none of parseAnyTime's layouts (all of which require one) ever
		// match it — routing it through parseAnyTime just to fail and hit
		// the fallback below would also be wrong on success: time.Parse
		// with a date-less layout fills in the zero date (year 0000), so
		// Display would misleadingly become "0000-01-01T14:23:01Z"
		// instead of the plain time. Pass the raw string straight through
		// for both Display and Raw.
		return engine.Cell{Type: engine.CellDate, Display: s, Raw: s}
	case "TIMESTAMP", "TIMESTAMPTZ", "DATE", "DATETIME":
		if t, err := parseAnyTime(s); err == nil {
			return engine.Cell{Type: engine.CellDate, Display: t.Format(time.RFC3339), Raw: s}
		}
		// Raw is still set here (unlike a bare formatting difference)
		// so a caller depending on it for edit/write-back — e.g. sending
		// the value back unchanged — doesn't silently lose it just
		// because parseAnyTime's layouts didn't recognize this format.
		return engine.Cell{Type: engine.CellDate, Display: s, Raw: s}
	}
	if looksNumeric(dbType) {
		return engine.Cell{Type: engine.CellNumber, Display: s, Raw: s}
	}
	return engine.Cell{Type: engine.CellString, Display: s, Raw: s}
}

func looksNumeric(dbType string) bool {
	switch dbType {
	case "INT", "INT2", "INT4", "INT8", "INTEGER", "SMALLINT", "BIGINT",
		"FLOAT4", "FLOAT8", "FLOAT", "DOUBLE", "REAL", "NUMERIC", "DECIMAL",
		"SERIAL", "BIGSERIAL":
		return true
	}
	return false
}

var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func parseAnyTime(s string) (time.Time, error) {
	var lastErr error
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, lastErr
}
