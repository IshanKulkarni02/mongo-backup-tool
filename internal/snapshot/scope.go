// Package snapshot implements git-like version control for MongoDB
// databases: content-addressed document storage, snapshots ("commits") with
// history, diffing between snapshots, and restore/rollback.
package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// identityFile records a scope directory's true, unsanitized
// connection+database identity, so a directory name collision between two
// different identities (e.g. "prod/a" and "prod:a", which both sanitize to
// "prod_a") never causes one scope's data to be read as another's — the
// hash suffix in the directory name (see scopeDirName) already prevents the
// collision itself; this file lets that identity be verified and, if ever
// needed, recovered from the directory alone rather than reverse-engineered
// from a lossy sanitized name.
type identityFile struct {
	Connection string `json:"connection"`
	Database   string `json:"database"`
}

// scopeDirName derives a filesystem-safe, collision-resistant directory
// name for one connection+database pair. Sanitizing unsafe characters to
// "_" alone is lossy (e.g. "prod/a" and "prod:a" both become "prod_a"), so a
// short hash of the *original, unsanitized* identity is appended — the
// sanitized prefix keeps the directory human-readable, the hash suffix
// guarantees two different identities never collide regardless of what
// characters they contain.
func scopeDirName(connection, database string) string {
	sum := sha256.Sum256([]byte(connection + "\x00" + database))
	suffix := hex.EncodeToString(sum[:])[:10]
	return fmt.Sprintf("%s__%s__%s", sanitize(connection), sanitize(database), suffix)
}

// scopeDir returns the directory holding all snapshot data for one
// connection+database pair, creating it if needed. Each connection+database
// gets its own object store and manifest index, which keeps garbage
// collection cheap (no cross-database reference scanning).
//
// Existing scopes created before the collision-resistant naming scheme (no
// hash suffix, just sanitize(connection)+"__"+sanitize(database)) are
// migrated in place the first time they're accessed: if the old-style
// directory exists and the new-style one doesn't, it's renamed rather than
// starting the connection+database over with an empty history.
func scopeDir(connection, database string) (string, error) {
	base, err := config.Dir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(base, "snapshots")
	dir := filepath.Join(root, scopeDirName(connection, database))

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		legacy := filepath.Join(root, fmt.Sprintf("%s__%s", sanitize(connection), sanitize(database)))
		if _, legacyErr := os.Stat(legacy); legacyErr == nil {
			if err := os.Rename(legacy, dir); err != nil {
				return "", fmt.Errorf("migrating legacy snapshot scope directory %s to %s: %w", legacy, dir, err)
			}
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating snapshot scope directory %s: %w", dir, err)
	}
	if err := writeIdentityIfMissing(dir, connection, database); err != nil {
		return "", err
	}
	return dir, nil
}

func identityPath(scope string) string { return filepath.Join(scope, "identity.json") }

// writeIdentityIfMissing records the scope's true connection+database
// identity the first time it's created (or migrated from the legacy
// naming), so it can be verified or recovered later without depending on
// the (lossy) sanitized directory name.
func writeIdentityIfMissing(scope, connection, database string) error {
	path := identityPath(scope)
	if _, err := os.Stat(path); err == nil {
		return nil // already recorded (either at creation, or by a prior migration)
	}
	data, err := json.MarshalIndent(identityFile{Connection: connection, Database: database}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
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
