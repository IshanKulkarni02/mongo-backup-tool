package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/mongotools"
	"github.com/IshanKulkarni02/dbhelm/internal/pathsafety"
	"github.com/IshanKulkarni02/dbhelm/internal/store"
)

// runBackup mirrors cmd.RunBackup's logic (classic mongodump archive +
// index bookkeeping). It's duplicated rather than shared because cmd
// imports internal/tui to launch it, so internal/tui can't import cmd back
// without a cycle; the two stay in sync by both being thin wrappers over
// internal/mongotools and internal/store.
func runBackup(connName, uri, dbName string) (string, error) {
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

	if _, err := mongotools.Dump(mongotools.DumpOptions{
		URI:         uri,
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
		return "", cleanupOrphanedArchive(archivePath, err)
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
		return "", cleanupOrphanedArchive(archivePath, err)
	}
	return id, nil
}

// cleanupOrphanedArchive removes an archive file that mongotools.Dump
// already wrote to disk but that never made it into the backup index
// (the index load/save that follows the dump failed). Left in place,
// that file would be invisible to Backup: list/restore — it's not in the
// index — yet would permanently consume disk space, and a retry would
// just create another archive alongside it rather than reusing or
// clearing the orphan. Best-effort: if the removal itself fails too,
// both errors are surfaced so the archive's path is at least visible in
// the reported error instead of disappearing silently.
func cleanupOrphanedArchive(archivePath string, cause error) error {
	if rmErr := os.Remove(archivePath); rmErr != nil && !os.IsNotExist(rmErr) {
		return fmt.Errorf("backup metadata save failed: %w (and failed to remove the orphaned archive %q: %v — remove it manually)", cause, archivePath, rmErr)
	}
	return fmt.Errorf("backup metadata save failed: %w (removed the orphaned archive %q)", cause, archivePath)
}

// runBackupRestore restores a backup archive in place (drop + restore),
// mirroring cmd's restore command.
func runBackupRestore(connName, uri, backupID string) error {
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
		return fmt.Errorf("no backup with id %q", backupID)
	}
	archivePath, err := pathsafety.SafeJoin(backupsDir, bk.FileName)
	if err != nil {
		return err
	}

	_, err = mongotools.Restore(mongotools.RestoreOptions{
		URI:         uri,
		ArchivePath: archivePath,
		SourceDB:    bk.Database,
		Drop:        true,
	})
	return err
}
