package safeguard

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want Risk
	}{
		{"plain select", "SELECT * FROM users", RiskNone},
		{"insert", "INSERT INTO users (email) VALUES ('a@b.com')", RiskNone},
		{"create table", "CREATE TABLE t (id INT)", RiskNone},

		{"delete with where", "DELETE FROM users WHERE id = 5", RiskConfirm},
		{"update with where", "UPDATE users SET active = false WHERE id = 5", RiskConfirm},

		{"delete no where", "DELETE FROM users", RiskDangerous},
		{"update no where", "UPDATE users SET active = false", RiskDangerous},
		{"drop table", "DROP TABLE users", RiskDangerous},
		{"truncate", "TRUNCATE TABLE users", RiskDangerous},
		{"alter table", "ALTER TABLE users DROP COLUMN email", RiskDangerous},

		// A quoted "where"/verb-shaped word is data, not a real clause or
		// statement — an unbounded DELETE/UPDATE must still classify as
		// dangerous even if a string literal happens to contain the word.
		{"delete no where, word in literal", "DELETE FROM logs RETURNING 'no where clause here'", RiskDangerous},
		{"update no where, word in literal", "UPDATE users SET note = 'the where clause'", RiskDangerous},

		// "WHERE true" is a real (if vacuous) WHERE clause — the
		// classifier can't judge truthiness, so this documents a known,
		// separate limitation rather than the quoted-literal bypass above.
		{"delete where true", "DELETE FROM users WHERE true", RiskConfirm},

		// Leading comments must not hide the real verb.
		{"line comment before drop", "-- cleanup\nDROP TABLE users", RiskDangerous},
		{"block comment before delete", "/* wipe */ DELETE FROM users", RiskDangerous},
		{"comment then safe select", "-- just checking\nSELECT * FROM users", RiskNone},

		// Case insensitivity.
		{"lowercase drop", "drop table users", RiskDangerous},
		{"mixed case delete no where", "DeLeTe FROM users", RiskDangerous},

		// Whitespace-only / empty input shouldn't crash or be misjudged.
		{"empty", "", RiskNone},
		{"whitespace only", "   \n\t  ", RiskNone},

		// CTEs: risk follows the final statement, not the WITH keyword.
		{"cte then delete no where", "WITH x AS (SELECT 1) DELETE FROM users", RiskDangerous},
		{"cte then delete with where", "WITH x AS (SELECT 1) DELETE FROM users WHERE id = 1", RiskConfirm},
		{"cte then select", "WITH x AS (SELECT 1) SELECT * FROM x", RiskNone},

		// CTEs: a quoted verb-shaped word after the real statement must not
		// shift which verb — or WHERE-clause presence — gets evaluated.
		{"cte then delete no where, verb word in literal", "WITH x AS (SELECT 1) DELETE FROM logs RETURNING 'insert a note'", RiskDangerous},
		{"cte then update no where, where word in literal", "WITH x AS (SELECT 1) UPDATE users SET note = 'the where clause'", RiskDangerous},

		// #25's exact reported bypass: a real DELETE inside the CTE with
		// no WHERE clause of its own must not be masked by 'INSERT'
		// appearing only as a quoted value in the *outer* SELECT's WHERE
		// clause — before the fix this returned RiskNone (INSERT was
		// picked as "the" verb) with no confirmation at all. It's
		// RiskConfirm rather than RiskDangerous here because that outer
		// WHERE is still (mis)read as belonging to the DELETE — a
		// separate, pre-existing, documented limitation (the classifier
		// has no notion of which sub-clause a WHERE belongs to) — but the
		// silent full bypass is closed.
		{"cte delete masked by insert literal in outer where", "WITH x AS (DELETE FROM comments) SELECT * FROM x WHERE name = 'INSERT'", RiskConfirm},

		// #25: a statement that doesn't start with a recognizable keyword
		// at all (firstWordRe can't match past a leading non-letter) must
		// not silently fall through to RiskNone — we can't tell what's
		// about to run, so it's treated as dangerous.
		{"leading paren before delete", "(DELETE FROM users)", RiskDangerous},

		// Multiple statements (#82): a dangerous statement anywhere in a
		// semicolon-separated string must be caught, not just the first
		// statement's verb.
		{"select then drop", "SELECT 1; DROP TABLE users", RiskDangerous},
		{"select then delete no where", "SELECT 1; DELETE FROM users", RiskDangerous},
		{"select then delete with where, worst wins", "SELECT 1; DELETE FROM users WHERE id = 1; DROP TABLE users", RiskDangerous},
		{"select then confirm-risk delete", "SELECT 1; DELETE FROM users WHERE id = 1", RiskConfirm},
		{"all safe statements", "SELECT 1; INSERT INTO t VALUES (1)", RiskNone},
		// A semicolon inside a string literal must not be mistaken for a
		// statement boundary — this is one statement, not two, and its
		// (only) verb is SELECT.
		{"semicolon inside string literal", "SELECT 'a;b' AS val", RiskNone},
		// ...but a real second statement after a string containing a
		// semicolon must still be found.
		{"string with semicolon then real drop", "SELECT 'a;b'; DROP TABLE users", RiskDangerous},
		// A semicolon inside a line comment must not split either.
		{"semicolon inside line comment", "SELECT 1 -- notes: a;b\n", RiskNone},
		// Trailing semicolon/whitespace shouldn't produce a bogus empty
		// second statement that changes the result.
		{"trailing semicolon", "DROP TABLE users;", RiskDangerous},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.sql)
			if got.Risk != c.want {
				t.Errorf("Classify(%q) = %q, want %q (reason: %q)", c.sql, got.Risk, c.want, got.Reason)
			}
		})
	}
}

