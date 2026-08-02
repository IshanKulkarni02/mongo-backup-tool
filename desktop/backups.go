package main

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
	archivePath, err := pathsafety.SafeJoin(dir, bk.FileName)
	if err != nil {
		return err
	}
	if err := os.Remove(archivePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return store.Update(dir, func(idx *store.Index) error {
		idx.Remove(id)
		return nil
	})
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

	err = store.Update(backupsDir, func(idx *store.Index) error {
		idx.Backups = append(idx.Backups, store.Backup{
			ID:         id,
			Connection: connName,
			Database:   dbName,
			FileName:   fileName,
			SizeBytes:  size,
			CreatedAt:  time.Now().Format(time.RFC3339),
		})
		return nil
	})
	if err != nil {
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
