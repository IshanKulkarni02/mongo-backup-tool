<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/brand/dbhelm-logo-dark.png">
  <img src="assets/brand/dbhelm-logo-light.png" alt="DBHelm" width="360">
</picture>

A cross-platform tool for backing up, restoring, and version-controlling
MongoDB databases — local deployments or Atlas clusters. `dbhelm` gives you
two complementary ways to protect your data: full-fidelity **backups** (via
the official MongoDB Database Tools) for portable, restore-anywhere archives,
and git-like **snapshots** — content-addressed, deduped, diffable,
tag-able history — for lightweight, frequent checkpoints you can roll back to
in seconds.

It ships as three interfaces over the same core, so nothing behaves
differently between them: a scriptable **CLI**, an interactive
arrow-key-driven **terminal UI**, and a native Mac/Windows/Linux **desktop
app** (see [Desktop app](#desktop-app)). None of the desktop app's builds
are code-signed yet — see [Distribution](#distribution) for what that means
on first launch.

## Table of contents

- [Why dbhelm](#why-dbhelm)
- [Install](#install)
- [Prerequisites: MongoDB Database Tools](#prerequisites-mongodb-database-tools)
- [Getting started](#getting-started)
- [Interactive mode (TUI)](#interactive-mode-tui)
- [Desktop app](#desktop-app)
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

## Why dbhelm

Most MongoDB backup tools stop at "run mongodump on a cron." That's fine
until you need to know *what changed* between two backups, or you want to
take a cheap checkpoint every few minutes without burning disk space on
mostly-identical dumps, or you want to roll back one bad migration without
restoring an entire multi-GB archive. dbhelm covers both ends:

- Need a **portable, restore-anywhere archive** (e.g. before decommissioning
  a server, or to hand to someone else)? Use a **backup**.
- Need **frequent checkpoints, diffs, and instant rollback** during active
  development or before risky operations (migrations, bulk edits)? Use a
  **snapshot**.

## Install

Requires [Go](https://go.dev) 1.26+ (matches `go.mod`/CI exactly; Go's
automatic toolchain management will fetch it for you if your installed
version is older).

```bash
git clone https://github.com/IshanKulkarni02/mongo-backup-tool.git
cd mongo-backup-tool
go build -o dbhelm .
```

This produces a single `dbhelm` binary. Move it onto your `PATH` (e.g.
`sudo mv dbhelm /usr/local/bin/` on macOS/Linux) so you can run it from
anywhere, or just invoke it as `./dbhelm` from the build directory.

## Prerequisites: MongoDB Database Tools

**Backups** (`dbhelm backup`/`restore`) shell out to the official
`mongodump`/`mongorestore` binaries. **Snapshots** (`dbhelm snapshot ...`)
talk to MongoDB directly via the Go driver and do *not* need these tools.

Check what's installed:

```bash
dbhelm doctor
```

If `mongodump`/`mongorestore` are missing, `doctor` prints install
instructions for your OS:

- **macOS**: `brew tap mongodb/brew && brew install mongodb-database-tools`
- **Linux**: see the [Linux install docs](https://www.mongodb.com/docs/database-tools/installation/installation-linux/)
- **Windows**: see the [Windows install docs](https://www.mongodb.com/docs/database-tools/installation/installation-windows/)
- Or download prebuilt binaries directly from
  [mongodb.com/try/download/database-tools](https://www.mongodb.com/try/download/database-tools)

If the tools are installed somewhere not on your `PATH`, point dbhelm at
them directly instead of modifying your `PATH`:

```bash
export DBHELM_MONGODUMP_PATH=/path/to/mongodump
export DBHELM_MONGORESTORE_PATH=/path/to/mongorestore
```

Or let dbhelm install them for you (macOS via Homebrew, Windows via
winget — always asks for confirmation first, never runs silently):

```bash
dbhelm doctor install
```

## Getting started

A five-minute tour, assuming you have a MongoDB instance running locally on
the default port:

```bash
# 1. Check dependencies
dbhelm doctor

# 2. Save a connection
dbhelm connection add local --uri "mongodb://localhost:27017"

# 3. Confirm it works and see what databases exist
dbhelm connection test local

# 4. Take your first snapshot of a database
dbhelm snapshot create --connection local --db myapp -m "initial checkpoint"

# 5. See it in your snapshot history
dbhelm snapshot log --connection local --db myapp

# 6. Or take a full portable backup instead/as well
dbhelm backup --connection local --db myapp
dbhelm list
```

That's the whole loop. Everything else in this README is detail on top of
those six commands.

## Interactive mode (TUI)

Everything above works as flags for scripting/automation, but you don't have
to memorize any of it. Run dbhelm with no arguments for a full-screen,
arrow-key-driven interface over the same core:

```bash
dbhelm
```

It walks you through: pick or add a connection → pick or type a database →
an action menu (snapshot create/history/diff/restore, backup create/list/restore)
→ live progress → a result screen. Destructive actions (restoring in place)
always show an explicit confirm screen first, and — same as the CLI — an
in-place snapshot restore automatically takes a safety snapshot before
touching anything.

## Desktop app

A native Mac/Windows/Linux desktop app (Wails v2 + React/TypeScript) covers
the same connection/backup/snapshot workflows with a full GUI:

- **Connections** — add, test, and manage saved connections.
- **Snapshot timeline** — browse history per database, create/tag/restore
  snapshots, and view diffs (added/modified/removed counts, with paginated
  drill-down into the actual changed document IDs).
- **Database browser** — collection tree with live doc counts/sizes, filtered
  and paginated document viewing/editing, index management.
- **Classic backups** — create, list, and restore `.archive.gz` backups, with
  explicit confirmation before any destructive restore.
- **Job progress** — live progress for long-running operations (snapshot
  create, restore, backup), with a dependency-manager modal that surfaces
  missing `mongodump`/`mongorestore` and offers manual or automatic install.

Build and run it locally — see [Desktop app development](#desktop-app-development)
under [Development](#development). Prebuilt installers are **not
code-signed**; see [Distribution](#distribution) for what that means on
first launch.

## In-tool guide

Everything in this README is also available inside the tool itself, so you
don't need to leave the terminal or have network access to look something
up:

```bash
dbhelm guide             # the full walkthrough
dbhelm guide quickstart  # just the getting-started steps
dbhelm guide connections # just the connections section
dbhelm guide backup      # just classic backups
dbhelm guide snapshot    # just snapshots/version control
dbhelm guide concepts    # how content-addressing/dedup/diff work
dbhelm guide troubleshooting
```

Run `dbhelm guide` with no topic to see the full guide, or `dbhelm guide`
followed by any topic name above to jump straight to that section.

## Connections

A connection is a saved, named MongoDB URI — local (`mongodb://`) or Atlas
(`mongodb+srv://`). Everything else in dbhelm references a database by
`--connection <name> --db <name>` rather than a raw URI, so you type
connection strings (and credentials) once.

```bash
# Add a connection
dbhelm connection add local --uri "mongodb://localhost:27017"
dbhelm connection add atlas --uri "mongodb+srv://user:pass@cluster0.mongodb.net"

# List saved connections (passwords are always redacted in output)
dbhelm connection list

# Test a connection — confirms it's reachable and lists its databases
dbhelm connection test local

# Remove a connection
dbhelm connection remove local
```

Connection URIs (which may contain credentials) are stored in a config file
with owner-only file permissions — see [Where your data lives](#where-your-data-lives).

## Classic backups

A backup is a single, portable, gzip-compressed archive file produced by
`mongodump --archive --gzip`, restored with `mongorestore`. It's the same
format DBAs have used for years: full fidelity (every BSON type, all
indexes), and the resulting `.archive.gz` file can be copied anywhere and
restored on a totally different machine without dbhelm even being
involved (plain `mongorestore --archive=... --gzip` works on it directly).

```bash
# Back up one database
dbhelm backup --connection local --db myapp

# Back up every database on the connection
dbhelm backup --connection local

# List local backup archives (ID, connection, database, size, date, filename)
dbhelm list

# Restore a backup as-is
dbhelm restore --backup <id> --connection local

# Restore into a different database name, without touching the original
dbhelm restore --backup <id> --connection local --target-db myapp_staging

# Restore, dropping existing collections first (overwrite in place)
dbhelm restore --backup <id> --connection local --drop

# Delete a local backup archive
dbhelm delete <id>
```

Backups are heavier than snapshots (a full dump every time, no dedup) but
maximally portable and don't depend on dbhelm's storage format — treat
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
of documents. Snapshot creation, diffing, and restore are all
bounded-memory by design (streamed/chunked, never a full collection or a
full change list held in RAM) — verified by an opt-in load test at
1,000,000 documents with a 15%/5%/2% modify/delete/insert mutation between
two snapshots (`internal/snapshot/loadtest_test.go`; run it yourself with
`DBHELM_LOAD_TEST=1 go test ./internal/snapshot/... -run TestLoadOneMillionDocuments -v`).

### Snapshot command reference

```bash
# Take a snapshot ("commit") of a database
dbhelm snapshot create --connection local --db myapp -m "before migration"

# Show snapshot history, newest first
dbhelm snapshot log --connection local --db myapp

# Diff two snapshots
dbhelm snapshot diff <id-a> <id-b> --connection local --db myapp

# Diff a snapshot against the current, live state of the database
dbhelm snapshot diff <id-a> --connection local --db myapp --live

# Restore a snapshot back into the same database, in place
dbhelm snapshot restore --snapshot <id> --connection local --db myapp

# Restore into a different database, leaving the original untouched
dbhelm snapshot restore --snapshot <id> --connection local --db myapp --target-db myapp_staging

# Restore, dropping existing collections first — this always takes an
# automatic safety snapshot of the target before it touches anything
dbhelm snapshot restore --snapshot <id> --connection local --db myapp --drop

# Restore just one collection instead of the whole snapshot
dbhelm snapshot restore --snapshot <id> --connection local --db myapp --collection users

# Restore into a different connection entirely (e.g. snapshot from prod, restore to staging)
dbhelm snapshot restore --snapshot <id> --connection prod --db myapp --target-connection staging

# Tag a snapshot — tagged snapshots are always kept, never garbage-collected
dbhelm snapshot tag <id> v1.0-before-migration --connection local --db myapp

# Prune old untagged snapshots beyond the 10 most recent, and reclaim their storage
dbhelm snapshot gc --connection local --db myapp --keep-last 10
```

Snapshot IDs can be shortened to any unique prefix — you don't need to type
the full UUID as long as it's unambiguous.

## Backups vs. snapshots — which do I use?

| | Classic backup | Snapshot |
|---|---|---|
| Format | Portable `.archive.gz` file | Content-addressed store, not portable as a single file |
| Cost per checkpoint | Full dump every time | Only changed documents are stored |
| History/diff | No — one file, one point in time | Yes — full history, diff between any two points |
| Restore elsewhere | Yes, with plain `mongorestore`, no dbhelm needed | Only via dbhelm, and only from the same store |
| Best for | Portable exports, "walk away with this," disaster recovery archives | Frequent checkpoints, pre-migration safety, rollback, understanding what changed |

Many workflows use both: a snapshot before every risky operation for instant
rollback, and a periodic classic backup for off-site, portable disaster
recovery.

## Common workflows

**Before a risky migration or bulk edit:**
```bash
dbhelm snapshot create --connection prod --db myapp -m "before user-schema migration"
dbhelm snapshot tag <id> pre-migration
# ...run your migration...
dbhelm snapshot diff pre-migration --connection prod --db myapp --live   # see exactly what changed
# if it went wrong:
dbhelm snapshot restore --snapshot pre-migration --connection prod --db myapp --drop
```

**Checking what changed since yesterday, without restoring anything:**
```bash
dbhelm snapshot log --connection prod --db myapp
dbhelm snapshot diff <yesterdays-id> --connection prod --db myapp --live
```

**Copying a database's state from one environment to another:**
```bash
dbhelm snapshot create --connection prod --db myapp -m "sync to staging"
dbhelm snapshot restore --snapshot <id> --connection prod --db myapp \
  --target-connection staging --target-db myapp --drop
```

**Scheduled backups via cron** (every command is fully scriptable — no
interactive prompts):
```cron
0 * * * * /usr/local/bin/dbhelm snapshot create --connection prod --db myapp -m "hourly checkpoint"
0 2 * * * /usr/local/bin/dbhelm backup --connection prod --db myapp
30 2 * * * /usr/local/bin/dbhelm snapshot gc --connection prod --db myapp --keep-last 168
```

## Where your data lives

Connections, backups, and snapshots are stored per-user under your OS's
standard config directory:

- macOS: `~/Library/Application Support/dbhelm`
- Windows: `%AppData%\dbhelm`
- Linux: `~/.config/dbhelm`

Layout:
```
dbhelm/
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
owner-only (`0600`) file permissions, and dbhelm always redacts passwords
in any command output.

## Remote sync (Git/GitHub)

A database's snapshot history can be pushed to a Git remote — GitHub or
anywhere else — so it's backed up off-machine and shareable. This requires
[Git](https://git-scm.com) and [Git LFS](https://git-lfs.com) (the actual
compressed document content is tracked via LFS so it doesn't bloat the repo
or hit GitHub's file-size limits; only small JSON manifests are tracked as
regular commits).

```bash
# One-time setup for a connection+database — must be a brand-new scope
# (remote sync needs the "fs" storage backend, not the default bbolt one;
# see "Where your data lives" above)
dbhelm remote init --connection local --db myapp --url git@github.com:you/myapp-snapshots.git

# After taking snapshots as usual, push the history
dbhelm snapshot create --connection local --db myapp -m "checkpoint"
dbhelm remote push --connection local --db myapp

# Elsewhere (or after a fresh install), pull an existing history down
dbhelm remote clone git@github.com:you/myapp-snapshots.git --connection local --db myapp

# Keep in sync going forward
dbhelm remote pull --connection local --db myapp
```

`remote init` switches a brand-new connection+database scope to the
file-per-document storage backend and configures Git LFS to track it; it
can't convert a scope that's already using the default bbolt backend — use
a fresh connection or database name for a remote-synced one. Pushing relies
entirely on your own Git credentials (SSH key, `gh auth login`, etc.) —
dbhelm only ever runs `git`/`git-lfs` commands, never stores or asks for
credentials itself.

## Troubleshooting

**`mongodump`/`mongorestore` not found**
Run `dbhelm doctor` for OS-specific install instructions, or set
`DBHELM_MONGODUMP_PATH`/`DBHELM_MONGORESTORE_PATH` if they're installed
somewhere not on your `PATH`.

**"this deployment doesn't support readConcern:snapshot"**
This is informational, not an error — it means the target is a standalone
`mongod` rather than a replica set, so dbhelm took a plain (non-transactional)
snapshot instead of a point-in-time-consistent one. Atlas clusters and local
replica sets (`mongod --replSet <name>`) support the consistent path.

**`opening snapshot store ...: timeout`**
The embedded snapshot store can only be opened by one process at a time.
This usually means another dbhelm command (or a hung previous run) still
has that connection+database's store open — make sure no other dbhelm
process is running against the same connection+database, then retry.

**No connection named "X"**
Check `dbhelm connection list` — connection names are case-sensitive and
must be added with `connection add` before they can be used elsewhere.

## Full command reference

```
dbhelm connection add <name> --uri <uri>      Save a connection
dbhelm connection list                        List saved connections
dbhelm connection test <name>                 Test a connection, list its databases
dbhelm connection remove <name>                Remove a saved connection

dbhelm backup --connection <name> [--db <db>] Back up one database, or all databases
dbhelm list                                   List local backup archives
dbhelm restore --backup <id> --connection <name> [--target-db <db>] [--drop]
dbhelm delete <backup-id>                     Delete a local backup archive

dbhelm snapshot create --connection <name> --db <db> [-m "message"]
dbhelm snapshot log --connection <name> --db <db>
dbhelm snapshot diff <id-a> [id-b] --connection <name> --db <db> [--live]
dbhelm snapshot restore --snapshot <id> --connection <name> --db <db>
    [--target-connection <name>] [--target-db <db>] [--collection <name>] [--drop]
dbhelm snapshot tag <id> <tag> --connection <name> --db <db>
dbhelm snapshot gc --connection <name> --db <db> [--keep-last <n>]

dbhelm remote init --connection <name> --db <db> [--url <git-url>] [--name origin]
dbhelm remote push --connection <name> --db <db> [--remote origin] [--branch main] [-m "message"]
dbhelm remote pull --connection <name> --db <db> [--remote origin] [--branch main]
dbhelm remote clone <git-url> --connection <name> --db <db> [--branch main]

dbhelm scheduler add --connection <name> --action snapshot|backup --interval <dur> [--db <db>] [-m "message"]
dbhelm scheduler list
dbhelm scheduler remove <id>
dbhelm scheduler run                          Run in the foreground, firing due schedules (Ctrl+C to stop)

dbhelm doctor                                 Check mongodump/mongorestore are installed
dbhelm doctor install [--yes]                 Automatically install missing dependencies
dbhelm guide [topic]                          Show the in-tool usage guide
dbhelm version                                Print dbhelm's version
dbhelm                                        Launch the interactive terminal UI
```

Every command supports `-h`/`--help` for its full flag list.

## Development

```bash
go build -o dbhelm .
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
- `internal/depmanager/` — dependency detection and manual/automatic install
- `internal/remote/` — Git/Git-LFS wrapper for remote sync
- `internal/scheduler/` — recurring snapshot/backup jobs (interval-based, no external cron needed)
- `internal/tui/` — the interactive terminal UI (Bubble Tea)
- `desktop/` — the native desktop app (Wails v2 + React/TypeScript), a separate Go module that imports `internal/*` directly

### Desktop app development

```bash
cd desktop
wails dev    # live-reloading dev build
wails build  # production .app / .exe
./build/scripts/package-dmg.sh   # macOS only: package the built .app into a .dmg
```

See [desktop/README.md](desktop/README.md) for the Wails-generated project notes.

## Distribution

`.github/workflows/release.yml` builds the CLI (macOS/Windows/Linux, all
architectures) and the desktop app (`.dmg` on macOS, an NSIS installer
`.exe` on Windows, a binary on Linux) on every `v*` tag push, uploading them
as workflow artifacts. It deliberately does **not** auto-publish a public
GitHub Release — that's a separate, explicit step for a maintainer to
trigger on purpose (`gh release create` with the built artifacts).

None of the built apps/binaries are code-signed or notarized, so first
launch will trigger an OS warning:
- **macOS**: right-click the app → Open, to bypass Gatekeeper's
  unidentified-developer block (only needed once).
- **Windows**: click "More info" → "Run anyway" on the SmartScreen prompt.

Paid code-signing/notarization is future work, not currently set up.

## Roadmap

- Code-signing/notarization for the desktop app
- Auto-publish releases from CI (opt-in, not yet wired up)
