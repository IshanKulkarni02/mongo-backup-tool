package main

import (
	"github.com/IshanKulkarni02/dbhelm/internal/service"
	"github.com/IshanKulkarni02/dbhelm/internal/store"
)

// ListBackups returns every local backup archive.
func (a *App) ListBackups() ([]store.Backup, error) {
	return service.ListBackups()
}

// CreateBackup starts a classic mongodump backup as a background job.
func (a *App) CreateBackup(connectionName, database string) (string, error) {
	if _, err := a.resolveConn(connectionName); err != nil {
		return "", err
	}
	return a.jobs.run("backup-create", func() (any, error) {
		id, err := service.CreateBackup(connectionName, database)
		if err != nil {
			return nil, err
		}
		return map[string]string{"backupId": id}, nil
	}), nil
}

// RestoreBackup starts an in-place backup restore (drop + restore) as a
// background job.
func (a *App) RestoreBackup(connectionName, backupID string) (string, error) {
	if _, err := a.resolveConn(connectionName); err != nil {
		return "", err
	}
	return a.jobs.run("backup-restore", func() (any, error) {
		return nil, service.RestoreBackup(connectionName, backupID)
	}), nil
}

// DeleteBackup removes a local backup archive.
func (a *App) DeleteBackup(id string) error {
	return service.DeleteBackup(id)
}
