package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/config"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/humansize"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/mongotools"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/store"
)

var (
	backupConn string
	backupDB   string
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Back up a database (or all databases) from a saved connection",
	Example: `  mongobak backup --connection local --db myapp
  mongobak backup --connection prod  # all databases`,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := RunBackup(backupConn, backupDB)
		if err != nil {
			return err
		}
		fmt.Println("Backup ID:", id)
		return nil
	},
}

// RunBackup performs a backup and records it in the local index, returning
// the new backup's ID. It's shared by the CLI and the web UI.
func RunBackup(connName, dbName string) (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	conn, ok := cfg.Find(connName)
	if !ok {
		return "", fmt.Errorf("no connection named %q (see: mongobak connection list)", connName)
	}

	backupsDir, err := config.BackupsDir()
	if err != nil {
		return "", err
	}

	label := dbName
	if label == "" {
		label = "all"
	}
	id := uuid.NewString()
	fileName := fmt.Sprintf("%s_%s_%s.archive.gz", connName, label, time.Now().Format("20060102-150405"))
	archivePath := filepath.Join(backupsDir, fileName)

	fmt.Printf("Backing up %q (db=%s)...\n", connName, label)
	start := time.Now()
	if _, err := mongotools.Dump(mongotools.DumpOptions{
		URI:         conn.URI,
		Database:    dbName,
		ArchivePath: archivePath,
	}); err != nil {
		return "", err
	}

	var size int64
	if info, statErr := os.Stat(archivePath); statErr == nil {
		size = info.Size()
	}

	idx, err := store.Load(backupsDir)
	if err != nil {
		return "", err
	}
	idx.Backups = append(idx.Backups, store.Backup{
		ID:         id,
		Connection: connName,
		Database:   dbName,
		FileName:   fileName,
		SizeBytes:  size,
		CreatedAt:  time.Now().Format(time.RFC3339),
	})
	if err := store.Save(backupsDir, idx); err != nil {
		return "", err
	}

	fmt.Printf("Backup complete: %s (%s) in %s\n", fileName, humansize.Format(size), time.Since(start).Round(time.Second))
	return id, nil
}

func init() {
	backupCmd.Flags().StringVar(&backupConn, "connection", "", "Saved connection name (required)")
	backupCmd.Flags().StringVar(&backupDB, "db", "", "Database name (omit to back up all databases)")
	backupCmd.MarkFlagRequired("connection")
	rootCmd.AddCommand(backupCmd)
}
