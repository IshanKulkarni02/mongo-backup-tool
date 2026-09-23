# Using DBHelm through MCP

Agents that speak the Model Context Protocol (Cursor, Codex, Claude Desktop, ...) can use DBHelm
without the CLI. `dbhelm mcp` is an MCP server on stdio; it is a thin adapter over the same broker,
so every rule in `SKILL.md` and `safety.md` applies unchanged.

```json
{ "mcpServers": { "dbhelm": { "command": "dbhelm", "args": ["mcp"], "env": { "DBHELM_RUN_DIR": "<project>/.dbhelm/run" } } } }
```

The VS Code extension writes this for you: **DBHelm: Add MCP Server to Project…** (merges into `.mcp.json`)
or **DBHelm: Copy MCP Config**.

| Tool | Same as |
|---|---|
| `dbhelm_status`, `dbhelm_connections`, `dbhelm_databases`, `dbhelm_schema`, `dbhelm_describe` | `agent status/connections/databases/schema/describe` |
| `dbhelm_query`, `dbhelm_explain` | `agent query/explain` |
| `dbhelm_snapshot_list`, `dbhelm_snapshot_create`, `dbhelm_snapshot_diff` | `agent snapshot list/create/diff` |
| `dbhelm_write`, `dbhelm_snapshot_restore` | `agent write`, `agent snapshot restore` (requests) |
| `dbhelm_request` | `agent request` |

Each tool returns the broker's JSON envelope as text and sets `isError` when `ok` is false.
`dbhelm_write` and `dbhelm_snapshot_restore` wait 45 seconds for a decision by default (most clients
cancel a tool call after about 60 s). A `PENDING_APPROVAL` error is not a failure: a person is deciding.
Poll with `dbhelm_request`; never resend the change.
