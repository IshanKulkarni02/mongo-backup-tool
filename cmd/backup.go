package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/humansize"
	"github.com/IshanKulkarni02/dbhelm/internal/mongotools"
	"github.com/IshanKulkarni02/dbhelm/internal/pathsafety"
	"github.com/IshanKulkarni02/dbhelm/internal/store"
)

var (
	backupConn string
	backupDB   string
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Back up a database (or all databases) from a saved connection",
	Example: `  dbhelm backup --connection local --db myapp
  dbhelm backup --connection prod  # all databases`,
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
		return "", fmt.Errorf("no connection named %q (see: dbhelm connection list)", connName)
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
	fileName := fmt.Sprintf("%s_%s_%s.archive.gz", pathsafety.SanitizeComponent(connName), pathsafety.SanitizeComponent(label), time.Now().Format("20060102-150405"))
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

	if err := store.Update(backupsDir, func(idx *store.Index) error {
		idx.Backups = append(idx.Backups, store.Backup{
			ID:         id,
			Connection: connName,
			Database:   dbName,
			FileName:   fileName,
			SizeBytes:  size,
			CreatedAt:  time.Now().Format(time.RFC3339),
		})
		return nil
	}); err != nil {
		return "", cleanupOrphanedArchive(archivePath, err)
	}

	fmt.Printf("Backup complete: %s (%s) in %s\n", fileName, humansize.Format(size), time.Since(start).Round(time.Second))
	return id, nil
}

// cleanupOrphanedArchive removes an archive file that mongotools.Dump
// already wrote to disk but that never made it into the backup index (the
// index load/save that follows the dump failed). Left in place, that file
// would be invisible to backup list/restore — it's not in the index — yet
// would permanently consume disk space, and a retry would just create
// another archive alongside it rather than reusing or clearing the orphan.
// Best-effort: if the removal itself fails too, both errors are surfaced
// so the archive's path is at least visible in the reported error instead
// of disappearing silently.
//
// Duplicated from internal/tui/actions_backup.go's identical helper
// rather than shared: cmd imports internal/tui to launch it, so
// internal/tui can't import cmd back without a cycle, and this is small
// enough that a shared package isn't worth it either.
func cleanupOrphanedArchive(archivePath string, cause error) error {
	if rmErr := os.Remove(archivePath); rmErr != nil && !os.IsNotExist(rmErr) {
		return fmt.Errorf("backup metadata save failed: %w (and failed to remove the orphaned archive %q: %v — remove it manually)", cause, archivePath, rmErr)
	}
	return fmt.Errorf("backup metadata save failed: %w (removed the orphaned archive %q)", cause, archivePath)
}

func init() {
	backupCmd.Flags().StringVar(&backupConn, "connection", "", "Saved connection name (required)")
	backupCmd.Flags().StringVar(&backupDB, "db", "", "Database name (omit to back up all databases)")
	backupCmd.MarkFlagRequired("connection")
	rootCmd.AddCommand(backupCmd)
}
