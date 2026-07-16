// Package snapshot implements git-like version control for MongoDB
// databases: content-addressed document storage, snapshots ("commits") with
// history, diffing between snapshots, and restore/rollback.
package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/config"
)

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func sanitize(s string) string {
	return unsafeChars.ReplaceAllString(s, "_")
}

// scopeDir returns the directory holding all snapshot data for one
// connection+database pair, creating it if needed. Each connection+database
// gets its own object store and manifest index, which keeps garbage
// collection cheap (no cross-database reference scanning).
func scopeDir(connection, database string) (string, error) {
	base, err := config.Dir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "snapshots", fmt.Sprintf("%s__%s", sanitize(connection), sanitize(database)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating snapshot scope directory %s: %w", dir, err)
	}
	return dir, nil
}

func objectsDir(scope string) string   { return filepath.Join(scope, "objects") }
func manifestsDir(scope string) string { return filepath.Join(scope, "manifests") }
func indexPath(scope string) string    { return filepath.Join(scope, "index.json") }

// allScopeDirs lists every existing connection+database scope directory,
// used by commands that operate across scopes (e.g. a global log, though
// most commands are scoped to one connection+database).
func allScopeDirs() ([]string, error) {
	base, err := config.Dir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(base, "snapshots")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(root, e.Name()))
		}
	}
	return dirs, nil
}
