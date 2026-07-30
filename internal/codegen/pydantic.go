package codegen

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

// invalidPyChar matches any rune that can't appear in a Python
// identifier.
var invalidPyChar = regexp.MustCompile(`[^A-Za-z0-9_]`)

// pyKeywords are Python's reserved words — none are valid as a bare
// identifier even though they're perfectly legal column names in SQL.
var pyKeywords = map[string]bool{
	"False": true, "None": true, "True": true, "and": true, "as": true, "assert": true,
	"async": true, "await": true, "break": true, "class": true, "continue": true, "def": true,
	"del": true, "elif": true, "else": true, "except": true, "finally": true, "for": true,
	"from": true, "global": true, "if": true, "import": true, "in": true, "is": true,
	"lambda": true, "nonlocal": true, "not": true, "or": true, "pass": true, "raise": true,
	"return": true, "try": true, "while": true, "with": true, "yield": true,
}

// pythonIdentifier sanitizes a column name into a valid Python
// identifier: every character outside [A-Za-z0-9_] becomes "_", a
// leading digit gets a "_" prefix, and a name that collides with a
// reserved keyword gets a trailing "_". A column name is a legal quoted
// SQL identifier that can contain spaces, hyphens, a leading digit, or
// be a bare keyword like "class" — none of which are valid as a Python
// attribute name and would otherwise produce a class body with a syntax
// error.
func pythonIdentifier(name string) string {
	out := invalidPyChar.ReplaceAllString(name, "_")
	if out == "" {
		out = "_"
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "_" + out
	}
	if pyKeywords[out] {
		out += "_"
	}
	return out
}

// pyStringLiteral renders s as a double-quoted Python string literal.
// Python's double-quoted string escaping is a superset of JSON's, so
// json.Marshal — already used the same way for YAML/TS string literals
// elsewhere in this package — produces valid, safe Python source here too.
func pyStringLiteral(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

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
	needsFieldImport := false
	pyNames := make([]string, len(cols))
	for i, c := range cols {
		if classify(c.DataType) == classDateTime {
			needsDatetime = true
		}
		pyNames[i] = pythonIdentifier(c.Name)
		if pyNames[i] != c.Name {
			needsFieldImport = true
		}
	}

	var sb strings.Builder
	if needsDatetime {
		sb.WriteString("import datetime\n")
	}
	sb.WriteString("from typing import Optional\n\n")
	if needsFieldImport {
		sb.WriteString("from pydantic import BaseModel, Field\n\n\n")
	} else {
		sb.WriteString("from pydantic import BaseModel\n\n\n")
	}
	fmt.Fprintf(&sb, "class %s(BaseModel):\n", className)
	if len(cols) == 0 {
		sb.WriteString("    pass\n")
		return sb.String()
	}
	for i, c := range cols {
		typ := pydanticType(classify(c.DataType))
		if c.Nullable {
			typ = "Optional[" + typ + "]"
		}
		comment := fieldComment(c)
		pyName := pyNames[i]
		var line string
		if pyName != c.Name {
			// The column name isn't a valid Python identifier (a space,
			// hyphen, leading digit, or a bare reserved keyword like
			// "class" — all legal quoted SQL identifiers): keep the
			// sanitized name as the Python attribute but preserve the
			// original via Field(alias=...), the standard Pydantic
			// pattern for a field whose wire/DB name differs from its
			// Python name.
			alias := pyStringLiteral(c.Name)
			if c.Nullable {
				line = fmt.Sprintf("    %s: %s = Field(default=None, alias=%s)", pyName, typ, alias)
			} else {
				line = fmt.Sprintf("    %s: %s = Field(alias=%s)", pyName, typ, alias)
			}
		} else {
			line = fmt.Sprintf("    %s: %s", pyName, typ)
			if c.Nullable {
				line += " = None"
			}
		}
		if comment != "" {
			line += " #" + comment
		}
		sb.WriteString(line + "\n")
	}
	return sb.String()
}
