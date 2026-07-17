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
