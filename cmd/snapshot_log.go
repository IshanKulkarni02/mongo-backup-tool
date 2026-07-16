package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/snapshot"
)

var snapshotLogCmd = &cobra.Command{
	Use:   "log",
	Short: "Show snapshot history for a database, newest first",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireConnAndDB(); err != nil {
			return err
		}
		summaries, err := snapshot.Log(snapConn, snapDB)
		if err != nil {
			return err
		}
		if len(summaries) == 0 {
			fmt.Println(`No snapshots yet. Create one with: mongobak snapshot create --connection <name> --db <db> -m "message"`)
			return nil
		}
		for i := len(summaries) - 1; i >= 0; i-- {
			s := summaries[i]
			tag := ""
			if len(s.Tags) > 0 {
				tag = "  [" + strings.Join(s.Tags, ", ") + "]"
			}
			fmt.Printf("%s  %s  %d docs  %s%s\n", s.ID, s.CreatedAt, s.DocCount, s.Message, tag)
		}
		return nil
	},
}

func init() {
	snapshotCmd.AddCommand(snapshotLogCmd)
}
