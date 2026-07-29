package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/pathsafety"
	"github.com/IshanKulkarni02/dbhelm/internal/store"
)

var deleteCmd = &cobra.Command{
	Use:   "delete <backup-id>",
	Short: "Delete a local backup archive",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		backupsDir, err := config.BackupsDir()
		if err != nil {
			return err
		}
		idx, err := store.Load(backupsDir)
		if err != nil {
			return err
		}
		bk, ok := idx.Find(args[0])
		if !ok {
			return fmt.Errorf("no backup with id %q", args[0])
		}
		archivePath, err := pathsafety.SafeJoin(backupsDir, bk.FileName)
		if err != nil {
			return err
		}
		if err := os.Remove(archivePath); err != nil && !os.IsNotExist(err) {
			return err
		}
		idx.Remove(args[0])
		if err := store.Save(backupsDir, idx); err != nil {
			return err
		}
		fmt.Printf("Deleted backup %s\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(deleteCmd)
}
