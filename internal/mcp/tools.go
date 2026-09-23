package mcp

type tool struct {
	name        string
	method      string // broker agent method
	description string
	props       map[string]any
	required    []string
	defaultWait bool
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }

var (
	pConn = str("Connection name from dbhelm_connections")
	pDB   = str("Database (Postgres: the schema, usually public; SQLite: main)")
)

var tools = []tool{
	{name: "dbhelm_status", method: "ping", description: "Is DBHelm up, and is a person attached who can approve changes?", props: map[string]any{}},
	{name: "dbhelm_connections", method: "connections", description: "List the database connections you may use: name, engine, capabilities and access level. Never returns credentials.", props: map[string]any{}},
	{name: "dbhelm_databases", method: "databases", description: "List a connection's databases (Postgres: schemas).", props: map[string]any{"connection": pConn}, required: []string{"connection"}},
	{name: "dbhelm_schema", method: "schema", description: "List a database's tables or collections with approximate row counts.", props: map[string]any{"connection": pConn, "database": pDB}, required: []string{"connection", "database"}},
	{name: "dbhelm_describe", method: "describe", description: "Describe one table (columns, keys, indexes) or collection (indexes and a sample document). Do this before querying; do not guess names.",
		props: map[string]any{"connection": pConn, "database": pDB, "table": str("SQL table"), "collection": str("Document collection")}, required: []string{"connection", "database"}},
	{name: "dbhelm_query", method: "query", description: "Run a read-only query: SQL, or a document find (collection + filter/sort/skip/limit), or an aggregation pipeline. Runs inside a read-only transaction; results are capped (100 rows by default) and meta.truncated says when. Use LIMIT.",
		props: map[string]any{"connection": pConn, "database": pDB, "sql": str("SQL to run"), "collection": str("Document collection"), "filter": str("Document filter, Extended JSON"), "sort": str("Document sort, Extended JSON"),
			"skip": num("Documents to skip"), "limit": num("Documents to return"), "pipeline": str("Aggregation pipeline, JSON array"), "maxRows": num("Row cap for this call (at most 1000)")}, required: []string{"connection", "database"}},
	{name: "dbhelm_explain", method: "explain", description: "Show a SQL query plan without running the query (plain EXPLAIN only).",
		props: map[string]any{"connection": pConn, "database": pDB, "sql": str("SQL to explain")}, required: []string{"connection", "database", "sql"}},
	{name: "dbhelm_snapshot_list", method: "snapshot.list", description: "List a database's snapshots, newest first.", props: map[string]any{"connection": pConn, "database": pDB}, required: []string{"connection", "database"}},
	{name: "dbhelm_snapshot_create", method: "snapshot.create", description: "Take a snapshot now. Do this before risky work.",
		props: map[string]any{"connection": pConn, "database": pDB, "message": str("What this checkpoint is for")}, required: []string{"connection", "database"}},
	{name: "dbhelm_snapshot_diff", method: "snapshot.diff", description: "Compare two snapshots: per-collection change counts, or with collection and change one page of changed ids. Leave 'to' empty to compare with the live database (MongoDB only).",
		props: map[string]any{"connection": pConn, "database": pDB, "from": str("Older snapshot id"), "to": str("Newer snapshot id"), "collection": str("Return this collection's changed ids"), "change": str("added, modified or removed"), "offset": num("Page offset")}, required: []string{"connection", "database", "from"}},
	{name: "dbhelm_write", method: "write", defaultWait: true, description: "REQUEST a change to data (SQL statement, or a document insert/update/delete). It does not run until a person approves it in VS Code (or Autopilot does). PENDING_APPROVAL means a person is deciding: poll with dbhelm_request, never resend or reword. DENIED means stop and ask the user. Use the narrowest statement (a WHERE on a key) and give a reason.",
		props: map[string]any{"connection": pConn, "database": pDB, "statement": str("SQL statement (SQL connections)"), "collection": str("Document collection (document connections)"), "op": str("insert, update or delete"),
			"document": str("Document, Extended JSON (insert; full replacement for update)"), "id": str("Document _id as JSON (update, delete)"), "reason": str("Why; shown to the user beside the statement"), "wait": num("Seconds to wait for a decision (default 45)")},
		required: []string{"connection", "database"}},
	{name: "dbhelm_snapshot_restore", method: "snapshot.restore", defaultWait: true, description: "REQUEST a restore of a snapshot over the database (replaces current data; a safety snapshot is taken first). Needs a person's approval, like dbhelm_write.",
		props: map[string]any{"connection": pConn, "database": pDB, "snapshotId": str("Snapshot id from dbhelm_snapshot_list"), "reason": str("Why"), "wait": num("Seconds to wait for a decision (default 45)")}, required: []string{"connection", "database", "snapshotId"}},
	{name: "dbhelm_request", method: "request.get", description: "Check on (or wait for) a pending change request by id.",
		props: map[string]any{"id": str("Request id"), "wait": num("Seconds to wait for a decision")}, required: []string{"id"}},
}

func toolByName(name string) (tool, bool) {
	for _, t := range tools {
		if t.name == name {
			return t, true
		}
	}
	return tool{}, false
}

func toolList() []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		schema := map[string]any{"type": "object", "properties": t.props}
		if len(t.required) > 0 {
			schema["required"] = t.required
		}
		out = append(out, map[string]any{"name": t.name, "description": t.description, "inputSchema": schema})
	}
	return out
}
