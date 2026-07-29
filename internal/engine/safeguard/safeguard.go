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

// riskRank orders Risk from least to most severe, so scanning multiple
// statements can keep "the worst one seen so far."
func riskRank(r Risk) int {
	switch r {
	case RiskDangerous:
		return 2
	case RiskConfirm:
		return 1
	default:
		return 0
	}
}

// Classify inspects sqlText — which may contain more than one
// semicolon-separated statement — and returns the risk of the single most
// dangerous statement found in it. Statement text alone is used — no live
// connection or parsed AST needed — so this can run synchronously in
// front of every Execute call.
func Classify(sqlText string) Classification {
	stmts := splitStatements(sqlText)
	if len(stmts) <= 1 {
		return classifyOne(sqlText)
	}
	worst := Classification{Risk: RiskNone}
	for _, s := range stmts {
		c := classifyOne(s)
		if riskRank(c.Risk) > riskRank(worst.Risk) {
			worst = c
		}
	}
	return worst
}

// classifyOne is Classify's original single-statement logic (no
// semicolon-splitting): line/keyword based, not a real SQL parser, so it
// can be fooled by pathological input within one statement — acceptable
// for a UI confirmation gate, not a security boundary. Classify's job is
// making sure every statement in a multi-statement string actually reaches
// this function once.
func classifyOne(sqlText string) Classification {
	stripped := leadingCommentRe.ReplaceAllString(sqlText, "")
	stripped = strings.TrimSpace(stripped)
	if stripped == "" {
		return Classification{Risk: RiskNone}
	}

	// A leading CTE (WITH ...) doesn't change the risk of whatever
	// statement it ultimately feeds; peel it off by jumping to the first
	// top-level DML/DDL keyword we recognize.
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

// splitStatements splits sqlText on top-level semicolons — skipping ones
// inside single/double-quoted strings or `--`/`/* */` comments — so
// "SELECT 1; DROP TABLE users" splits into two statements instead of
// being classified only by its leading SELECT, and a semicolon inside a
// string literal or comment never causes a false split. This is a
// character-based scanner, not a real SQL parser (see classifyOne's doc
// comment on the same limitation); it only needs to be good enough to
// find statement boundaries, not to fully validate syntax.
func splitStatements(sqlText string) []string {
	var stmts []string
	var cur strings.Builder
	runes := []rune(sqlText)
	n := len(runes)
	i := 0
	for i < n {
		c := runes[i]
		switch {
		case c == '\'' || c == '"':
			quote := c
			cur.WriteRune(c)
			i++
			for i < n {
				cur.WriteRune(runes[i])
				if runes[i] == quote {
					if i+1 < n && runes[i+1] == quote {
						i++
						cur.WriteRune(runes[i])
						i++
						continue
					}
					i++
					break
				}
				i++
			}
		case c == '-' && i+1 < n && runes[i+1] == '-':
			for i < n && runes[i] != '\n' {
				cur.WriteRune(runes[i])
				i++
			}
		case c == '/' && i+1 < n && runes[i+1] == '*':
			cur.WriteRune(runes[i])
			cur.WriteRune(runes[i+1])
			i += 2
			for i+1 < n && !(runes[i] == '*' && runes[i+1] == '/') {
				cur.WriteRune(runes[i])
				i++
			}
			if i+1 < n {
				cur.WriteRune(runes[i])
				cur.WriteRune(runes[i+1])
				i += 2
			} else {
				for i < n {
					cur.WriteRune(runes[i])
					i++
				}
			}
		case c == ';':
			stmts = append(stmts, cur.String())
			cur.Reset()
			i++
		default:
			cur.WriteRune(c)
			i++
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		stmts = append(stmts, cur.String())
	}
	return stmts
}

// readVerbs are statement verbs that never modify data, so callers that
// only ever intend to read (RunSQLQuery's bounded table-browser path,
// RunSavedQuery) can skip the requireWritable/Classify gating entirely —
// mirroring the frontend's looksLikeRead check for the ad-hoc "Run" button.
var readVerbs = map[string]bool{
	"SELECT": true, "SHOW": true, "PRAGMA": true, "EXPLAIN": true,
}

var writeVerbRe = regexp.MustCompile(`(?i)\b(DELETE|UPDATE|INSERT|DROP|TRUNCATE|ALTER|CREATE)\b`)

// IsRead reports whether every statement in sqlText (which may contain
// more than one semicolon-separated statement) is read-only: SELECT,
// SHOW, PRAGMA, EXPLAIN, or a WITH/CTE whose text contains no write verb
// anywhere. A single non-read statement anywhere in the input makes the
// whole thing non-read — the same reasoning as Classify: a false negative
// here just routes an ordinary read through the write-gated path, whereas
// a false positive would let a destructive statement riding alongside an
// innocuous leading SELECT bypass requireWritable and the
// dangerous-statement confirmation entirely.
func IsRead(sqlText string) bool {
	stmts := splitStatements(sqlText)
	if len(stmts) <= 1 {
		return isReadOne(sqlText)
	}
	for _, s := range stmts {
		if !isReadOne(s) {
			return false
		}
	}
	return true
}

func isReadOne(sqlText string) bool {
	stripped := leadingCommentRe.ReplaceAllString(sqlText, "")
	stripped = strings.TrimSpace(stripped)
	upper := strings.ToUpper(stripped)
	verb := firstWordRe.FindString(upper)
	if readVerbs[verb] {
		return true
	}
	if verb == "WITH" {
		return !writeVerbRe.MatchString(upper)
	}
	return false
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
