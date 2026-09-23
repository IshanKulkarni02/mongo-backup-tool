package service

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
func ListBackups() ([]store.Backup, error) {
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

// CreateBackup runs a classic mongodump backup of one database (or all of
// them when database is empty) and records it in the backup index.
func CreateBackup(connName, database string) (string, error) {
	conn, err := ResolveConn(connName)
	if err != nil {
		return "", err
	}
	backupsDir, err := config.BackupsDir()
	if err != nil {
		return "", err
	}
	label := database
	if label == "" {
		label = "all"
	}
	id := uuid.NewString()
	fileName := fmt.Sprintf("%s_%s_%s.archive.gz", pathsafety.SanitizeComponent(connName), pathsafety.SanitizeComponent(label), time.Now().Format("20060102-150405"))
	archivePath := filepath.Join(backupsDir, fileName)

	if _, err := mongotools.Dump(mongotools.DumpOptions{URI: conn.URI, Database: database, ArchivePath: archivePath}); err != nil {
		return "", err
	}
	var size int64
	if info, statErr := os.Stat(archivePath); statErr == nil {
		size = info.Size()
	}
	err = store.Update(backupsDir, func(idx *store.Index) error {
		idx.Backups = append(idx.Backups, store.Backup{
			ID: id, Connection: connName, Database: database, FileName: fileName,
			SizeBytes: size, CreatedAt: time.Now().Format(time.RFC3339),
		})
		return nil
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// RestoreBackup restores a backup archive over its source database
// (drop + restore) on the named connection.
func RestoreBackup(connName, backupID string) error {
	conn, err := ResolveConn(connName)
	if err != nil {
		return err
	}
	if err := RequireWritable(connName); err != nil {
		return err
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
		return fmt.Errorf("no backup with id %q", backupID)
	}
	archivePath, err := pathsafety.SafeJoin(backupsDir, bk.FileName)
	if err != nil {
		return err
	}
	_, err = mongotools.Restore(mongotools.RestoreOptions{URI: conn.URI, ArchivePath: archivePath, SourceDB: bk.Database, Drop: true})
	return err
}

// DeleteBackup removes a local backup archive and its index entry.
func DeleteBackup(id string) error {
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
