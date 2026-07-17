// Package schemadiff compares two databases' table schemas — cross
// connection, cross environment (local vs. staging), or before/after a
// migration — and generates a transactional SQL script to reconcile them.
// This is schema-shape diffing (columns, types, tables), distinct from and
// complementary to internal/snapshot's document-level Mongo diff; the two
// are not merged.
package schemadiff

import (
	"fmt"
	"sort"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
)

// ChangeKind classifies one column's difference between two schema
// snapshots.
type ChangeKind string

const (
	ColumnAdded     ChangeKind = "added"
	ColumnRemoved   ChangeKind = "removed"
	ColumnModified  ChangeKind = "modified"
	ColumnUnchanged ChangeKind = "unchanged"
)

// ColumnDiff is one column's before/after state. Before/After are nil when
// the column doesn't exist on that side (added/removed).
type ColumnDiff struct {
	Name   string         `json:"name"`
	Change ChangeKind     `json:"change"`
	Before *engine.Column `json:"before,omitempty"`
	After  *engine.Column `json:"after,omitempty"`
}

// TableChange classifies a whole table's presence across the two sides.
type TableChange string

const (
	TableAdded     TableChange = "added"
	TableRemoved   TableChange = "removed"
	TableModified  TableChange = "modified"
	TableUnchanged TableChange = "unchanged"
)

// TableDiff is one table's column-level diff.
type TableDiff struct {
	Table   string       `json:"table"`
	Change  TableChange  `json:"change"`
	Columns []ColumnDiff `json:"columns"`
}

// Diff compares two full-schema snapshots (every table on each side) and
// returns one TableDiff per table that appears on either side, sorted by
// name for a stable, reviewable order.
func Diff(before, after []engine.TableSchema) []TableDiff {
	beforeByName := indexByName(before)
	afterByName := indexByName(after)

	names := map[string]bool{}
	for name := range beforeByName {
		names[name] = true
	}
	for name := range afterByName {
		names[name] = true
	}

	out := make([]TableDiff, 0, len(names))
	for name := range names {
		b, hasBefore := beforeByName[name]
		a, hasAfter := afterByName[name]
		switch {
		case hasBefore && !hasAfter:
			out = append(out, TableDiff{Table: name, Change: TableRemoved, Columns: allColumns(&b, nil)})
		case !hasBefore && hasAfter:
			out = append(out, TableDiff{Table: name, Change: TableAdded, Columns: allColumns(nil, &a)})
		default:
			cols := diffColumns(b, a)
			change := TableUnchanged
			for _, c := range cols {
				if c.Change != ColumnUnchanged {
					change = TableModified
					break
				}
			}
			out = append(out, TableDiff{Table: name, Change: change, Columns: cols})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Table < out[j].Table })
	return out
}

func indexByName(schemas []engine.TableSchema) map[string]engine.TableSchema {
	out := make(map[string]engine.TableSchema, len(schemas))
	for _, s := range schemas {
		out[s.Name] = s
	}
	return out
}

func columnsByName(cols []engine.Column) map[string]engine.Column {
	out := make(map[string]engine.Column, len(cols))
	for _, c := range cols {
		out[c.Name] = c
	}
	return out
}

// allColumns renders every column of a wholly added/removed table as
// added/removed column diffs (rather than "modified" against nothing).
func allColumns(before, after *engine.TableSchema) []ColumnDiff {
	var cols []engine.Column
	kind := ColumnAdded
	if before != nil {
		cols = before.Columns
		kind = ColumnRemoved
	} else if after != nil {
		cols = after.Columns
	}
	out := make([]ColumnDiff, len(cols))
	for i, c := range cols {
		col := c
		if kind == ColumnRemoved {
			out[i] = ColumnDiff{Name: c.Name, Change: ColumnRemoved, Before: &col}
		} else {
			out[i] = ColumnDiff{Name: c.Name, Change: ColumnAdded, After: &col}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func diffColumns(before, after engine.TableSchema) []ColumnDiff {
	beforeCols := columnsByName(before.Columns)
	afterCols := columnsByName(after.Columns)

	names := map[string]bool{}
	for name := range beforeCols {
		names[name] = true
	}
	for name := range afterCols {
		names[name] = true
	}

	out := make([]ColumnDiff, 0, len(names))
	for name := range names {
		b, hasBefore := beforeCols[name]
		a, hasAfter := afterCols[name]
		switch {
		case hasBefore && !hasAfter:
			bb := b
			out = append(out, ColumnDiff{Name: name, Change: ColumnRemoved, Before: &bb})
		case !hasBefore && hasAfter:
			aa := a
			out = append(out, ColumnDiff{Name: name, Change: ColumnAdded, After: &aa})
		case b.DataType != a.DataType || b.Nullable != a.Nullable || b.IsPK != a.IsPK:
			bb, aa := b, a
			out = append(out, ColumnDiff{Name: name, Change: ColumnModified, Before: &bb, After: &aa})
		default:
			bb, aa := b, a
			out = append(out, ColumnDiff{Name: name, Change: ColumnUnchanged, Before: &bb, After: &aa})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// HasChanges reports whether any table in diffs actually differs.
func HasChanges(diffs []TableDiff) bool {
	for _, d := range diffs {
		if d.Change != TableUnchanged {
			return true
		}
	}
	return false
}

// fmtColumnDef renders one column as it would appear in a CREATE TABLE.
func fmtColumnDef(c engine.Column) string {
	def := fmt.Sprintf("%s %s", c.Name, c.DataType)
	if !c.Nullable {
		def += " NOT NULL"
	}
	if c.IsPK {
		def += " PRIMARY KEY"
	}
	return def
}
