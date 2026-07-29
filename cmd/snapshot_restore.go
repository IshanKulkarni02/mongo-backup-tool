package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/snapshot"
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
	Example: `  mongobak snapshot restore --connection local --db myapp --snapshot abc123
  mongobak snapshot restore --connection local --db myapp --snapshot abc123 --target-db myapp_staging --drop`,
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

		opts := snapshot.RestoreOptions{
			SourceConnection: snapConn,
			SourceDatabase:   snapDB,
			SnapshotID:       snapRestoreID,
			TargetURI:        targetConn.URI,
			TargetDatabase:   snapRestoreTargetDB,
			Collection:       snapRestoreCollection,
			Drop:             snapRestoreDrop,
		}

		fmt.Printf("Restoring snapshot %s into connection %q...\n", snapRestoreID, targetConnName)
		start := time.Now()
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
	snapshotRestoreCmd.Flags().StringVar(&snapRestoreCollection, "collection", "", "Restore only this collection (defaults to all collections in the snapshot)")
	snapshotRestoreCmd.Flags().BoolVar(&snapRestoreDrop, "drop", false, "Drop existing collections before restoring (an automatic safety snapshot of the target is taken first)")
	snapshotCmd.AddCommand(snapshotRestoreCmd)
}
