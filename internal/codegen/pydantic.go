package codegen

import (
	"fmt"
	"strings"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
)

func pydanticType(class sqlTypeClass) string {
	switch class {
	case classInteger:
		return "int"
	case classNumber:
		return "float"
	case classBoolean:
		return "bool"
	case classDateTime:
		return "datetime.datetime"
	case classJSON:
		return "dict"
	case classBinary:
		return "bytes"
	default:
		return "str"
	}
}

// GeneratePydantic renders a table as a Pydantic BaseModel. className
// defaults to the table name in PascalCase if empty.
func GeneratePydantic(schema engine.TableSchema, className string) string {
	if className == "" {
		className = pascalCase(schema.Name)
	}
	cols := sortedColumns(schema.Columns)

	needsDatetime := false
	for _, c := range cols {
		if classify(c.DataType) == classDateTime {
			needsDatetime = true
		}
	}

	var sb strings.Builder
	if needsDatetime {
		sb.WriteString("import datetime\n")
	}
	sb.WriteString("from typing import Optional\n\nfrom pydantic import BaseModel\n\n\n")
	fmt.Fprintf(&sb, "class %s(BaseModel):\n", className)
	if len(cols) == 0 {
		sb.WriteString("    pass\n")
		return sb.String()
	}
	for _, c := range cols {
		typ := pydanticType(classify(c.DataType))
		if c.Nullable {
			typ = "Optional[" + typ + "]"
		}
		comment := fieldComment(c)
		line := fmt.Sprintf("    %s: %s", c.Name, typ)
		if c.Nullable {
			line += " = None"
		}
		if comment != "" {
			line += " #" + comment
		}
		sb.WriteString(line + "\n")
	}
	return sb.String()
}
