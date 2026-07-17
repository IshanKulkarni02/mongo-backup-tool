package ai

import (
	"fmt"
	"strings"
)

// BuildNLToSQLPrompt scopes the model to just the schemas the user has
// open/selected (RAG-lite — an explicit allowlist rather than embeddings,
// since a database client already knows exactly which tables are in view),
// so the request never needs — or leaks — the whole database's schema.
func BuildNLToSQLPrompt(dialect string, schemaDDL []string, request string) []Message {
	sys := fmt.Sprintf(
		"You are a %s SQL assistant embedded in a database client. Generate a single SQL statement "+
			"that satisfies the user's request, using only the tables described below. "+
			"Respond with ONLY the SQL statement — no explanation, no markdown code fences.\n\nSchema:\n%s",
		dialect, strings.Join(schemaDDL, "\n\n"),
	)
	return []Message{{Role: "system", Content: sys}, {Role: "user", Content: request}}
}

// BuildNLToAggregationPrompt is BuildNLToSQLPrompt's MongoDB counterpart:
// the model returns a pipeline (a JSON array of stage documents) instead
// of a SQL statement.
func BuildNLToAggregationPrompt(collectionName string, schemaSample string, request string) []Message {
	sys := fmt.Sprintf(
		"You are a MongoDB aggregation assistant embedded in a database client. Generate a single "+
			"aggregation pipeline for the collection %q that satisfies the user's request, based on the "+
			"sample document shape below. Respond with ONLY a JSON array of pipeline stages — no "+
			"explanation, no markdown code fences.\n\nSample document:\n%s",
		collectionName, schemaSample,
	)
	return []Message{{Role: "system", Content: sys}, {Role: "user", Content: request}}
}

// BuildExplainTuningPrompt asks the model to translate a raw EXPLAIN
// output into plain-English tuning advice.
func BuildExplainTuningPrompt(dialect, query, explainOutput string) []Message {
	sys := fmt.Sprintf(
		"You are a %s performance-tuning assistant. Given a query and its EXPLAIN output, explain in "+
			"plain English what the database is doing, flag likely problems (sequential scans, missing "+
			"indexes, expensive sorts/joins), and suggest concrete fixes. Be concise.",
		dialect,
	)
	user := fmt.Sprintf("Query:\n%s\n\nEXPLAIN output:\n%s", query, explainOutput)
	return []Message{{Role: "system", Content: sys}, {Role: "user", Content: user}}
}

// BuildErrorFixPrompt asks the model to correct a failed statement given
// the database's own error message and the relevant schema.
func BuildErrorFixPrompt(dialect, query, errMsg string, schemaDDL []string) []Message {
	sys := fmt.Sprintf(
		"You are a %s SQL assistant embedded in a database client. The user's statement failed. "+
			"Using the schema below, return a corrected statement. Respond with ONLY the corrected "+
			"SQL statement — no explanation, no markdown code fences.\n\nSchema:\n%s",
		dialect, strings.Join(schemaDDL, "\n\n"),
	)
	user := fmt.Sprintf("Statement:\n%s\n\nError:\n%s", query, errMsg)
	return []Message{{Role: "system", Content: sys}, {Role: "user", Content: user}}
}

// BuildMockDataPrompt asks the model to generate realistic INSERT
// statements for a table, informed by its column names/types rather than
// generic random values.
func BuildMockDataPrompt(dialect, table string, schemaDDL string, rowCount int) []Message {
	sys := fmt.Sprintf(
		"You are a %s test-data assistant embedded in a database client. Generate %d realistic INSERT "+
			"statements for the table below — infer sensible values from each column's name and type "+
			"(e.g. an \"email\" column gets real-looking emails, not random strings). Respond with ONLY "+
			"the INSERT statements, one per line, no explanation, no markdown code fences.\n\nSchema:\n%s",
		dialect, rowCount, schemaDDL,
	)
	user := fmt.Sprintf("Generate %d rows for %s.", rowCount, table)
	return []Message{{Role: "system", Content: sys}, {Role: "user", Content: user}}
}
