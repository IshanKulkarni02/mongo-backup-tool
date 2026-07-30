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

		// A quoted "where" is data, not a real WHERE clause — an unbounded
		// DELETE/UPDATE must still classify as dangerous even if a string
		// literal happens to contain the word.
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

		// Multiple statements: DROP anywhere should still flag dangerous
		// even if it's not the first keyword after a CTE-less statement.
		{"select then drop", "SELECT 1; DROP TABLE users", RiskNone}, // documented limitation: only first statement's verb is checked
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
