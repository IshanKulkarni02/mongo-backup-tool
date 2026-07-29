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
