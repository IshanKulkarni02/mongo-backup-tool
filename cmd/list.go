package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/config"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/humansize"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/store"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List local backup archives",
	RunE: func(cmd *cobra.Command, args []string) error {
		backupsDir, err := config.BackupsDir()
		if err != nil {
			return err
		}
		idx, err := store.Load(backupsDir)
		if err != nil {
			return err
		}
		if len(idx.Backups) == 0 {
			fmt.Println("No backups yet. Create one with: mongobak backup --connection <name> [--db <db>]")
			return nil
		}
		fmt.Printf("%-36s  %-15s  %-15s  %-10s  %-25s  %s\n", "ID", "CONNECTION", "DATABASE", "SIZE", "CREATED", "FILE")
		for _, b := range idx.Backups {
			db := b.Database
			if db == "" {
				db = "(all)"
			}
			fmt.Printf("%-36s  %-15s  %-15s  %-10s  %-25s  %s\n",
				b.ID, b.Connection, db, humansize.Format(b.SizeBytes), b.CreatedAt, b.FileName)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
