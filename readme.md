# mongobak

A cross-platform tool for backing up, restoring, and version-controlling
MongoDB databases — local deployments or Atlas clusters. `mongobak` gives you
two complementary ways to protect your data: full-fidelity **backups** (via
the official MongoDB Database Tools) for portable, restore-anywhere archives,
and git-like **snapshots** — content-addressed, deduped, diffable,
tag-able history — for lightweight, frequent checkpoints you can roll back to
in seconds.

It runs as a plain CLI today; an interactive terminal UI and a native
Mac/Windows/Linux desktop app are on the roadmap (see [Roadmap](#roadmap)).
All interfaces share the same core, so nothing behaves differently between
them.

## Table of contents

- [Why mongobak](#why-mongobak)
- [Install](#install)
- [Prerequisites: MongoDB Database Tools](#prerequisites-mongodb-database-tools)
- [Getting started](#getting-started)
- [In-tool guide](#in-tool-guide)
- [Connections](#connections)
- [Classic backups](#classic-backups)
- [Snapshots (version control)](#snapshots-version-control)
  - [Concepts](#concepts)
  - [Command reference](#snapshot-command-reference)
- [Backups vs. snapshots — which do I use?](#backups-vs-snapshots--which-do-i-use)
- [Common workflows](#common-workflows)
- [Where your data lives](#where-your-data-lives)
- [Troubleshooting](#troubleshooting)
- [Full command reference](#full-command-reference)
- [Development](#development)
- [Roadmap](#roadmap)

## Why mongobak

Most MongoDB backup tools stop at "run mongodump on a cron." That's fine
until you need to know *what changed* between two backups, or you want to
take a cheap checkpoint every few minutes without burning disk space on
mostly-identical dumps, or you want to roll back one bad migration without
restoring an entire multi-GB archive. mongobak covers both ends:

- Need a **portable, restore-anywhere archive** (e.g. before decommissioning
  a server, or to hand to someone else)? Use a **backup**.
- Need **frequent checkpoints, diffs, and instant rollback** during active
  development or before risky operations (migrations, bulk edits)? Use a
  **snapshot**.

## Install

Requires [Go](https://go.dev) 1.21+.

```bash
git clone https://github.com/IshanKulkarni02/mongo-backup-tool.git
cd mongo-backup-tool
go build -o mongobak .
```

This produces a single `mongobak` binary. Move it onto your `PATH` (e.g.
`sudo mv mongobak /usr/local/bin/` on macOS/Linux) so you can run it from
anywhere, or just invoke it as `./mongobak` from the build directory.

## Prerequisites: MongoDB Database Tools

**Backups** (`mongobak backup`/`restore`) shell out to the official
`mongodump`/`mongorestore` binaries. **Snapshots** (`mongobak snapshot ...`)
talk to MongoDB directly via the Go driver and do *not* need these tools.

Check what's installed:

```bash
mongobak doctor
```

If `mongodump`/`mongorestore` are missing, `doctor` prints install
instructions for your OS:

- **macOS**: `brew tap mongodb/brew && brew install mongodb-database-tools`
- **Linux**: see the [Linux install docs](https://www.mongodb.com/docs/database-tools/installation/installation-linux/)
- **Windows**: see the [Windows install docs](https://www.mongodb.com/docs/database-tools/installation/installation-windows/)
- Or download prebuilt binaries directly from
  [mongodb.com/try/download/database-tools](https://www.mongodb.com/try/download/database-tools)

If the tools are installed somewhere not on your `PATH`, point mongobak at
them directly instead of modifying your `PATH`:

```bash
export MONGOBAK_MONGODUMP_PATH=/path/to/mongodump
export MONGOBAK_MONGORESTORE_PATH=/path/to/mongorestore
```

## Getting started

A five-minute tour, assuming you have a MongoDB instance running locally on
the default port:

```bash
# 1. Check dependencies
mongobak doctor

# 2. Save a connection
mongobak connection add local --uri "mongodb://localhost:27017"

# 3. Confirm it works and see what databases exist
mongobak connection test local

# 4. Take your first snapshot of a database
mongobak snapshot create --connection local --db myapp -m "initial checkpoint"

# 5. See it in your snapshot history
mongobak snapshot log --connection local --db myapp

# 6. Or take a full portable backup instead/as well
mongobak backup --connection local --db myapp
mongobak list
```

That's the whole loop. Everything else in this README is detail on top of
those six commands.

## In-tool guide

Everything in this README is also available inside the tool itself, so you
don't need to leave the terminal or have network access to look something
up:

```bash
mongobak guide             # the full walkthrough
mongobak guide quickstart  # just the getting-started steps
mongobak guide connections # just the connections section
mongobak guide backup      # just classic backups
mongobak guide snapshot    # just snapshots/version control
mongobak guide concepts    # how content-addressing/dedup/diff work
mongobak guide troubleshooting
```

Run `mongobak guide` with no topic to see the full guide, or `mongobak guide`
followed by any topic name above to jump straight to that section.

## Connections

A connection is a saved, named MongoDB URI — local (`mongodb://`) or Atlas
(`mongodb+srv://`). Everything else in mongobak references a database by
`--connection <name> --db <name>` rather than a raw URI, so you type
connection strings (and credentials) once.

```bash
# Add a connection
mongobak connection add local --uri "mongodb://localhost:27017"
mongobak connection add atlas --uri "mongodb+srv://user:pass@cluster0.mongodb.net"

# List saved connections (passwords are always redacted in output)
mongobak connection list

# Test a connection — confirms it's reachable and lists its databases
mongobak connection test local

# Remove a connection
mongobak connection remove local
```

Connection URIs (which may contain credentials) are stored in a config file
with owner-only file permissions — see [Where your data lives](#where-your-data-lives).

## Classic backups

A backup is a single, portable, gzip-compressed archive file produced by
`mongodump --archive --gzip`, restored with `mongorestore`. It's the same
format DBAs have used for years: full fidelity (every BSON type, all
indexes), and the resulting `.archive.gz` file can be copied anywhere and
restored on a totally different machine without mongobak even being
involved (plain `mongorestore --archive=... --gzip` works on it directly).

```bash
# Back up one database
mongobak backup --connection local --db myapp

# Back up every database on the connection
mongobak backup --connection local

# List local backup archives (ID, connection, database, size, date, filename)
mongobak list

# Restore a backup as-is
mongobak restore --backup <id> --connection local

# Restore into a different database name, without touching the original
mongobak restore --backup <id> --connection local --target-db myapp_staging

# Restore, dropping existing collections first (overwrite in place)
mongobak restore --backup <id> --connection local --drop

# Delete a local backup archive
mongobak delete <id>
```

Backups are heavier than snapshots (a full dump every time, no dedup) but
maximally portable and don't depend on mongobak's storage format — treat
them as your "take this and walk away" option.

## Snapshots (version control)

### Concepts

A snapshot is like a git commit for a database:

- **Content-addressed storage**: every document is hashed (SHA-256 of its
  canonical Extended JSON) and stored once, compressed. Taking a second
  snapshot of a mostly-unchanged database only writes the documents that
  actually changed — everything else is deduped against what's already
  stored. This is why snapshots are cheap to take frequently, unlike a full
  backup.
- **History**: `snapshot log` shows every snapshot for a database — who,
  when, message, document count — oldest or newest first.
- **Diff**: `snapshot diff` compares any two snapshots (or a snapshot
  against the *live* database with `--live`) and shows exactly which
  documents were added, modified, or removed, per collection.
- **Restore/rollback**: `snapshot restore` applies a snapshot back onto a
  live database — the whole thing, one collection, or into a different
  database name entirely. A destructive restore (`--drop`) automatically
  takes a safety snapshot of the target first, so an in-place rollback is
  never a one-way door.
- **Tags**: label a snapshot (e.g. `v1.0-before-migration`) so it's easy to
  find and — importantly — tagged snapshots are always protected from
  cleanup.
- **GC**: `snapshot gc` prunes old, untagged snapshots beyond a keep-last-N
  policy and reclaims storage no longer referenced by any remaining
  snapshot.
- **Point-in-time consistency**: when the deployment is a replica set
  (Atlas clusters qualify), snapshot creation uses MongoDB's
  `readConcern: snapshot` so a multi-collection snapshot reflects one
  consistent instant rather than a rolling scan. Against a bare standalone
  `mongod` (which doesn't support this), it falls back to a plain scan and
  tells you so.

Snapshots are stored in an embedded, single-file database (not one file per
document) specifically so this stays fast and inode-safe even with millions
of documents — this has been load-tested at 1,000,000 documents (see
[internal/snapshot](internal/snapshot)).

### Snapshot command reference

```bash
# Take a snapshot ("commit") of a database
mongobak snapshot create --connection local --db myapp -m "before migration"

# Show snapshot history, newest first
mongobak snapshot log --connection local --db myapp

# Diff two snapshots
mongobak snapshot diff <id-a> <id-b> --connection local --db myapp

# Diff a snapshot against the current, live state of the database
mongobak snapshot diff <id-a> --connection local --db myapp --live

# Restore a snapshot back into the same database, in place
mongobak snapshot restore --snapshot <id> --connection local --db myapp

# Restore into a different database, leaving the original untouched
mongobak snapshot restore --snapshot <id> --connection local --db myapp --target-db myapp_staging

# Restore, dropping existing collections first — this always takes an
# automatic safety snapshot of the target before it touches anything
mongobak snapshot restore --snapshot <id> --connection local --db myapp --drop

# Restore just one collection instead of the whole snapshot
mongobak snapshot restore --snapshot <id> --connection local --db myapp --collection users

# Restore into a different connection entirely (e.g. snapshot from prod, restore to staging)
mongobak snapshot restore --snapshot <id> --connection prod --db myapp --target-connection staging

# Tag a snapshot — tagged snapshots are always kept, never garbage-collected
mongobak snapshot tag <id> v1.0-before-migration --connection local --db myapp

# Prune old untagged snapshots beyond the 10 most recent, and reclaim their storage
mongobak snapshot gc --connection local --db myapp --keep-last 10
```

Snapshot IDs can be shortened to any unique prefix — you don't need to type
the full UUID as long as it's unambiguous.

## Backups vs. snapshots — which do I use?

| | Classic backup | Snapshot |
|---|---|---|
| Format | Portable `.archive.gz` file | Content-addressed store, not portable as a single file |
| Cost per checkpoint | Full dump every time | Only changed documents are stored |
| History/diff | No — one file, one point in time | Yes — full history, diff between any two points |
| Restore elsewhere | Yes, with plain `mongorestore`, no mongobak needed | Only via mongobak, and only from the same store |
| Best for | Portable exports, "walk away with this," disaster recovery archives | Frequent checkpoints, pre-migration safety, rollback, understanding what changed |

Many workflows use both: a snapshot before every risky operation for instant
rollback, and a periodic classic backup for off-site, portable disaster
recovery.

## Common workflows

**Before a risky migration or bulk edit:**
```bash
mongobak snapshot create --connection prod --db myapp -m "before user-schema migration"
mongobak snapshot tag <id> pre-migration
# ...run your migration...
mongobak snapshot diff pre-migration --connection prod --db myapp --live   # see exactly what changed
# if it went wrong:
mongobak snapshot restore --snapshot pre-migration --connection prod --db myapp --drop
```

**Checking what changed since yesterday, without restoring anything:**
```bash
mongobak snapshot log --connection prod --db myapp
mongobak snapshot diff <yesterdays-id> --connection prod --db myapp --live
```

**Copying a database's state from one environment to another:**
```bash
mongobak snapshot create --connection prod --db myapp -m "sync to staging"
mongobak snapshot restore --snapshot <id> --connection prod --db myapp \
  --target-connection staging --target-db myapp --drop
```

**Scheduled backups via cron** (every command is fully scriptable — no
interactive prompts):
```cron
0 * * * * /usr/local/bin/mongobak snapshot create --connection prod --db myapp -m "hourly checkpoint"
0 2 * * * /usr/local/bin/mongobak backup --connection prod --db myapp
30 2 * * * /usr/local/bin/mongobak snapshot gc --connection prod --db myapp --keep-last 168
```

## Where your data lives

Connections, backups, and snapshots are stored per-user under your OS's
standard config directory:

- macOS: `~/Library/Application Support/mongobak`
- Windows: `%AppData%\mongobak`
- Linux: `~/.config/mongobak`

Layout:
```
mongobak/
├── config.json                    # saved connections (owner-only permissions: credentials live here)
├── backups/                       # classic backup archives + index.json
└── snapshots/
    └── <connection>__<database>/  # one store per connection+database
        ├── backend.json           # which storage engine this scope uses
        ├── store.bolt             # the embedded object + doc-ref store (default backend)
        ├── manifests/*.json       # small per-snapshot metadata files
        └── index.json             # snapshot history index
```

Because `config.json` can contain database credentials, it's written with
owner-only (`0600`) file permissions, and mongobak always redacts passwords
in any command output.

## Troubleshooting

**`mongodump`/`mongorestore` not found**
Run `mongobak doctor` for OS-specific install instructions, or set
`MONGOBAK_MONGODUMP_PATH`/`MONGOBAK_MONGORESTORE_PATH` if they're installed
somewhere not on your `PATH`.

**"this deployment doesn't support readConcern:snapshot"**
This is informational, not an error — it means the target is a standalone
`mongod` rather than a replica set, so mongobak took a plain (non-transactional)
snapshot instead of a point-in-time-consistent one. Atlas clusters and local
replica sets (`mongod --replSet <name>`) support the consistent path.

**`opening snapshot store ...: timeout`**
The embedded snapshot store can only be opened by one process at a time.
This usually means another mongobak command (or a hung previous run) still
has that connection+database's store open — make sure no other mongobak
process is running against the same connection+database, then retry.

**No connection named "X"**
Check `mongobak connection list` — connection names are case-sensitive and
must be added with `connection add` before they can be used elsewhere.

## Full command reference

```
mongobak connection add <name> --uri <uri>      Save a connection
mongobak connection list                        List saved connections
mongobak connection test <name>                 Test a connection, list its databases
mongobak connection remove <name>                Remove a saved connection

mongobak backup --connection <name> [--db <db>] Back up one database, or all databases
mongobak list                                   List local backup archives
mongobak restore --backup <id> --connection <name> [--target-db <db>] [--drop]
mongobak delete <backup-id>                     Delete a local backup archive

mongobak snapshot create --connection <name> --db <db> [-m "message"]
mongobak snapshot log --connection <name> --db <db>
mongobak snapshot diff <id-a> [id-b] --connection <name> --db <db> [--live]
mongobak snapshot restore --snapshot <id> --connection <name> --db <db>
    [--target-connection <name>] [--target-db <db>] [--collection <name>] [--drop]
mongobak snapshot tag <id> <tag> --connection <name> --db <db>
mongobak snapshot gc --connection <name> --db <db> [--keep-last <n>]

mongobak doctor                                 Check mongodump/mongorestore are installed
mongobak guide [topic]                          Show the in-tool usage guide
mongobak version                                Print mongobak's version
```

Every command supports `-h`/`--help` for its full flag list.

## Development

```bash
go build -o mongobak .
go test ./...
go vet ./...
gofmt -l .
```

The codebase is organized as:
- `cmd/` — CLI commands (Cobra)
- `internal/config/` — saved connections
- `internal/mongotools/` — mongodump/mongorestore wrappers, connection testing
- `internal/store/` — classic backup index
- `internal/snapshot/` — the version-control engine (storage backends, diff, restore, gc)

## Roadmap

- Interactive terminal UI (arrow-key navigation, no need to memorize flags)
- Native desktop app for macOS (`.dmg`) and Windows (`.exe`), also usable on Linux
- Remote sync: push/pull snapshot history to a Git/GitHub remote (via Git LFS)
- Smart dependency manager: one-click automatic install of missing tools
- Scheduling built into the tool itself (no external cron needed)
