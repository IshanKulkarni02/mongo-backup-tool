package codegen

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

func openAPIType(class sqlTypeClass) (typ, format string) {
	switch class {
	case classInteger:
		return "integer", ""
	case classNumber:
		return "number", ""
	case classBoolean:
		return "boolean", ""
	case classDateTime:
		return "string", "date-time"
	case classBinary:
		return "string", "byte"
	case classJSON:
		return "object", ""
	default:
		return "string", ""
	}
}

// yamlString renders s as a properly escaped, always-safe YAML scalar —
// usable as either a mapping key or a value — by JSON-encoding it. JSON
// is a strict subset of YAML 1.2, so this handles every YAML-special
// character (colons, newlines, leading "- "/"*"/"&", embedded quotes,
// etc.) correctly without needing a hand-rolled YAML quoting
// implementation. json.Marshal on a string never errors.
func yamlString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// GenerateOpenAPI renders a table as an OpenAPI 3.0 component schema
// (YAML). schemaName defaults to the table name, capitalized, if empty.
// Every table/column name is YAML-quoted (see yamlString) rather than
// interpolated raw: SQL identifiers can legally contain characters (a
// bare colon, a newline, a leading "- ") that would otherwise let a
// crafted table/column name alter the structure of the generated
// document, not just look ugly in it.
func GenerateOpenAPI(schema engine.TableSchema, schemaName string) string {
	if schemaName == "" {
		schemaName = pascalCase(schema.Name)
	}
	cols := sortedColumns(schema.Columns)

	var sb strings.Builder
	fmt.Fprintf(&sb, "components:\n  schemas:\n    %s:\n      type: object\n      properties:\n", yamlString(schemaName))
	var required []string
	for _, c := range cols {
		typ, format := openAPIType(classify(c.DataType))
		fmt.Fprintf(&sb, "        %s:\n          type: %s\n", yamlString(c.Name), typ)
		if format != "" {
			fmt.Fprintf(&sb, "          format: %s\n", format)
		}
		if desc := fieldComment(c); desc != "" {
			fmt.Fprintf(&sb, "          description: %s\n", yamlString(strings.TrimSpace(desc)))
		}
		if !c.Nullable {
			required = append(required, c.Name)
		}
	}
	if len(required) > 0 {
		sb.WriteString("      required:\n")
		for _, r := range required {
			fmt.Fprintf(&sb, "        - %s\n", yamlString(r))
		}
	}
	return sb.String()
}

func pascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' })
	var sb strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		sb.WriteString(strings.ToUpper(p[:1]))
		sb.WriteString(p[1:])
	}
	if sb.Len() == 0 {
		return s
	}
	return sb.String()
}
