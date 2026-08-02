package codegen

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

// validTSIdentifier matches a bare identifier that's safe to use
// unquoted as a TypeScript interface property name.
var validTSIdentifier = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// tsPropertyName renders name as a TypeScript interface property key: a
// bare identifier when name is one, otherwise a quoted string literal
// (interfaces accept "any string" as a key, e.g. `"first name": string;`).
// A column name is a legal quoted SQL identifier that can contain
// characters — spaces, hyphens, a leading digit, even an embedded
// newline — that aren't valid in a bare TypeScript identifier and would
// otherwise produce a file that fails to parse. json.Marshal produces a
// valid double-quoted TS/JS string literal for any Go string (JS string
// literal escaping is a superset of JSON's), including turning a literal
// newline into `\n` rather than emitting one into the source.
func tsPropertyName(name string) string {
	if validTSIdentifier.MatchString(name) {
		return name
	}
	b, _ := json.Marshal(name)
	return string(b)
}

func tsType(class sqlTypeClass) string {
	switch class {
	case classInteger, classNumber:
		return "number"
	case classBoolean:
		return "boolean"
	case classDateTime:
		return "string" // ISO 8601 — callers parse with `new Date(...)` as needed
	case classJSON:
		return "Record<string, unknown>"
	case classBinary:
		return "string" // base64
	default:
		return "string"
	}
}

// GenerateTypeScript renders a table as a TypeScript interface.
// interfaceName defaults to the table name in PascalCase if empty.
func GenerateTypeScript(schema engine.TableSchema, interfaceName string) string {
	if interfaceName == "" {
		interfaceName = pascalCase(schema.Name)
	}
	cols := sortedColumns(schema.Columns)

	var sb strings.Builder
	fmt.Fprintf(&sb, "export interface %s {\n", interfaceName)
	for _, c := range cols {
		optional := ""
		if c.Nullable {
			optional = "?"
		}
		typ := tsType(classify(c.DataType))
		if c.Nullable {
			typ += " | null"
		}
		comment := fieldComment(c)
		if comment != "" {
			fmt.Fprintf(&sb, "  /**%s */\n", comment)
		}
		fmt.Fprintf(&sb, "  %s%s: %s;\n", tsPropertyName(c.Name), optional, typ)
	}
	sb.WriteString("}\n")
	return sb.String()
}
