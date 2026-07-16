package main

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

// ListBackups returns every local backup archive.
func (a *App) ListBackups() ([]store.Backup, error) {
	dir, err := config.BackupsDir()
	if err != nil {
		return nil, err
	}
	idx, err := store.Load(dir)
	if err != nil {
		return nil, err
	}
	if idx.Backups == nil {
		return []store.Backup{}, nil
	}
	return idx.Backups, nil
}

// CreateBackup starts a classic mongodump backup as a background job.
func (a *App) CreateBackup(connectionName, database string) (string, error) {
	conn, err := a.resolveConn(connectionName)
	if err != nil {
		return "", err
	}
	return a.jobs.run("backup-create", func() (any, error) {
		id, err := runBackup(connectionName, conn.URI, database)
		if err != nil {
			return nil, err
		}
		return map[string]string{"backupId": id}, nil
	}), nil
}

// RestoreBackup starts an in-place backup restore (drop + restore) as a
// background job.
func (a *App) RestoreBackup(connectionName, backupID string) (string, error) {
	conn, err := a.resolveConn(connectionName)
	if err != nil {
		return "", err
	}
	return a.jobs.run("backup-restore", func() (any, error) {
		if err := runBackupRestore(connectionName, conn.URI, backupID); err != nil {
			return nil, err
		}
		return nil, nil
	}), nil
}

// DeleteBackup removes a local backup archive.
func (a *App) DeleteBackup(id string) error {
	dir, err := config.BackupsDir()
	if err != nil {
		return err
	}
	idx, err := store.Load(dir)
	if err != nil {
		return err
	}
	bk, ok := idx.Find(id)
	if !ok {
		return fmt.Errorf("no backup with id %q", id)
	}
	if err := os.Remove(filepath.Join(dir, bk.FileName)); err != nil && !os.IsNotExist(err) {
		return err
	}
	idx.Remove(id)
	return store.Save(dir, idx)
}

// runBackup mirrors cmd.RunBackup / internal/tui's identical helper. It's
// duplicated (not imported) because neither cmd nor internal/tui is
// reusable here without creating an import cycle risk across modules; all
// three stay in sync by being thin wrappers over internal/mongotools and
// internal/store.
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
