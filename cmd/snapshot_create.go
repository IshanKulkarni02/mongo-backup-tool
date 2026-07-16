package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/snapshot"
)

var snapCreateMsg string

var snapshotCreateCmd = &cobra.Command{
	Use:     "create",
	Short:   "Take a snapshot of a database (like `git commit`)",
	Example: `  mongobak snapshot create --connection local --db myapp -m "before migration"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireConnAndDB(); err != nil {
			return err
		}
		conn, err := resolveConn(snapConn)
		if err != nil {
			return err
		}

		fmt.Printf("Snapshotting %q (db=%s)...\n", snapConn, snapDB)
		start := time.Now()
		res, err := snapshot.Create(snapshot.CreateOptions{
			Connection: snapConn,
			URI:        conn.URI,
			Database:   snapDB,
			Message:    snapCreateMsg,
		})
		if err != nil {
			return err
		}
		fmt.Printf("Snapshot %s created: %d docs (%d new objects) in %s\n",
			res.Summary.ID, res.Summary.DocCount, res.Summary.NewObjects, time.Since(start).Round(time.Second))
		if !res.Consistent {
			fmt.Println("Note: this deployment doesn't support readConcern:snapshot (needs a replica set), so this was a plain scan rather than a point-in-time-consistent one.")
		}
		return nil
	},
}

func init() {
	snapshotCreateCmd.Flags().StringVarP(&snapCreateMsg, "message", "m", "", "Snapshot message")
	snapshotCmd.AddCommand(snapshotCreateCmd)
}
