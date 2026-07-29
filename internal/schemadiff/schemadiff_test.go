package schemadiff

import (
	"strings"
	"testing"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
)

func col(name, dataType string, nullable, pk bool) engine.Column {
	return engine.Column{Name: name, DataType: dataType, Nullable: nullable, IsPK: pk}
}

func TestDiffUnchangedTable(t *testing.T) {
	before := []engine.TableSchema{{Name: "users", Columns: []engine.Column{col("id", "INT", false, true), col("email", "TEXT", false, false)}}}
	after := []engine.TableSchema{{Name: "users", Columns: []engine.Column{col("id", "INT", false, true), col("email", "TEXT", false, false)}}}

	diffs := Diff(before, after)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 table diff, got %d", len(diffs))
	}
	if diffs[0].Change != TableUnchanged {
		t.Fatalf("expected unchanged, got %s", diffs[0].Change)
	}
	if HasChanges(diffs) {
		t.Fatal("expected HasChanges to report false for an identical schema")
	}
}

func TestDiffAddedTable(t *testing.T) {
	after := []engine.TableSchema{{Name: "orders", Columns: []engine.Column{col("id", "INT", false, true)}}}
	diffs := Diff(nil, after)
	if len(diffs) != 1 || diffs[0].Change != TableAdded {
		t.Fatalf("expected 1 added table, got %+v", diffs)
	}
	if diffs[0].Columns[0].Change != ColumnAdded {
		t.Fatalf("expected column marked added, got %+v", diffs[0].Columns)
	}
}

func TestDiffRemovedTable(t *testing.T) {
	before := []engine.TableSchema{{Name: "legacy", Columns: []engine.Column{col("id", "INT", false, true)}}}
	diffs := Diff(before, nil)
	if len(diffs) != 1 || diffs[0].Change != TableRemoved {
		t.Fatalf("expected 1 removed table, got %+v", diffs)
	}
}

func TestDiffColumnAddedRemovedModified(t *testing.T) {
	before := []engine.TableSchema{{Name: "users", Columns: []engine.Column{
		col("id", "INT", false, true),
		col("legacy_flag", "INT", true, false),
		col("age", "INT", true, false),
	}}}
	after := []engine.TableSchema{{Name: "users", Columns: []engine.Column{
		col("id", "INT", false, true),
		col("email", "TEXT", false, false), // added
		col("age", "TEXT", true, false),    // modified: type changed
	}}}

	diffs := Diff(before, after)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 table, got %d", len(diffs))
	}
	d := diffs[0]
	if d.Change != TableModified {
		t.Fatalf("expected modified, got %s", d.Change)
	}

	byName := map[string]ColumnDiff{}
	for _, c := range d.Columns {
		byName[c.Name] = c
	}
	if byName["email"].Change != ColumnAdded {
		t.Fatalf("expected email added, got %+v", byName["email"])
	}
	if byName["legacy_flag"].Change != ColumnRemoved {
		t.Fatalf("expected legacy_flag removed, got %+v", byName["legacy_flag"])
	}
	if byName["age"].Change != ColumnModified {
		t.Fatalf("expected age modified, got %+v", byName["age"])
	}
	if byName["id"].Change != ColumnUnchanged {
		t.Fatalf("expected id unchanged, got %+v", byName["id"])
	}
}

func TestGenerateMigrationPostgresIsTransactional(t *testing.T) {
	after := []engine.TableSchema{{Name: "orders", Columns: []engine.Column{col("id", "INT", false, true)}}}
	diffs := Diff(nil, after)
	m := GenerateMigration(diffs, "postgres")

	want := "BEGIN;\n\nCREATE TABLE orders (\n  id INT NOT NULL PRIMARY KEY\n);\n\nCOMMIT;\n"
	if m.SQL != want {
		t.Fatalf("unexpected SQL:\ngot:\n%s\nwant:\n%s", m.SQL, want)
	}
	if len(m.Warnings) != 0 {
		t.Fatalf("expected no warnings for a transactional dialect, got %v", m.Warnings)
	}
}

func TestGenerateMigrationMySQLWarnsNonTransactional(t *testing.T) {
	after := []engine.TableSchema{{Name: "orders", Columns: []engine.Column{col("id", "INT", false, true)}}}
	diffs := Diff(nil, after)
	m := GenerateMigration(diffs, "mysql")

	if len(m.Warnings) != 1 {
		t.Fatalf("expected 1 warning for mysql, got %v", m.Warnings)
	}
	if m.SQL[:5] == "BEGIN" {
		t.Fatalf("expected no BEGIN wrapper for mysql, got:\n%s", m.SQL)
	}
}

func TestGenerateMigrationAddedAndRemovedColumns(t *testing.T) {
	before := []engine.TableSchema{{Name: "users", Columns: []engine.Column{col("id", "INT", false, true), col("old_col", "TEXT", true, false)}}}
	after := []engine.TableSchema{{Name: "users", Columns: []engine.Column{col("id", "INT", false, true), col("email", "TEXT", false, false)}}}

	diffs := Diff(before, after)
	m := GenerateMigration(diffs, "postgres")

	if !strings.Contains(m.SQL, "ALTER TABLE users ADD COLUMN email TEXT NOT NULL;") {
		t.Fatalf("expected ADD COLUMN statement, got:\n%s", m.SQL)
	}
	if !strings.Contains(m.SQL, "ALTER TABLE users DROP COLUMN old_col;") {
		t.Fatalf("expected DROP COLUMN statement, got:\n%s", m.SQL)
	}
}

func TestGenerateMigrationModifiedColumnIsManualReviewOnly(t *testing.T) {
	before := []engine.TableSchema{{Name: "users", Columns: []engine.Column{col("age", "INT", true, false)}}}
	after := []engine.TableSchema{{Name: "users", Columns: []engine.Column{col("age", "TEXT", true, false)}}}

	diffs := Diff(before, after)
	m := GenerateMigration(diffs, "postgres")

	if !strings.Contains(m.SQL, "MANUAL REVIEW") {
		t.Fatalf("expected a manual-review comment for a type change, got:\n%s", m.SQL)
	}
	if strings.Contains(m.SQL, "ALTER TABLE users ALTER COLUMN") {
		t.Fatal("expected no auto-generated ALTER COLUMN for a type change")
	}
}

func TestGenerateMigrationNoChangesIsANoOp(t *testing.T) {
	same := []engine.TableSchema{{Name: "users", Columns: []engine.Column{col("id", "INT", false, true)}}}
	diffs := Diff(same, same)
	m := GenerateMigration(diffs, "postgres")
	if m.SQL != "-- no schema changes detected\n" {
		t.Fatalf("expected a no-op message, got:\n%s", m.SQL)
	}
}
