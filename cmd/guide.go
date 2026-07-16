package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

type guideSection struct {
	title string
	body  func()
}

var guideSections = map[string]guideSection{
	"quickstart":      {"Quick start", printGuideQuickstart},
	"connections":     {"Connections", printGuideConnections},
	"backup":          {"Classic backups", printGuideBackup},
	"snapshot":        {"Snapshots (version control)", printGuideSnapshot},
	"concepts":        {"Snapshot concepts", printGuideConcepts},
	"compare":         {"Backups vs. snapshots", printGuideCompare},
	"remote":          {"Remote sync (Git/GitHub)", printGuideRemote},
	"troubleshooting": {"Troubleshooting", printGuideTroubleshooting},
}

// guideOrder is the order sections print in when showing the full guide.
var guideOrder = []string{"quickstart", "connections", "backup", "snapshot", "concepts", "compare", "remote", "troubleshooting"}

var guideCmd = &cobra.Command{
	Use:   "guide [topic]",
	Short: "Show the in-tool usage guide",
	Long: `Show the in-tool usage guide — the same walkthrough as the README, without
leaving the terminal or needing network access.

Run with no arguments for the full guide, or pass a topic to jump straight
to it.`,
	Example: `  mongobak guide
  mongobak guide quickstart
  mongobak guide snapshot`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			printGuideBanner()
			for _, key := range guideOrder {
				printGuideHeader(guideSections[key].title)
				guideSections[key].body()
			}
			printGuideFooter()
			return nil
		}

		topic := strings.ToLower(args[0])
		section, ok := guideSections[topic]
		if !ok {
			names := make([]string, 0, len(guideSections))
			for k := range guideSections {
				names = append(names, k)
			}
			sort.Strings(names)
			return fmt.Errorf("unknown guide topic %q — available topics: %s", topic, strings.Join(names, ", "))
		}
		printGuideHeader(section.title)
		section.body()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(guideCmd)
}

func printGuideHeader(title string) {
	fmt.Println()
	fmt.Println(title)
	fmt.Println(strings.Repeat("=", len(title)))
	fmt.Println()
}

func printGuideBanner() {
	fmt.Println("mongobak — backup, restore, and version-control MongoDB databases")
	fmt.Println("Full docs: https://github.com/IshanKulkarni02/mongo-backup-tool")
}

func printGuideFooter() {
	fmt.Println()
	fmt.Println("Jump straight to a section next time: mongobak guide <topic>")
	fmt.Println("Topics: quickstart, connections, backup, snapshot, concepts, compare, troubleshooting")
	fmt.Println()
}

func printGuideQuickstart() {
	fmt.Println(`A five-minute tour, assuming a MongoDB instance running locally on the
default port:

  1. Check dependencies
     mongobak doctor

  2. Save a connection
     mongobak connection add local --uri "mongodb://localhost:27017"

  3. Confirm it works and see what databases exist
     mongobak connection test local

  4. Take your first snapshot of a database
     mongobak snapshot create --connection local --db myapp -m "initial checkpoint"

  5. See it in your snapshot history
     mongobak snapshot log --connection local --db myapp

  6. Or take a full portable backup instead/as well
     mongobak backup --connection local --db myapp
     mongobak list

That's the whole loop — everything else builds on those six commands.`)
}

func printGuideConnections() {
	fmt.Println(`A connection is a saved, named MongoDB URI — local (mongodb://) or Atlas
(mongodb+srv://). Everything else references a database by
--connection <name> --db <name> rather than a raw URI, so credentials are
typed once.

  mongobak connection add local --uri "mongodb://localhost:27017"
  mongobak connection add atlas --uri "mongodb+srv://user:pass@cluster0.mongodb.net"
  mongobak connection list                 # passwords are always redacted
  mongobak connection test local           # confirms reachable, lists its databases
  mongobak connection remove local

Connection URIs may contain credentials, so they're stored in a config file
with owner-only file permissions — see the README's "Where your data lives"
section.`)
}

func printGuideBackup() {
	fmt.Println(`A backup is a single, portable, gzip-compressed archive file produced by
mongodump, restored with mongorestore — full fidelity, and the resulting
.archive.gz file can be copied anywhere and restored on a different machine
without mongobak, using plain mongorestore --archive=... --gzip.

  mongobak backup --connection local --db myapp     # one database
  mongobak backup --connection local                # every database
  mongobak list                                     # local archives: id, db, size, date
  mongobak restore --backup <id> --connection local
  mongobak restore --backup <id> --connection local --target-db myapp_staging
  mongobak restore --backup <id> --connection local --drop   # overwrite in place
  mongobak delete <id>

Backups are heavier than snapshots (a full dump every time, no dedup) but
maximally portable and don't depend on mongobak's storage format — treat
them as your "take this and walk away" option.`)
}

