package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/snapshot"
)

var snapDiffLive bool

var snapshotDiffCmd = &cobra.Command{
	Use:   "diff <from> [to]",
	Short: "Show what changed between two snapshots, or a snapshot and the live database",
	Args:  cobra.RangeArgs(1, 2),
	Example: `  dbhelm snapshot diff abc123 def456 --connection local --db myapp
  dbhelm snapshot diff abc123 --connection local --db myapp --live`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireConnAndDB(); err != nil {
			return err
		}
		ctx := cmd.Context()

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

		// printed tracks whether StreamDiff produced anything, so we can
		// still print "No differences." — StreamDiff never builds a full
		// Diff up front, so there's nothing to call .Empty() on beforehand.
		printed := false
		onChange := func(collection string, ct snapshot.ChangeType, id string) error {
			printed = true
			var marker string
			switch ct {
			case snapshot.Added:
				marker = "+"
			case snapshot.Removed:
				marker = "-"
			case snapshot.Modified:
				marker = "~"
			}
			fmt.Printf("%s: %s %s\n", collection, marker, id)
			return nil
		}

		switch {
		case len(args) == 2:
			to, err := snapshot.Get(snapConn, snapDB, args[1])
			if err != nil {
				return err
			}
			if err := snapshot.StreamDiff(ctx, from, fromSource, to, scope.Source(to.ID), onChange); err != nil {
				return err
			}

		case snapDiffLive:
			conn, err := resolveConn(snapConn)
			if err != nil {
				return err
			}
			eng, err := engine.Lookup(conn.EngineID())
			if err != nil {
				return err
			}
			// ScanLive only speaks the Mongo wire protocol; comparing a SQL
			// snapshot against its live database isn't implemented yet.
			if eng.Capabilities().SQL {
				return fmt.Errorf("--live diffing isn't supported for SQL connections yet — diff two snapshots instead")
			}
			live, err := snapshot.ScanLive(conn.URI, snapDB)
			if err != nil {
				return err
			}
			defer live.Close()
			if err := snapshot.StreamDiff(ctx, from, fromSource, live.Manifest, live.Source(), onChange); err != nil {
				return err
			}

		default:
			return fmt.Errorf("provide a second snapshot ID, or pass --live to diff against the current database state")
		}

		if !printed {
			fmt.Println("No differences.")
		}
		return nil
	},
}

func init() {
	snapshotDiffCmd.Flags().BoolVar(&snapDiffLive, "live", false, "Diff against the current (live) state of the database instead of a second snapshot")
	snapshotCmd.AddCommand(snapshotDiffCmd)
}
