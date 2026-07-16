package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/config"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/mongotools"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/store"
)

var (
	restoreBackupID string
	restoreConn     string
	restoreTargetDB string
	restoreDrop     bool
)

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore a local backup archive into a saved connection",
	Example: `  mongobak restore --backup <id> --connection local
  mongobak restore --backup <id> --connection local --target-db myapp_restored --drop`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunRestore(restoreBackupID, restoreConn, restoreTargetDB, restoreDrop)
	},
}

// RunRestore restores a backup archive into a saved connection. It's shared
// by the CLI and the web UI.
func RunRestore(backupID, connName, targetDB string, drop bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	conn, ok := cfg.Find(connName)
	if !ok {
		return fmt.Errorf("no connection named %q (see: mongobak connection list)", connName)
	}

	backupsDir, err := config.BackupsDir()
	if err != nil {
		return err
	}
	idx, err := store.Load(backupsDir)
	if err != nil {
		return err
	}
	bk, ok := idx.Find(backupID)
	if !ok {
		return fmt.Errorf("no backup with id %q (see: mongobak list)", backupID)
	}

	archivePath := filepath.Join(backupsDir, bk.FileName)

	target := targetDB
	if target == "" {
		target = bk.Database
		if target == "" {
			target = "(all)"
		}
	}
	fmt.Printf("Restoring %s into connection %q, db=%s...\n", bk.FileName, connName, target)

	start := time.Now()
	if _, err := mongotools.Restore(mongotools.RestoreOptions{
		URI:         conn.URI,
		ArchivePath: archivePath,
		SourceDB:    bk.Database,
		TargetDB:    targetDB,
		Drop:        drop,
	}); err != nil {
		return err
	}

	fmt.Printf("Restore complete in %s\n", time.Since(start).Round(time.Second))
	return nil
}

func init() {
	restoreCmd.Flags().StringVar(&restoreBackupID, "backup", "", "Backup ID to restore (required, see: mongobak list)")
	restoreCmd.Flags().StringVar(&restoreConn, "connection", "", "Saved connection name to restore into (required)")
	restoreCmd.Flags().StringVar(&restoreTargetDB, "target-db", "", "Restore into a different database name than the one backed up")
	restoreCmd.Flags().BoolVar(&restoreDrop, "drop", false, "Drop existing collections before restoring")
	restoreCmd.MarkFlagRequired("backup")
	restoreCmd.MarkFlagRequired("connection")
	rootCmd.AddCommand(restoreCmd)
}
