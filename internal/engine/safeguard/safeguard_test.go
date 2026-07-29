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

		// A literal "where" inside a string/comment must not be mistaken
		// for a real WHERE clause... but note our regex-based classifier
		// can't tell the difference — this documents the known limitation
		// rather than asserting an unsafe false negative is caught.
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
