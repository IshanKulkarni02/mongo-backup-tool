package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/snapshot"
)

var snapGCKeepLast int

var snapshotGCCmd = &cobra.Command{
	Use:     "gc",
	Short:   "Prune old untagged snapshots and sweep unreferenced object storage",
	Example: `  mongobak snapshot gc --connection local --db myapp --keep-last 10`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireConnAndDB(); err != nil {
			return err
		}
		result, err := snapshot.GC(snapshot.GCOptions{
			Connection: snapConn,
			Database:   snapDB,
			KeepLast:   snapGCKeepLast,
		})
		if err != nil {
			return err
		}
		fmt.Printf("Deleted %d snapshot(s), freed %d object(s)\n", result.ManifestsDeleted, result.ObjectsDeleted)
		return nil
	},
}

func init() {
	snapshotGCCmd.Flags().IntVar(&snapGCKeepLast, "keep-last", 10, "Keep this many most-recent snapshots regardless of tags (tagged snapshots are always kept)")
	snapshotCmd.AddCommand(snapshotGCCmd)
}
