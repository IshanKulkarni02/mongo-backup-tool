# DBHelm quick reference

DBHelm gives you snapshots and backups of your databases, and lets your coding agents use them
without holding your passwords or changing anything you did not approve.

## The Helm (sidebar)

| Section | What it shows |
|---|---|
| **Bridge** | Requests from agents waiting for you, the mode (Manual Helm or Autopilot), and recent activity |
| **Connections** | Your saved connections; test one, or pick it for snapshots |
| **Snapshots** | Timeline for a database; take one now, or restore an earlier one |
| **Backups** | MongoDB archives made with the Database Tools |

## The Chart (schema and data)

**DBHelm: Open Chart (Schema and Data)** opens a read-only browser: pick a connection and database, choose a table
or collection, and see its **Structure** (columns, keys, indexes) and **Data** (50 rows per page, with a filter).
The filter is a `WHERE` clause for SQL or a JSON filter for MongoDB. Everything runs in a read-only transaction,
so a filter cannot change anything.

## Manual Helm and Autopilot

| Mode | What happens when an agent asks to change data |
|---|---|
| **Manual Helm** (default) | The request appears on the Bridge. You press **Run Statement** or **Refuse**. Nothing runs before that. |
| **Autopilot** | DBHelm approves for you. A safety snapshot is still taken first, and everything is logged. Dangerous changes still wait unless you allowed them in settings. |

The agent never approves its own request. If VS Code is not open, changes are refused (`NO_OPERATOR`), not queued.

## Agent access, per connection

| Setting | Agents can |
|---|---|
| Off (default) | Not see the connection |
| Read | Read schema and data through read-only queries |
| Read and ask to write | Also ask to change data; you approve each change |

Set it under **Connections and Agent Access** (the link icon in the Helm's title bar).

## Commands

| Command | Does |
|---|---|
| DBHelm: Add Connection… | Opens the form |
| DBHelm: Take Snapshot… / Restore Snapshot… | Checkpoint a database, or roll one back |
| DBHelm: Back Up Database… / Restore Backup… | MongoDB archives |
| DBHelm: Open Chart (Schema and Data) | Browse tables and rows, read-only |
| DBHelm: Toggle Autopilot | Switch Autopilot on or off |
| DBHelm: Add MCP Server to Project… / Copy MCP Config | Let Cursor, Codex, Claude Desktop and other MCP clients use DBHelm |
| DBHelm: Set Up Agent Skill… | Teach this project's coding agents to use DBHelm |
| DBHelm: Open Ship's Log | The record of every agent call and decision |
| DBHelm: App Lock Settings… | Touch ID / Windows Hello / app password for approvals |
| DBHelm: Restart Broker | Restart the local service if it stopped |

## Settings

| Setting | Meaning |
|---|---|
| `dbhelm.binaryPath` | Use a specific `dbhelm` binary instead of the bundled one |
| `dbhelm.safetySnapshot` | Snapshot before each approved agent change (default on) |
| `dbhelm.autopilotAllowDangerous` | Let Autopilot approve dangerous changes (default off) |
| `dbhelm.autopilotMinutes` | Switch Autopilot off after N minutes (0 = until VS Code closes) |
| `dbhelm.requireAuth`, `authMethod`, `authTimeoutMinutes` | App Lock |

## Where things live

`.dbhelm/` in your project holds the ship's log (`logbook.jsonl`), the broker's connection file
(`run/`) and `AGENTS.md` for agents. The log and `run/` are added to `.gitignore`. Passwords live in your
system keychain, never in the project. Connections are shared with the DBHelm CLI and desktop app.

## Limits worth knowing

- Agents read through a database-enforced read-only transaction, capped at 100 rows (1000 at most) and 15 seconds.
- Snapshots version data, not table structure.
- DBHelm guards against an agent's mistakes and over-reach. It cannot stop malicious code running as you.
