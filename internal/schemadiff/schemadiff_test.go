package schemadiff

import (
	"strings"
	"testing"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

func col(name, dataType string, nullable, pk bool) engine.Column {
	return engine.Column{Name: name, DataType: dataType, Nullable: nullable, IsPK: pk}
}

func TestDiffUnchangedTable(t *testing.T) {
	before := []engine.TableSchema{{Name: "users", Columns: []engine.Column{col("id", "INT", false, true), col("email", "TEXT", false, false)}, PrimaryKey: []string{"id"}}}
	after := []engine.TableSchema{{Name: "users", Columns: []engine.Column{col("id", "INT", false, true), col("email", "TEXT", false, false)}, PrimaryKey: []string{"id"}}}

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
	after := []engine.TableSchema{{Name: "orders", Columns: []engine.Column{col("id", "INT", false, true)}, PrimaryKey: []string{"id"}}}
	diffs := Diff(nil, after)
	if len(diffs) != 1 || diffs[0].Change != TableAdded {
		t.Fatalf("expected 1 added table, got %+v", diffs)
	}
	if diffs[0].Columns[0].Change != ColumnAdded {
		t.Fatalf("expected column marked added, got %+v", diffs[0].Columns)
	}
	if len(diffs[0].PrimaryKey) != 1 || diffs[0].PrimaryKey[0] != "id" {
		t.Fatalf("expected PrimaryKey to carry over from the schema, got %+v", diffs[0].PrimaryKey)
	}
}

func TestDiffRemovedTable(t *testing.T) {
	before := []engine.TableSchema{{Name: "legacy", Columns: []engine.Column{col("id", "INT", false, true)}, PrimaryKey: []string{"id"}}}
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
	after := []engine.TableSchema{{Name: "orders", Columns: []engine.Column{col("id", "INT", false, true)}, PrimaryKey: []string{"id"}}}
	diffs := Diff(nil, after)
	m := GenerateMigration(diffs, "postgres")

	want := "BEGIN;\n\nCREATE TABLE \"orders\" (\n  \"id\" INT NOT NULL,\n  PRIMARY KEY (\"id\")\n);\n\nCOMMIT;\n"
	if m.SQL != want {
		t.Fatalf("unexpected SQL:\ngot:\n%s\nwant:\n%s", m.SQL, want)
	}
	if len(m.Warnings) != 0 {
		t.Fatalf("expected no warnings for a transactional dialect, got %v", m.Warnings)
	}
}

// TestGenerateMigrationQuotesReservedWordIdentifiers guards against #18:
// table/column names come verbatim from live DB introspection with no
// identifier quoting, so an ordinary schema using a reserved word (order,
// group, user, select) as a name produced syntactically invalid DDL, e.g.
// "DROP TABLE order;" — no adversarial input needed. Postgres/SQLite use
// double quotes; MySQL uses backticks.
func TestGenerateMigrationQuotesReservedWordIdentifiers(t *testing.T) {
	before := []engine.TableSchema{{Name: "order", Columns: []engine.Column{col("id", "INT", false, true), col("select", "TEXT", true, false)}}}

	postgres := GenerateMigration(Diff(before, nil), "postgres")
	if !strings.Contains(postgres.SQL, `DROP TABLE "order";`) {
		t.Fatalf("expected quoted DROP TABLE for postgres, got:\n%s", postgres.SQL)
	}

	mysql := GenerateMigration(Diff(before, nil), "mysql")
	if !strings.Contains(mysql.SQL, "DROP TABLE `order`;") {
		t.Fatalf("expected backtick-quoted DROP TABLE for mysql, got:\n%s", mysql.SQL)
	}

	after := []engine.TableSchema{{Name: "group", Columns: []engine.Column{col("select", "TEXT", true, false)}}}
	createPostgres := GenerateMigration(Diff(nil, after), "postgres")
	want := "BEGIN;\n\nCREATE TABLE \"group\" (\n  \"select\" TEXT\n);\n\nCOMMIT;\n"
	if createPostgres.SQL != want {
		t.Fatalf("unexpected SQL:\ngot:\n%s\nwant:\n%s", createPostgres.SQL, want)
	}

	createMySQL := GenerateMigration(Diff(nil, after), "mysql")
	if !strings.Contains(createMySQL.SQL, "CREATE TABLE `group` (\n  `select` TEXT\n") {
		t.Fatalf("expected backtick-quoted CREATE TABLE for mysql, got:\n%s", createMySQL.SQL)
	}
}

// TestGenerateMigrationCompositePrimaryKey guards against #17:
// fmtColumnDef used to append a bare PRIMARY KEY to every column with
// IsPK == true, so a composite-key table generated DDL with PRIMARY KEY
// on two separate columns — invalid in both Postgres ("multiple primary
// keys ... are not allowed") and MySQL ("Multiple primary key defined").
// The primary key must now be a single table-level constraint built from
// TableSchema.PrimaryKey (which preserves declared column order, unlike
// the flat per-column IsPK bool), and no column definition should carry
// an inline PRIMARY KEY at all. Also covers #18: every identifier in the
// generated DDL, including each column named inside PRIMARY KEY (...), is
// quoted for dialect.
func TestGenerateMigrationCompositePrimaryKey(t *testing.T) {
	after := []engine.TableSchema{{
		Name: "order_items",
		Columns: []engine.Column{
			col("order_id", "INT", false, true),
			col("product_id", "INT", false, true),
			col("qty", "INT", false, false),
		},
		PrimaryKey: []string{"order_id", "product_id"},
	}}
	diffs := Diff(nil, after)
	m := GenerateMigration(diffs, "postgres")

	want := "BEGIN;\n\nCREATE TABLE \"order_items\" (\n  \"order_id\" INT NOT NULL,\n  \"product_id\" INT NOT NULL,\n  \"qty\" INT NOT NULL,\n  PRIMARY KEY (\"order_id\", \"product_id\")\n);\n\nCOMMIT;\n"
	if m.SQL != want {
		t.Fatalf("unexpected SQL:\ngot:\n%s\nwant:\n%s", m.SQL, want)
	}
	if strings.Count(m.SQL, "PRIMARY KEY") != 1 {
		t.Fatalf("expected exactly one PRIMARY KEY clause (table-level), got:\n%s", m.SQL)
	}
}

func TestGenerateMigrationMySQLWarnsNonTransactional(t *testing.T) {
	after := []engine.TableSchema{{Name: "orders", Columns: []engine.Column{col("id", "INT", false, true)}, PrimaryKey: []string{"id"}}}
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
	before := []engine.TableSchema{{Name: "users", Columns: []engine.Column{col("id", "INT", false, true), col("old_col", "TEXT", true, false)}, PrimaryKey: []string{"id"}}}
	after := []engine.TableSchema{{Name: "users", Columns: []engine.Column{col("id", "INT", false, true), col("email", "TEXT", false, false)}, PrimaryKey: []string{"id"}}}

	diffs := Diff(before, after)
	m := GenerateMigration(diffs, "postgres")

	if !strings.Contains(m.SQL, `ALTER TABLE "users" ADD COLUMN "email" TEXT NOT NULL;`) {
		t.Fatalf("expected ADD COLUMN statement, got:\n%s", m.SQL)
	}
	if !strings.Contains(m.SQL, `ALTER TABLE "users" DROP COLUMN "old_col";`) {
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
	same := []engine.TableSchema{{Name: "users", Columns: []engine.Column{col("id", "INT", false, true)}, PrimaryKey: []string{"id"}}}
	diffs := Diff(same, same)
	m := GenerateMigration(diffs, "postgres")
	if m.SQL != "-- no schema changes detected\n" {
		t.Fatalf("expected a no-op message, got:\n%s", m.SQL)
	}
}
