package schemadiff

import (
	"fmt"
	"strings"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
)

// Migration is a generated reconciliation script plus whatever the caller
// should know before running it.
type Migration struct {
	SQL      string   `json:"sql"`
	Warnings []string `json:"warnings"`
}

// transactionalDDL reports whether dialect rolls back DDL inside a
// transaction. Postgres and SQLite do; MySQL implicitly commits DDL
// statement-by-statement, so wrapping it in BEGIN/COMMIT would be
// misleading about the safety it actually provides.
func transactionalDDL(dialect string) bool {
	switch dialect {
	case "postgres", "sqlite":
		return true
	default:
		return false
	}
}

// GenerateMigration renders diffs as a SQL script for the given dialect.
// Column type/nullability changes are never auto-generated as an ALTER
// COLUMN statement — the syntax and safety of changing a column's type
// varies too much across dialects (and can silently lose data), so those
// are emitted as a review comment instead, consistent with mongobak's
// "explicit about destructive consequences" principle elsewhere in the
// tool. New/dropped tables and added/removed columns are generated
// directly since those are unambiguous.
func GenerateMigration(diffs []TableDiff, dialect string) Migration {
	var sql strings.Builder
	var warnings []string
	transactional := transactionalDDL(dialect)

	if transactional {
		sql.WriteString("BEGIN;\n\n")
	} else {
		warnings = append(warnings, fmt.Sprintf("%s does not run DDL transactionally — statements below apply one at a time, not atomically. Review carefully before running.", dialect))
	}

	any := false
	for _, d := range diffs {
		switch d.Change {
		case TableAdded:
			any = true
			sql.WriteString(renderCreateTable(d))
		case TableRemoved:
			any = true
			fmt.Fprintf(&sql, "DROP TABLE %s;\n\n", d.Table)
		case TableModified:
			for _, c := range d.Columns {
				switch c.Change {
				case ColumnAdded:
					any = true
					fmt.Fprintf(&sql, "ALTER TABLE %s ADD COLUMN %s;\n", d.Table, fmtColumnDef(*c.After))
				case ColumnRemoved:
					any = true
					fmt.Fprintf(&sql, "ALTER TABLE %s DROP COLUMN %s;\n", d.Table, c.Name)
				case ColumnModified:
					any = true
					fmt.Fprintf(&sql, "-- MANUAL REVIEW: %s.%s changed from %q to %q — auto-generating an ALTER COLUMN isn't safe across dialects\n",
						d.Table, c.Name, describeColumn(c.Before), describeColumn(c.After))
				}
			}
			sql.WriteString("\n")
		}
	}

	if !any {
		return Migration{SQL: "-- no schema changes detected\n"}
	}

	if transactional {
		sql.WriteString("COMMIT;\n")
	}
	return Migration{SQL: sql.String(), Warnings: warnings}
}

func renderCreateTable(d TableDiff) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "CREATE TABLE %s (\n", d.Table)
	defs := make([]string, 0, len(d.Columns))
	for _, c := range d.Columns {
		defs = append(defs, "  "+fmtColumnDef(*c.After))
	}
	sb.WriteString(strings.Join(defs, ",\n"))
	sb.WriteString("\n);\n\n")
	return sb.String()
}

func describeColumn(c *engine.Column) string {
	if c == nil {
		return "?"
	}
	extra := ""
	if !c.Nullable {
		extra += " NOT NULL"
	}
	return c.DataType + extra
}
