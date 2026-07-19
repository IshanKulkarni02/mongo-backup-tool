package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/dbhelm/internal/engine"
	"github.com/IshanKulkarni02/dbhelm/internal/snapshot"
)

var (
	snapRestoreID         string
	snapRestoreTargetConn string
	snapRestoreTargetDB   string
	snapRestoreCollection string
	snapRestoreDrop       bool
)

var snapshotRestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore a snapshot into a live database",
	Example: `  dbhelm snapshot restore --connection local --db myapp --snapshot abc123
  dbhelm snapshot restore --connection local --db myapp --snapshot abc123 --target-db myapp_staging --drop`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireConnAndDB(); err != nil {
			return err
		}
		if snapRestoreID == "" {
			return fmt.Errorf("--snapshot is required")
		}

		targetConnName := snapRestoreTargetConn
		if targetConnName == "" {
			targetConnName = snapConn
		}
		targetConn, err := resolveConn(targetConnName)
		if err != nil {
			return err
		}
		eng, err := engine.Lookup(targetConn.EngineID())
		if err != nil {
			return err
		}

		fmt.Printf("Restoring snapshot %s into connection %q...\n", snapRestoreID, targetConnName)
		start := time.Now()

		if eng.Capabilities().SQL {
			sess, release, err := openSQLSession(targetConn)
			if err != nil {
				return err
			}
			defer release()
			// RestoreSQLWithSafety's error message already says whether it
			// auto-rolled back, so it's returned straight through below.
			result, safety, _, err := snapshot.RestoreSQLWithSafety(context.Background(), snapshot.SQLRestoreOptions{
				SourceConnection: snapConn,
				SourceDatabase:   snapDB,
				SnapshotID:       snapRestoreID,
				Session:          sess,
				EngineID:         targetConn.EngineID(),
				TargetDatabase:   snapRestoreTargetDB,
				Table:            snapRestoreCollection,
				Drop:             snapRestoreDrop,
			}, targetConnName)
			if safety != nil {
				fmt.Printf("Safety snapshot taken before restore: %s\n", safety.Summary.ID)
			}
			if err != nil {
				return err
			}
			fmt.Printf("Restored %d rows across %d table(s) into %q in %s\n",
				result.DocsWritten, len(result.Collections), result.Database, time.Since(start).Round(time.Second))
			return nil
		}

		opts := snapshot.RestoreOptions{
			SourceConnection: snapConn,
			SourceDatabase:   snapDB,
			SnapshotID:       snapRestoreID,
			TargetURI:        targetConn.URI,
			TargetDatabase:   snapRestoreTargetDB,
			Collection:       snapRestoreCollection,
			Drop:             snapRestoreDrop,
		}

		// RestoreWithSafety's error message already says whether it
		// auto-rolled back, so it's returned straight through below.
		result, safety, _, err := snapshot.RestoreWithSafety(opts, targetConnName)
		if safety != nil {
			fmt.Printf("Safety snapshot taken before restore: %s\n", safety.Summary.ID)
		}
		if err != nil {
			return err
		}
		fmt.Printf("Restored %d docs across %d collection(s) into %q in %s\n",
			result.DocsWritten, len(result.Collections), result.Database, time.Since(start).Round(time.Second))
		return nil
	},
}

func init() {
	snapshotRestoreCmd.Flags().StringVar(&snapRestoreID, "snapshot", "", "Snapshot ID (or unique prefix) to restore (required)")
	snapshotRestoreCmd.Flags().StringVar(&snapRestoreTargetConn, "target-connection", "", "Connection to restore into (defaults to --connection)")
	snapshotRestoreCmd.Flags().StringVar(&snapRestoreTargetDB, "target-db", "", "Database name to restore into (defaults to --db)")
	snapshotRestoreCmd.Flags().StringVar(&snapRestoreCollection, "collection", "", "Restore only this collection/table (defaults to all in the snapshot)")
	snapshotRestoreCmd.Flags().BoolVar(&snapRestoreDrop, "drop", false, "Clear existing collections/tables before restoring (an automatic safety snapshot of the target is taken first)")
	snapshotCmd.AddCommand(snapshotRestoreCmd)
}
