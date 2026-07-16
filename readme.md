# mongobak

A cross-platform tool for backing up, restoring, and version-controlling MongoDB
databases — local deployments or Atlas clusters. Works via a plain CLI, an
interactive terminal UI, and (eventually) a native desktop app for macOS and
Windows. One Go core, three interfaces, no divergence.

## Status

Built and tested end-to-end against real MongoDB instances:
- **Connections & classic backups**: full-fidelity `mongodump`/`mongorestore`-based backup/restore.
- **Git-like version control**: content-addressed snapshots with history, diff, tag, restore, and gc. Verified at 1M-document scale (see `internal/snapshot`).

The interactive TUI, remote Git/GitHub sync, and the desktop app are in progress — see the plan for the full feature roadmap.

## Install

Requires [Go](https://go.dev) 1.21+.

```bash
git clone https://github.com/IshanKulkarni02/mongo-backup-tool.git
cd mongo-backup-tool
go build -o mongobak .
```

### MongoDB Database Tools

Backup and restore shell out to `mongodump`/`mongorestore`. Check whether
they're installed and on your PATH:

```bash
./mongobak doctor
```

If they're missing, `doctor` prints install instructions for your OS
(Homebrew on macOS, package manager or direct download on Linux/Windows), or
grab them directly from
[mongodb.com/try/download/database-tools](https://www.mongodb.com/try/download/database-tools).

If the tools live somewhere not on your PATH, point to them directly:

```bash
export MONGOBAK_MONGODUMP_PATH=/path/to/mongodump
export MONGOBAK_MONGORESTORE_PATH=/path/to/mongorestore
```

## Usage

```bash
# Save a connection (local or Atlas)
mongobak connection add local --uri "mongodb://localhost:27017"
mongobak connection add atlas --uri "mongodb+srv://user:pass@cluster0.mongodb.net"

# List saved connections, or test one (lists its databases)
mongobak connection list
mongobak connection test local

# Back up a single database, or every database on the connection
mongobak backup --connection local --db myapp
mongobak backup --connection local

# List local backup archives
mongobak list

# Restore a backup — optionally into a different database name, optionally
# dropping existing collections first
mongobak restore --backup <id> --connection local
mongobak restore --backup <id> --connection local --target-db myapp_staging --drop

# Delete a local backup archive
mongobak delete <id>
```

### Version control (snapshots)

Unlike a classic backup, a snapshot is content-addressed: unchanged documents
are deduped across snapshots, so history builds up cheaply, and you can diff
or roll back to any point.

```bash
# Take a snapshot ("commit") of a database
mongobak snapshot create --connection local --db myapp -m "before migration"

# Show snapshot history for a database, newest first
mongobak snapshot log --connection local --db myapp

# Diff two snapshots, or a snapshot against the live database
mongobak snapshot diff <id-a> <id-b> --connection local --db myapp
mongobak snapshot diff <id-a> --connection local --db myapp --live

# Restore a snapshot — in place, into a different database, or one collection.
# A --drop restore automatically takes a safety snapshot of the target first.
mongobak snapshot restore --snapshot <id> --connection local --db myapp
mongobak snapshot restore --snapshot <id> --connection local --db myapp --target-db myapp_staging --drop
mongobak snapshot restore --snapshot <id> --connection local --db myapp --collection users

# Tag a snapshot (tagged snapshots are always protected from gc)
mongobak snapshot tag <id> v1.0-before-migration --connection local --db myapp

# Prune old untagged snapshots and reclaim unreferenced storage
mongobak snapshot gc --connection local --db myapp --keep-last 10
```

Each connection+database gets its own snapshot store under the config
directory (below), using an embedded [bbolt](https://github.com/etcd-io/bbolt)
database by default — a single file, chosen specifically to avoid the inode
exhaustion and directory-listing slowdowns a one-file-per-document layout
would hit at large scale. Snapshot creation uses `readConcern: snapshot` for
point-in-time consistency when the deployment is a replica set (Atlas
clusters qualify); against a bare standalone `mongod`, it falls back to a
plain scan and says so.

Connections and backups are stored per-user under your OS's standard config
directory (e.g. `~/Library/Application Support/mongobak` on macOS,
`%AppData%\mongobak` on Windows, `~/.config/mongobak` on Linux). Connection
URIs may contain credentials, so `config.json` is written with owner-only
permissions, and passwords are always redacted in CLI output.

## Development

```bash
go build -o mongobak .
go test ./...
```