func printGuideSnapshot() {
	fmt.Println(`A snapshot is like a git commit for a database — cheap, frequent, diffable,
and instantly reversible. Snapshot IDs can be shortened to any unique
prefix.

  mongobak snapshot create --connection local --db myapp -m "before migration"
  mongobak snapshot log --connection local --db myapp

  mongobak snapshot diff <id-a> <id-b> --connection local --db myapp
  mongobak snapshot diff <id-a> --connection local --db myapp --live   # vs. live state

  mongobak snapshot restore --snapshot <id> --connection local --db myapp
  mongobak snapshot restore --snapshot <id> --connection local --db myapp --target-db myapp_staging
  mongobak snapshot restore --snapshot <id> --connection local --db myapp --drop
  mongobak snapshot restore --snapshot <id> --connection local --db myapp --collection users
  mongobak snapshot restore --snapshot <id> --connection prod --db myapp --target-connection staging

  mongobak snapshot tag <id> v1.0-before-migration --connection local --db myapp
  mongobak snapshot gc --connection local --db myapp --keep-last 10

A --drop restore always takes an automatic safety snapshot of the target
first, so an in-place rollback is never a one-way door. Tagged snapshots are
always protected from gc.

Run "mongobak guide concepts" for how content-addressing, dedup, and diff
actually work.`)
}

func printGuideConcepts() {
	fmt.Println(`  Content-addressed storage
  Every document is hashed (SHA-256 of its canonical Extended JSON) and
  stored once, compressed. A second snapshot of a mostly-unchanged database
  only writes the documents that actually changed — everything else is
  deduped against what's already stored. This is why snapshots are cheap to
  take frequently, unlike a full backup.

  History
  "snapshot log" shows every snapshot for a database: who, when, message,
  document count.

  Diff
  "snapshot diff" compares any two snapshots (or a snapshot against the
  live database with --live) and shows exactly which documents were added,
  modified, or removed, per collection.

  Restore/rollback
  "snapshot restore" applies a snapshot back onto a live database — the
  whole thing, one collection, or into a different database name entirely.

  Tags
  Label a snapshot so it's easy to find, and so it's always protected from
  gc.

  Point-in-time consistency
  On a replica set (Atlas clusters qualify), snapshot creation uses
  MongoDB's readConcern:snapshot so a multi-collection snapshot reflects
  one consistent instant rather than a rolling scan. A bare standalone
  mongod doesn't support this — mongobak falls back to a plain scan and
  says so.

  Storage engine
  Snapshots live in an embedded, single-file database (not one file per
  document), specifically to stay fast and inode-safe at large scale —
  this has been load-tested at 1,000,000 documents.`)
}

func printGuideCompare() {
	fmt.Println(`  Classic backup                          Snapshot
  -----------------------------------     -----------------------------------
  Portable .archive.gz file               Content-addressed store (not a
                                           single portable file)
  Full dump every time                    Only changed documents are stored
  No history or diff                      Full history, diff between any
                                           two points
  Restorable anywhere with plain          Only restorable via mongobak, from
  mongorestore, no mongobak needed        the same store
  Best for: portable exports, disaster    Best for: frequent checkpoints,
  recovery archives                       pre-migration safety, rollback

Many workflows use both: a snapshot before every risky operation for
instant rollback, and a periodic classic backup for off-site, portable
disaster recovery.`)
}

func printGuideRemote() {
	fmt.Println(`A database's snapshot history can be pushed to a Git remote (GitHub or
anywhere else), backed by Git LFS so the compressed document content
doesn't bloat the repo or hit file-size limits. Requires git and git-lfs.

  mongobak remote init --connection local --db myapp \
      --url git@github.com:you/myapp-snapshots.git

  mongobak snapshot create --connection local --db myapp -m "checkpoint"
  mongobak remote push --connection local --db myapp

  mongobak remote clone git@github.com:you/myapp-snapshots.git \
      --connection local --db myapp
  mongobak remote pull --connection local --db myapp

"remote init" only works on a brand-new connection+database scope — remote
sync needs the file-per-document storage backend, not the default bbolt
one, and an existing bbolt-backed scope can't be converted. Use a fresh
connection or database name for a remote-synced one.

Pushing relies entirely on your own Git credentials (SSH key, gh auth
login, etc.) — mongobak only ever runs git/git-lfs commands, never stores
or asks for credentials itself.`)
}

func printGuideTroubleshooting() {
	fmt.Println(`  mongodump/mongorestore not found
    Run "mongobak doctor" for install instructions, or set
    MONGOBAK_MONGODUMP_PATH / MONGOBAK_MONGORESTORE_PATH if they're
    installed somewhere not on your PATH.

  "this deployment doesn't support readConcern:snapshot"
    Informational, not an error — the target is a standalone mongod rather
    than a replica set, so mongobak took a plain snapshot instead of a
    point-in-time-consistent one.

  "opening snapshot store ...: timeout"
    The embedded snapshot store can only be opened by one process at a
    time. Make sure no other mongobak command is running against the same
    connection+database, then retry.

  "no connection named ..."
    Check "mongobak connection list" — connection names are case-sensitive
    and must be added with "connection add" before use elsewhere.`)
}