func TestIsRead(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want bool
	}{
		{"select", "SELECT * FROM users", true},
		{"show", "SHOW TABLES", true},
		{"pragma", "PRAGMA table_info(users)", true},
		{"explain", "EXPLAIN SELECT 1", true},
		{"cte pure read", "WITH x AS (SELECT 1) SELECT * FROM x", true},

		{"insert", "INSERT INTO users (email) VALUES ('a@b.com')", false},
		{"create", "CREATE TABLE t (id INT)", false},
		{"delete", "DELETE FROM users WHERE id = 1", false},
		{"drop", "DROP TABLE users", false},
		{"cte with delete", "WITH x AS (SELECT 1) DELETE FROM users", false},

		{"empty", "", false},

		// Multiple statements (#82): a write hiding behind a leading read
		// must not be classified as read-only.
		{"select then drop", "SELECT 1; DROP TABLE users", false},
		{"two reads", "SELECT 1; SHOW TABLES", true},
		{"semicolon inside string stays one read statement", "SELECT 'a;b' AS val", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsRead(c.sql); got != c.want {
				t.Errorf("IsRead(%q) = %v, want %v", c.sql, got, c.want)
			}
		})
	}
}

func TestStripExplainAnalyze(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want string
	}{
		{"analyze select", "ANALYZE SELECT * FROM users", "SELECT * FROM users"},
		{"lowercase analyze", "analyze select 1", "select 1"},
		{"no analyze", "SELECT * FROM users", "SELECT * FROM users"},
		{"analyze delete", "ANALYZE DELETE FROM users", "DELETE FROM users"},
		{"leading whitespace before analyze", "  ANALYZE SELECT 1", "SELECT 1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StripExplainAnalyze(c.sql); got != c.want {
				t.Errorf("StripExplainAnalyze(%q) = %q, want %q", c.sql, got, c.want)
			}
		})
	}
}

// TestIsReadAfterStripExplainAnalyze guards against #9: an ExplainSQL call
// gates on IsRead(StripExplainAnalyze(sqlText)), so "ANALYZE SELECT ..."
// must still read as a read (not misclassified as a write because ANALYZE
// isn't a recognized read verb), while "ANALYZE DELETE ..." must not.
func TestIsReadAfterStripExplainAnalyze(t *testing.T) {
	if !IsRead(StripExplainAnalyze("ANALYZE SELECT * FROM users")) {
		t.Error("expected ANALYZE SELECT to read as a read after stripping the modifier")
	}
	if IsRead(StripExplainAnalyze("ANALYZE DELETE FROM users")) {
		t.Error("expected ANALYZE DELETE to still read as a write after stripping the modifier")
	}
}

func TestClassifyDangerousAlwaysHasReason(t *testing.T) {
	dangerous := []string{
		"DROP TABLE t", "TRUNCATE t", "ALTER TABLE t ADD COLUMN x INT",
		"DELETE FROM t", "UPDATE t SET x = 1",
	}
	for _, sql := range dangerous {
		got := Classify(sql)
		if got.Risk != RiskDangerous {
			t.Fatalf("Classify(%q).Risk = %q, want dangerous", sql, got.Risk)
		}
		if got.Reason == "" {
			t.Fatalf("Classify(%q) has no reason for a dangerous classification", sql)
		}
	}
}
