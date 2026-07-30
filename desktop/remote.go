package main

import (
	"fmt"

	"github.com/IshanKulkarni02/dbhelm/internal/remote"
	"github.com/IshanKulkarni02/dbhelm/internal/snapshot"
)

// IsRemoteInitialized reports whether a database's snapshot scope already
// has Git + Git LFS set up for remote sync.
func (a *App) IsRemoteInitialized(connectionName, database string) (bool, error) {
	scopeDir, err := snapshot.ScopeDir(connectionName, database)
	if err != nil {
		return false, err
	}
	return remote.IsInitialized(scopeDir), nil
}

// InitRemote sets up Git + Git LFS for a database's snapshot scope, and
// optionally adds a remote URL under name (default "origin") in the same
// call — mirrors `dbhelm remote init [--url] [--name]`.
func (a *App) InitRemote(connectionName, database, url, name string) error {
	scopeDir, err := snapshot.ScopeDir(connectionName, database)
	if err != nil {
		return err
	}
	backend, err := snapshot.OpenBackendForRemote(scopeDir)
	if err != nil {
		return err
	}
	backend.Close()
	if err := remote.Init(scopeDir); err != nil {
		return err
	}
	if url != "" {
		if name == "" {
			name = "origin"
		}
		if err := remote.AddRemote(scopeDir, name, url); err != nil {
			return err
		}
	}
	return nil
}

// PushRemote commits everything in a database's snapshot scope and pushes
// it to remoteName/branch (defaults "origin"/"main" if empty).
func (a *App) PushRemote(connectionName, database, remoteName, branch, message string) error {
	scopeDir, err := snapshot.ScopeDir(connectionName, database)
	if err != nil {
		return err
	}
	if !remote.IsInitialized(scopeDir) {
		return fmt.Errorf("not a git remote-sync scope yet — initialize it first")
	}
	if remoteName == "" {
		remoteName = "origin"
	}
	if branch == "" {
		branch = "main"
	}
	if message == "" {
		message = "dbhelm sync"
	}
	return remote.Push(scopeDir, remoteName, branch, message)
}

// PullRemote fetches and merges from remoteName/branch (defaults
// "origin"/"main" if empty).
func (a *App) PullRemote(connectionName, database, remoteName, branch string) error {
	scopeDir, err := snapshot.ScopeDir(connectionName, database)
	if err != nil {
		return err
	}
	if !remote.IsInitialized(scopeDir) {
		return fmt.Errorf("not a git remote-sync scope yet — initialize it first")
	}
	if remoteName == "" {
		remoteName = "origin"
	}
	if branch == "" {
		branch = "main"
	}
	return remote.Pull(scopeDir, remoteName, branch)
}

// CloneRemote clones an existing remote snapshot history into a database's
// scope directory, checking out branch (default "main" if empty).
func (a *App) CloneRemote(connectionName, database, url, branch string) error {
	scopeDir, err := snapshot.ScopeDir(connectionName, database)
	if err != nil {
		return err
	}
	if branch == "" {
		branch = "main"
	}
	return remote.Clone(url, scopeDir, branch)
}
