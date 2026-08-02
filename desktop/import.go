package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// PickCSVFile opens a native file picker for choosing a CSV file to
// import, returning "" if the user cancels.
func (a *App) PickCSVFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose a CSV file to import",
		Filters: []runtime.FileFilter{
			{DisplayName: "CSV files (*.csv)", Pattern: "*.csv"},
		},
	})
}

// ReadCSVHeader returns a CSV file's header row, so the frontend can build
// a column-mapping UI before importing.
func (a *App) ReadCSVHeader(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	header, err := csv.NewReader(f).Read()
	if err != nil {
		return nil, fmt.Errorf("reading CSV header: %w", err)
	}
	return header, nil
}

var importNumericRe = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

func importQuoteIdent(engineID, name string) string {
	if engineID == "mysql" {
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// importLiteral renders a CSV cell for interpolation into an INSERT
// statement. Defensive escaping (quotes doubled), not parameterized-query
// safety — the same tradeoff desktop/frontend/src/lib/sql.ts's sqlLiteral
// makes: acceptable because the statement runs against the user's own
// connection through the same Safe Mode path as any other write, not
// against input from someone else.
func importLiteral(value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.EqualFold(trimmed, "NULL") {
		return "NULL"
	}
	if importNumericRe.MatchString(trimmed) {
		return trimmed
	}
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// ImportCSV bulk-inserts a CSV file's rows into table, one INSERT per row.
// When hasHeaderRow is true, columnMapping maps table column name -> CSV
// header name (matched against the file's first row). When it's false,
// there's no header text to match against, so columnMapping instead maps
// table column name -> the CSV column's 0-based positional index as a
// decimal string (e.g. "0", "1") — the frontend builds it this way when
// the "first row is a header" checkbox is unchecked. Unmapped table
// columns are left out of each INSERT so column defaults apply. Requires
// the connection to be writable, gated the same as RunSQLExecute, since
// this is a write path over an engine.SQLSession.Execute — which takes a
// complete SQL string, not parameterized args, so there's no lower-level
// place to enforce this than here.
func (a *App) ImportCSV(connectionName, database, table, csvPath, engineID string, hasHeaderRow bool, columnMapping map[string]string) (int64, error) {
	if err := a.requireWritable(connectionName); err != nil {
		return 0, err
	}
	if len(columnMapping) == 0 {
		return 0, fmt.Errorf("map at least one column before importing")
	}

	f, err := os.Open(csvPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := csv.NewReader(f)

	var header []string
	if hasHeaderRow {
		header, err = r.Read()
		if err != nil {
			return 0, fmt.Errorf("reading CSV header: %w", err)
		}
	}
	csvIndexByHeader := make(map[string]int, len(header))
	for i, h := range header {
		csvIndexByHeader[h] = i
	}

	type mapping struct {
		tableCol string
		csvIndex int
	}
	mapped := make([]mapping, 0, len(columnMapping))
	for tableCol, csvField := range columnMapping {
		var idx int
		if hasHeaderRow {
			var ok bool
			idx, ok = csvIndexByHeader[csvField]
			if !ok {
				return 0, fmt.Errorf("mapped CSV column %q not found in the file's header", csvField)
			}
		} else {
			var err error
			idx, err = strconv.Atoi(csvField)
			if err != nil || idx < 0 {
				return 0, fmt.Errorf("invalid CSV column position %q for column %q", csvField, tableCol)
			}
		}
		mapped = append(mapped, mapping{tableCol: tableCol, csvIndex: idx})
	}

	ident := importQuoteIdent(engineID, table)
	cols := make([]string, len(mapped))
	for i, m := range mapped {
		cols[i] = importQuoteIdent(engineID, m.tableCol)
	}
	colList := strings.Join(cols, ", ")

	sess, release, err := a.sqlSession(connectionName)
	if err != nil {
		return 0, err
	}
	defer release()
	ctx := context.Background()

	var imported int64
	rowNum := 1
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		rowNum++
		if err != nil {
			return imported, fmt.Errorf("reading CSV row %d: %w", rowNum, err)
		}
		vals := make([]string, len(mapped))
		for i, m := range mapped {
			if m.csvIndex >= len(record) {
				vals[i] = "NULL"
				continue
			}
			vals[i] = importLiteral(record[m.csvIndex])
		}
		sqlText := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", ident, colList, strings.Join(vals, ", "))
		if _, err := sess.Execute(ctx, database, sqlText); err != nil {
			return imported, fmt.Errorf("row %d: %w", rowNum, err)
		}
		imported++
	}
	return imported, nil
}
