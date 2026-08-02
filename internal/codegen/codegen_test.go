package codegen

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

func sampleSchema() engine.TableSchema {
	return engine.TableSchema{
		Name: "users",
		Columns: []engine.Column{
			{Name: "id", DataType: "INTEGER", Nullable: false, IsPK: true},
			{Name: "email", DataType: "VARCHAR", Nullable: false},
			{Name: "bio", DataType: "TEXT", Nullable: true},
			{Name: "signup_count", DataType: "NUMERIC", Nullable: true},
			{Name: "is_active", DataType: "BOOLEAN", Nullable: false},
			{Name: "created_at", DataType: "TIMESTAMP", Nullable: false},
			{Name: "metadata", DataType: "JSONB", Nullable: true},
		},
	}
}

func TestGenerateOpenAPI(t *testing.T) {
	got := GenerateOpenAPI(sampleSchema(), "")
	want := `components:
  schemas:
    "Users":
      type: object
      properties:
        "id":
          type: integer
          description: "(primary key)"
        "email":
          type: string
        "bio":
          type: string
        "signup_count":
          type: number
        "is_active":
          type: boolean
        "created_at":
          type: string
          format: date-time
        "metadata":
          type: object
      required:
        - "id"
        - "email"
        - "is_active"
        - "created_at"
`
	if got != want {
		t.Fatalf("unexpected OpenAPI output:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestGenerateOpenAPICustomName(t *testing.T) {
	got := GenerateOpenAPI(sampleSchema(), "UserAccount")
	if !strings.Contains(got, `"UserAccount":`) {
		t.Fatalf("expected custom schema name to be used, got:\n%s", got)
	}
}

// TestGenerateOpenAPIEscapesInjectionAttempt is the regression test for
// #75: a column name containing YAML-structural characters (a colon, a
// newline that could inject a bogus sibling key) must not be able to
// alter the generated document's structure. Verified two ways: the
// output actually parses as YAML (an unescaped injection would either
// fail to parse or parse into a structure with extra/altered keys), and
// the parsed structure has exactly the fields this table should produce
// — no injected "admin" key, no altered nesting.
func TestGenerateOpenAPIEscapesInjectionAttempt(t *testing.T) {
	schema := engine.TableSchema{
		Name: "t",
		Columns: []engine.Column{
			{Name: "foo\n    admin: true", DataType: "TEXT", Nullable: true},
		},
	}
	got := GenerateOpenAPI(schema, "")

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("generated OpenAPI YAML failed to parse: %v\noutput:\n%s", err, got)
	}

	components, _ := doc["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	tSchema, _ := schemas["T"].(map[string]any)
	properties, _ := tSchema["properties"].(map[string]any)

	if len(properties) != 1 {
		t.Fatalf("expected exactly 1 property (the malicious column name treated as one literal string key), got %d: %+v", len(properties), properties)
	}
	if _, injected := properties["admin"]; injected {
		t.Fatalf("column name injected a sibling \"admin\" key into properties: %+v", properties)
	}
	for k := range properties {
		if !strings.Contains(k, "admin: true") {
			t.Fatalf("expected the property key to contain the full literal column name, got %q", k)
		}
	}
}

func TestGenerateTypeScript(t *testing.T) {
	got := GenerateTypeScript(sampleSchema(), "")
	want := `export interface Users {
  /** (primary key) */
  id: number;
  email: string;
  bio?: string | null;
  signup_count?: number | null;
  is_active: boolean;
  created_at: string;
  metadata?: Record<string, unknown> | null;
}
`
	if got != want {
		t.Fatalf("unexpected TypeScript output:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestGeneratePydantic(t *testing.T) {
	got := GeneratePydantic(sampleSchema(), "")
	want := `import datetime
from typing import Optional

from pydantic import BaseModel


class Users(BaseModel):
    id: int # (primary key)
    email: str
    bio: Optional[str] = None
    signup_count: Optional[float] = None
    is_active: bool
    created_at: datetime.datetime
    metadata: Optional[dict] = None
`
	if got != want {
		t.Fatalf("unexpected Pydantic output:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestGeneratePydanticSkipsDatetimeImportWhenUnneeded(t *testing.T) {
	schema := engine.TableSchema{Name: "flags", Columns: []engine.Column{{Name: "enabled", DataType: "BOOLEAN", Nullable: false}}}
	got := GeneratePydantic(schema, "")
	if strings.Contains(got, "import datetime") {
		t.Fatalf("expected no datetime import when no column needs it, got:\n%s", got)
	}
}

func TestGenerateEmptyTable(t *testing.T) {
	schema := engine.TableSchema{Name: "empty_table"}
	if !strings.Contains(GenerateTypeScript(schema, ""), "export interface EmptyTable {\n}\n") {
		t.Fatalf("expected an empty interface body, got:\n%s", GenerateTypeScript(schema, ""))
	}
	if !strings.Contains(GeneratePydantic(schema, ""), "pass") {
		t.Fatalf("expected a pass-body class for a table with no columns, got:\n%s", GeneratePydantic(schema, ""))
	}
}

func TestPascalCase(t *testing.T) {
	cases := map[string]string{
		"users":            "Users",
		"user_accounts":    "UserAccounts",
		"order-items":      "OrderItems",
		"already_snake_ok": "AlreadySnakeOk",
	}
	for in, want := range cases {
		if got := pascalCase(in); got != want {
			t.Errorf("pascalCase(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPascalCaseHandlesArbitraryInvalidIdentifierCharacters is the
// regression test for #76's second half: pascalCase previously only
// stripped "_"/"-", so a table name like "2fa_codes" or "my table" (both
// legal quoted SQL identifiers) produced an invalid TS/Python identifier
// — a leading digit or an embedded space — that fails to compile/parse
// in the generated file.
func TestPascalCaseHandlesArbitraryInvalidIdentifierCharacters(t *testing.T) {
	cases := map[string]string{
		"2fa_codes": "T2faCodes",
		"my table":  "MyTable",
		"a.b.c":     "ABC",
		"___":       "T",
	}
	for in, want := range cases {
		got := pascalCase(in)
		if got != want {
			t.Errorf("pascalCase(%q) = %q, want %q", in, got, want)
		}
		if len(got) == 0 || (got[0] >= '0' && got[0] <= '9') {
			t.Errorf("pascalCase(%q) = %q starts with a digit — not a valid identifier", in, got)
		}
	}
}

// TestGenerateTypeScriptQuotesInvalidPropertyNames is the regression
// test for #76: a column name containing a space is a legal quoted SQL
// identifier but produced a bare (unquoted) TypeScript property name —
// e.g. "  first name: string;" — which is a syntax error. It must now be
// rendered as a quoted string-literal key, which TypeScript interfaces
// accept for any property name.
func TestGenerateTypeScriptQuotesInvalidPropertyNames(t *testing.T) {
	schema := engine.TableSchema{
		Name: "t",
		Columns: []engine.Column{
			{Name: "first name", DataType: "TEXT", Nullable: false},
		},
	}
	got := GenerateTypeScript(schema, "")
	if strings.Contains(got, "\n  first name:") {
		t.Fatalf("expected the invalid bare property name to be gone, got:\n%s", got)
	}
	want := "export interface T {\n  \"first name\": string;\n}\n"
	if got != want {
		t.Fatalf("unexpected TypeScript output:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestGenerateTypeScriptEscapesEmbeddedNewline confirms a column name
// containing a literal newline (also a legal quoted SQL identifier)
// doesn't inject an extra line into the generated file — it must appear
// as an escaped \n inside a single quoted property key, not a real
// line break.
func TestGenerateTypeScriptEscapesEmbeddedNewline(t *testing.T) {
	schema := engine.TableSchema{
		Name: "t",
		Columns: []engine.Column{
			{Name: "bad\nname", DataType: "TEXT", Nullable: false},
		},
	}
	got := GenerateTypeScript(schema, "")
	// export interface T {\n + the one field line + }\n = 3 lines (plus a
	// trailing empty element from the final \n when split).
	lines := strings.Split(got, "\n")
	if len(lines) != 4 || lines[3] != "" {
		t.Fatalf("expected exactly 3 lines of output (no injected extra line from the embedded newline), got %d lines:\n%q", len(lines)-1, got)
	}
	if !strings.Contains(got, `"bad\nname"`) {
		t.Fatalf("expected the embedded newline to be escaped as \\n inside the quoted key, got:\n%s", got)
	}
}

// TestGeneratePydanticAliasesInvalidFieldNames is the regression test
// for #76: a column name with a space, or one that collides with a
// Python reserved keyword (both legal quoted SQL identifiers), produced
// an invalid Python attribute name — a syntax error in the generated
// class body. It must now be sanitized into a valid identifier and
// aliased back to the original name via pydantic's Field(alias=...).
func TestGeneratePydanticAliasesInvalidFieldNames(t *testing.T) {
	schema := engine.TableSchema{
		Name: "t",
		Columns: []engine.Column{
			{Name: "first name", DataType: "TEXT", Nullable: false},
			{Name: "class", DataType: "TEXT", Nullable: true},
		},
	}
	got := GeneratePydantic(schema, "")
	if !strings.Contains(got, "from pydantic import BaseModel, Field") {
		t.Fatalf("expected the Field import to be added when aliasing is needed, got:\n%s", got)
	}
	if !strings.Contains(got, `first_name: str = Field(alias="first name")`) {
		t.Fatalf("expected a sanitized, aliased field for \"first name\", got:\n%s", got)
	}
	if !strings.Contains(got, `class_: Optional[str] = Field(default=None, alias="class")`) {
		t.Fatalf("expected the reserved keyword \"class\" to be sanitized and aliased, got:\n%s", got)
	}
}

// TestGeneratePydanticDisambiguatesCollidingNames is the regression test
// for #76's residual gap: pythonIdentifier is many-to-one, so two
// distinct SQL columns sanitizing to the same Python name ("first-name"
// and "first_name" both become "first_name") must not silently shadow
// each other in the generated class body — the second field would
// otherwise vanish from the model with no error.
func TestGeneratePydanticDisambiguatesCollidingNames(t *testing.T) {
	schema := engine.TableSchema{
		Name: "t",
		Columns: []engine.Column{
			{Name: "first-name", DataType: "TEXT", Nullable: false},
			{Name: "first_name", DataType: "TEXT", Nullable: false},
		},
	}
	got := GeneratePydantic(schema, "")
	if !strings.Contains(got, `first_name: str = Field(alias="first-name")`) {
		t.Fatalf("expected \"first-name\" sanitized to first_name, got:\n%s", got)
	}
	if !strings.Contains(got, `first_name_2: str = Field(alias="first_name")`) {
		t.Fatalf("expected the colliding \"first_name\" column disambiguated to first_name_2, got:\n%s", got)
	}
	if strings.Count(got, "first_name:") != 1 {
		t.Fatalf("expected exactly one field literally named first_name (not shadowed), got:\n%s", got)
	}
}

// TestPythonIdentifierSanitizesInvalidNames covers pythonIdentifier
// directly: invalid characters, a leading digit, and reserved keywords
// all need distinct handling to produce a valid Python identifier.
func TestPythonIdentifierSanitizesInvalidNames(t *testing.T) {
	cases := map[string]string{
		"email":      "email", // already valid, unchanged
		"first name": "first_name",
		"2fa_code":   "_2fa_code",
		"class":      "class_",
		"a-b.c":      "a_b_c",
	}
	for in, want := range cases {
		if got := pythonIdentifier(in); got != want {
			t.Errorf("pythonIdentifier(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestClassifyGeometricTypesAreNotInteger covers issue #28: Postgres' point
// type and MySQL/PostGIS multipoint contain "INT" as a bare substring
// (poINT, multipoINT) and must not be classified as classInteger.
func TestClassifyGeometricTypesAreNotInteger(t *testing.T) {
	cases := []string{"point", "POINT", "multipoint", "MultiPoint", "linestring", "polygon"}
	for _, dt := range cases {
		if got := classify(dt); got == classInteger {
			t.Errorf("classify(%q) = classInteger, want anything else", dt)
		}
	}
}

// TestClassifyIntegerTypesStillMatch guards against the fix above
// overcorrecting: real integer types, including ones with a length
// modifier or UNSIGNED suffix, must still classify as classInteger.
func TestClassifyIntegerTypesStillMatch(t *testing.T) {
	cases := []string{
		"INT", "int", "INTEGER", "INT2", "INT4", "INT8",
		"TINYINT", "SMALLINT", "MEDIUMINT", "BIGINT",
		"SERIAL", "SMALLSERIAL", "BIGSERIAL",
		"INT(11)", "INT UNSIGNED", "INT(11) UNSIGNED", "BIGINT(20)",
	}
	for _, dt := range cases {
		if got := classify(dt); got != classInteger {
			t.Errorf("classify(%q) = %v, want classInteger", dt, got)
		}
	}
}

// TestClassifyIntervalIsNotInteger covers the same substring hazard for
// Postgres' INTERVAL type, which also contains "INT".
func TestClassifyIntervalIsNotInteger(t *testing.T) {
	if got := classify("INTERVAL"); got == classInteger {
		t.Errorf("classify(%q) = classInteger, want anything else", "INTERVAL")
	}
}

func TestBaseTypeName(t *testing.T) {
	cases := map[string]string{
		"INT":          "INT",
		"INT(11)":      "INT",
		"INT UNSIGNED": "INT",
		"VARCHAR(255)": "VARCHAR",
		"POINT":        "POINT",
		"":             "",
	}
	for in, want := range cases {
		if got := baseTypeName(in); got != want {
			t.Errorf("baseTypeName(%q) = %q, want %q", in, got, want)
		}
	}
}
