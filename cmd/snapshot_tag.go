package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/snapshot"
)

var snapshotTagCmd = &cobra.Command{
	Use:     "tag <snapshot-id> <tag>",
	Short:   "Label a snapshot (tagged snapshots are always protected from gc)",
	Args:    cobra.ExactArgs(2),
	Example: `  mongobak snapshot tag abc123 v1.0-before-migration --connection local --db myapp`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireConnAndDB(); err != nil {
			return err
		}
		if err := snapshot.Tag(snapConn, snapDB, args[0], args[1]); err != nil {
			return err
		}
		fmt.Printf("Tagged %s as %q\n", args[0], args[1])
		return nil
	},
}

func init() {
	snapshotCmd.AddCommand(snapshotTagCmd)
}
