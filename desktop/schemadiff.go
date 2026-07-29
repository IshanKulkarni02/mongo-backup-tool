package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/engine"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/migrations"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/schemadiff"
)

// collectSchemas introspects every table in one connection/database.
func (a *App) collectSchemas(connectionName, database string) ([]engine.TableSchema, error) {
	sess, release, err := a.sqlSession(connectionName)
	if err != nil {
		return nil, err
	}
	defer release()

	ctx := context.Background()
	namespaces, err := sess.ListNamespaces(ctx, database)
	if err != nil {
		return nil, err
	}
	out := make([]engine.TableSchema, 0, len(namespaces))
	for _, ns := range namespaces {
		schema, err := sess.TableSchema(ctx, database, ns.Name)
		if err != nil {
			continue // an uninspectable table just doesn't participate in the diff
		}
		out = append(out, schema)
	}
	return out, nil
}

// DiffSchemas compares every table across two connection/database pairs
// (which may be the same connection, e.g. diffing "local" against
// "staging", or a schema-per-tenant pair) and returns one TableDiff per
// table that differs or exists on only one side.
func (a *App) DiffSchemas(connA, databaseA, connB, databaseB string) ([]schemadiff.TableDiff, error) {
	before, err := a.collectSchemas(connA, databaseA)
	if err != nil {
		return nil, err
	}
	after, err := a.collectSchemas(connB, databaseB)
	if err != nil {
		return nil, err
	}
	return schemadiff.Diff(before, after), nil
}

// GenerateSchemaMigration diffs two connection/database pairs and renders
// a migration script for dialect (see schemadiff.GenerateMigration for
// what is and isn't auto-generated).
func (a *App) GenerateSchemaMigration(connA, databaseA, connB, databaseB, dialect string) (schemadiff.Migration, error) {
	diffs, err := a.DiffSchemas(connA, databaseA, connB, databaseB)
	if err != nil {
		return schemadiff.Migration{}, err
	}
	return schemadiff.GenerateMigration(diffs, dialect), nil
}

// PickMigrationsFolder opens a native directory picker for choosing where
// generated migration scripts get saved, returning "" if the user cancels.
func (a *App) PickMigrationsFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose a migrations folder",
	})
}

// SaveMigration writes a migration script to folder, committing it if
// folder is a git working tree (see internal/migrations).
func (a *App) SaveMigration(folder, name, sql, commitMessage string) (migrations.SaveResult, error) {
	return migrations.Save(folder, name, sql, commitMessage)
}
