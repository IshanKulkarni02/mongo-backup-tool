// Package codegen turns a table's introspected schema into API/type
// artifacts — an OpenAPI schema, a TypeScript interface, a Pydantic model —
// aligning dbhelm with a schema-first development workflow. Pure
// functions: no I/O, so every generator is directly golden-file testable.
package codegen

import (
	"sort"
	"strings"
	"unicode"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

// sqlTypeClass buckets a dialect-specific data type into one of a handful
// of general shapes, the common step every generator below builds on.
type sqlTypeClass int

const (
	classString sqlTypeClass = iota
	classInteger
	classNumber
	classBoolean
	classDateTime
	classJSON
	classBinary
)

// integerTypeNames are the base type names (see baseTypeName) classify
// treats as classInteger, matched exactly rather than via a bare
// substring check — "INT" as a substring also matches unrelated type
// names, most notably Postgres' native point type and MySQL/PostGIS
// multipoint (neither of which are integers, their driver value is a
// coordinate-pair string), and even the unrelated INTERVAL type.
var integerTypeNames = map[string]bool{
	"INT": true, "INTEGER": true, "INT2": true, "INT4": true, "INT8": true,
	"TINYINT": true, "SMALLINT": true, "MEDIUMINT": true, "BIGINT": true,
	"SERIAL": true, "SMALLSERIAL": true, "BIGSERIAL": true,
}

// baseTypeName returns t's leading alphabetic run, stripping any
// length/precision suffix ("(11)"), modifier (" UNSIGNED"), or array
// marker ("[]") that might follow — e.g. "INT(11)" and "INT UNSIGNED"
// both become "INT", so integerTypeNames can match on the exact base name
// instead of a substring.
func baseTypeName(t string) string {
	for i, r := range t {
		if !unicode.IsLetter(r) {
			return t[:i]
		}
	}
	return t
}

func classify(dataType string) sqlTypeClass {
	t := strings.ToUpper(dataType)
	switch {
	case strings.Contains(t, "BOOL"):
		return classBoolean
	case strings.Contains(t, "JSON"):
		return classJSON
	case strings.Contains(t, "BYTEA"), strings.Contains(t, "BLOB"), strings.Contains(t, "BINARY"):
		return classBinary
	case strings.Contains(t, "TIMESTAMP"), strings.Contains(t, "DATETIME"), strings.Contains(t, "DATE"), strings.Contains(t, "TIME"):
		return classDateTime
	case integerTypeNames[baseTypeName(t)]:
		return classInteger
	case strings.Contains(t, "FLOAT"), strings.Contains(t, "DOUBLE"), strings.Contains(t, "REAL"),
		strings.Contains(t, "NUMERIC"), strings.Contains(t, "DECIMAL"):
		return classNumber
	default:
		return classString
	}
}

// sortedColumns returns a copy of cols with primary-key columns moved to
// the front; column order is otherwise preserved (a stable sort whose less
// function only distinguishes on IsPK keeps everything else in place).
func sortedColumns(cols []engine.Column) []engine.Column {
	out := make([]engine.Column, len(cols))
	copy(out, cols)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].IsPK && !out[j].IsPK
	})
	return out
}

func fieldComment(c engine.Column) string {
	if c.IsPK {
		return " (primary key)"
	}
	return ""
}
