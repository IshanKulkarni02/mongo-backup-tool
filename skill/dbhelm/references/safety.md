# What DBHelm guarantees, and what it does not

## Guaranteed by DBHelm

- **Reads cannot write.** Every read runs in a database-enforced read-only
  transaction (Postgres/MySQL `READ ONLY`, SQLite `query_only`); a statement
  that slips past the text filter is still refused by the database.
- **No credentials.** You see connection names only. Errors are scrubbed of
  URIs, hosts and passwords before you see them.
- **You cannot approve your own change.** Approval and Autopilot are only
  reachable with a token the extension holds in memory; the agent token cannot
  call them. With nobody attached, changes are refused (`NO_OPERATOR`), never
  queued to run later.
- **Every call is logged** to the ship's log (`.dbhelm/logbook.jsonl`): who
  asked, what, and what happened. It records statements, never results or
  credentials.
- **Changes are undoable.** A safety snapshot is taken before each approved
  change; its id is in the result.

## Not guaranteed

- DBHelm protects against mistakes and over-reach by an agent. It does not
  defend against arbitrary malicious code running as the same operating-system
  user, which could read the user's keychain or config directly.
- Without an OS keyring (some headless Linux setups), saved passwords sit in
  the owner-only `config.json`. `dbhelm doctor` reports this.
- A read can still be expensive (a full scan within the 15 s limit). Use
  `explain` on big tables.
- Snapshots version data, not table structure: restoring does not undo a
  `CREATE`/`ALTER`.
