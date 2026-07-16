# mongobak

A cross-platform tool for backing up, restoring, and version-controlling MongoDB
databases — local deployments or Atlas clusters. Works via a plain CLI, an
interactive terminal UI, and (eventually) a native desktop app for macOS and
Windows. One Go core, three interfaces, no divergence.

## Status

The CLI foundation is built and tested end-to-end against a real MongoDB
instance: connection management, full-fidelity backup/restore via the
official MongoDB Database Tools. Snapshot-based version control, the
interactive TUI, and the desktop app are in progress — see the plan for the
full feature roadmap.

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
