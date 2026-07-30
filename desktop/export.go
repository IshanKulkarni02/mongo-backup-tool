package main

import (
	"encoding/csv"
	"encoding/json"
	"os"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

// ExportQueryResultsCSV opens a native "Save As" dialog and writes result
// as CSV. Returns "" (with no error) if the user cancels the dialog.
// Writes directly to disk in Go rather than shipping the result across
// the Wails IPC boundary to trigger a browser-style download — not the
// right primitive for a large result set in a desktop app.
func (a *App) ExportQueryResultsCSV(result engine.SQLResult, suggestedName string) (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:                "Export query results as CSV",
		DefaultFilename:      suggestedName + ".csv",
		CanCreateDirectories: true,
		Filters: []runtime.FileFilter{
			{DisplayName: "CSV files (*.csv)", Pattern: "*.csv"},
		},
	})
	if err != nil || path == "" {
		return "", err
	}

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write(result.Columns); err != nil {
		return "", err
	}
	record := make([]string, len(result.Columns))
	for _, row := range result.Rows {
		for i, col := range result.Columns {
			record[i] = row[col].Display
		}
		if err := w.Write(record); err != nil {
			return "", err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", err
	}
	return path, nil
}

// ExportQueryResultsJSON opens a native "Save As" dialog and writes result
// as a JSON array of objects, one per row. Each field prefers the cell's
// typed Raw value (so numbers/booleans round-trip as JSON numbers/booleans,
// not strings) and falls back to Display when Raw isn't set. Returns "" if
// the user cancels.
func (a *App) ExportQueryResultsJSON(result engine.SQLResult, suggestedName string) (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:                "Export query results as JSON",
		DefaultFilename:      suggestedName + ".json",
		CanCreateDirectories: true,
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON files (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil || path == "" {
		return "", err
	}

	rows := make([]map[string]any, len(result.Rows))
	for i, row := range result.Rows {
		obj := make(map[string]any, len(result.Columns))
		for _, col := range result.Columns {
			cell := row[col]
			if cell.Raw != nil {
				obj[col] = cell.Raw
			} else {
				obj[col] = cell.Display
			}
		}
		rows[i] = obj
	}

	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
