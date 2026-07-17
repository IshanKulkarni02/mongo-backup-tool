package codegen

import (
	"fmt"
	"strings"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
)

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
		fmt.Fprintf(&sb, "  %s%s: %s;\n", c.Name, optional, typ)
	}
	sb.WriteString("}\n")
	return sb.String()
}
