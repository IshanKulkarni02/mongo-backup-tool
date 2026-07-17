// Package safeguard classifies SQL statements by destructive risk so the
// UI can require an explicit confirmation (or block outright in read-only
// mode) before a DROP, TRUNCATE, or an unqualified DELETE/UPDATE runs
// against a database the user has flagged as sensitive.
package safeguard

import (
	"regexp"
	"strings"
)

// Risk is how dangerous a statement is judged to be.
type Risk string

const (
	// RiskNone is an ordinary read or a qualified, bounded write.
	RiskNone Risk = "none"
	// RiskConfirm is destructive but the user's intent is unambiguous
	// (e.g. DELETE with a WHERE clause) — worth a lightweight confirm.
	RiskConfirm Risk = "confirm"
	// RiskDangerous is a statement that can wipe out unbounded data (DROP,
	// TRUNCATE, ALTER, or DELETE/UPDATE with no WHERE clause) — requires
	// the strong "type the database name" confirmation.
	RiskDangerous Risk = "dangerous"
)

// Classification is the result of inspecting one statement.
type Classification struct {
	Risk   Risk   `json:"risk"`
	Reason string `json:"reason"`
}

var (
	// Matches leading SQL comments (-- line and /* block */) and
	// whitespace, stripped before keyword inspection so a statement can't
	// hide its true verb behind a comment.
	leadingCommentRe = regexp.MustCompile(`(?s)^(\s*(--[^\n]*\n|/\*.*?\*/))*\s*`)
	whereRe          = regexp.MustCompile(`(?i)\bwhere\b`)
	firstWordRe      = regexp.MustCompile(`(?i)^([a-zA-Z]+)`)
)

// Classify inspects a single SQL statement (no trailing semicolon assumed)
// and returns its risk level. It works on statement text alone — it does
// not need a live connection or a parsed AST, so it can run synchronously
// in front of every Execute call.
func Classify(sqlText string) Classification {
	stripped := leadingCommentRe.ReplaceAllString(sqlText, "")
	stripped = strings.TrimSpace(stripped)
	if stripped == "" {
		return Classification{Risk: RiskNone}
	}

	// A leading CTE (WITH ...) doesn't change the risk of whatever
	// statement it ultimately feeds; peel it off by jumping to the first
	// top-level DML/DDL keyword we recognize. This is line/keyword based,
	// not a real SQL parser, so it can be fooled by pathological input —
	// acceptable for a UI confirmation gate, not a security boundary.
	upper := strings.ToUpper(stripped)
	verb := firstWordRe.FindString(upper)

	switch verb {
	case "DROP", "TRUNCATE":
		return Classification{Risk: RiskDangerous, Reason: verb + " removes an entire object; this cannot be undone"}
	case "ALTER":
		return Classification{Risk: RiskDangerous, Reason: "ALTER changes schema and may be irreversible"}
	case "DELETE":
		if !whereRe.MatchString(stripped) {
			return Classification{Risk: RiskDangerous, Reason: "DELETE with no WHERE clause removes every row"}
		}
		return Classification{Risk: RiskConfirm, Reason: "DELETE removes rows"}
	case "UPDATE":
		if !whereRe.MatchString(stripped) {
			return Classification{Risk: RiskDangerous, Reason: "UPDATE with no WHERE clause modifies every row"}
		}
		return Classification{Risk: RiskConfirm, Reason: "UPDATE modifies rows"}
	case "INSERT", "CREATE":
		return Classification{Risk: RiskNone}
	case "WITH":
		// A CTE's risk comes from its final statement; find the last
		// top-level DML keyword in the text as a heuristic.
		return classifyWithCTE(upper)
	default:
		return Classification{Risk: RiskNone}
	}
}

var finalVerbRe = regexp.MustCompile(`(?i)\b(DELETE|UPDATE|INSERT|DROP|TRUNCATE|ALTER)\b`)

func classifyWithCTE(upper string) Classification {
	matches := finalVerbRe.FindAllString(upper, -1)
	if len(matches) == 0 {
		return Classification{Risk: RiskNone}
	}
	last := strings.ToUpper(matches[len(matches)-1])
	switch last {
	case "DROP", "TRUNCATE", "ALTER":
		return Classification{Risk: RiskDangerous, Reason: last + " inside a WITH statement"}
	case "DELETE", "UPDATE":
		if !whereRe.MatchString(upper) {
			return Classification{Risk: RiskDangerous, Reason: last + " with no WHERE clause inside a WITH statement"}
		}
		return Classification{Risk: RiskConfirm, Reason: last + " inside a WITH statement"}
	default:
		return Classification{Risk: RiskNone}
	}
}
