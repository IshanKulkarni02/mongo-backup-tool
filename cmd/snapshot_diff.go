package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/snapshot"
)

var snapDiffLive bool

var snapshotDiffCmd = &cobra.Command{
	Use:   "diff <from> [to]",
	Short: "Show what changed between two snapshots, or a snapshot and the live database",
	Args:  cobra.RangeArgs(1, 2),
	Example: `  mongobak snapshot diff abc123 def456 --connection local --db myapp
  mongobak snapshot diff abc123 --connection local --db myapp --live`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireConnAndDB(); err != nil {
			return err
		}

		from, err := snapshot.Get(snapConn, snapDB, args[0])
		if err != nil {
			return err
		}

		// Both sides of a persisted-vs-persisted diff live in the same
		// connection+database scope, so they share one backend handle here
		// rather than each opening their own — the bbolt backend can only
		// be opened once per process at a time.
		scope, err := snapshot.OpenScope(snapConn, snapDB)
		if err != nil {
			return err
		}
		defer scope.Close()
		fromSource := scope.Source(from.ID)

		var diff snapshot.Diff

		switch {
		case len(args) == 2:
			to, err := snapshot.Get(snapConn, snapDB, args[1])
			if err != nil {
				return err
			}
			diff, err = snapshot.Compare(from, fromSource, to, scope.Source(to.ID))
			if err != nil {
				return err
			}

		case snapDiffLive:
			conn, err := resolveConn(snapConn)
			if err != nil {
				return err
			}
			live, err := snapshot.ScanLive(conn.URI, snapDB)
			if err != nil {
				return err
			}
			diff, err = snapshot.Compare(from, fromSource, live.Manifest, live.Source())
			if err != nil {
				return err
			}

		default:
			return fmt.Errorf("provide a second snapshot ID, or pass --live to diff against the current database state")
		}

		return printDiff(diff)
	},
}

func printDiff(diff snapshot.Diff) error {
	if diff.Empty() {
		fmt.Println("No differences.")
		return nil
	}
	names := make([]string, 0, len(diff.Collections))
	for name := range diff.Collections {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cd := diff.Collections[name]
		fmt.Printf("%s:\n", name)
		for _, id := range cd.Added {
			fmt.Printf("  + %s\n", id)
		}
		for _, id := range cd.Modified {
			fmt.Printf("  ~ %s\n", id)
		}
		for _, id := range cd.Removed {
			fmt.Printf("  - %s\n", id)
		}
	}
	return nil
}

func init() {
	snapshotDiffCmd.Flags().BoolVar(&snapDiffLive, "live", false, "Diff against the current (live) state of the database instead of a second snapshot")
	snapshotCmd.AddCommand(snapshotDiffCmd)
}
