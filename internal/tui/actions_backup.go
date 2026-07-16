package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/config"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/mongotools"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/store"
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
	fileName := fmt.Sprintf("%s_%s_%s.archive.gz", connName, label, time.Now().Format("20060102-150405"))
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
	return id, nil
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

	_, err = mongotools.Restore(mongotools.RestoreOptions{
		URI:         uri,
		ArchivePath: filepath.Join(backupsDir, bk.FileName),
		SourceDB:    bk.Database,
		Drop:        true,
	})
	return err
}
