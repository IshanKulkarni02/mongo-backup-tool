// Package migrations writes generated schema-migration scripts (see
// internal/schemadiff) to a folder on disk and, when that folder is a git
// working tree, commits them there. This is deliberately separate from
// internal/remote's git/git-LFS sync of Mongo snapshot history — that
// package tracks document-level snapshot content against a specific
// remote; this one just files a plain .sql script into wherever the user
// keeps their migrations, using git only if it's already there.
package migrations

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SaveResult reports where a migration script was written and whether it
// was committed to a git repository.
type SaveResult struct {
	FilePath  string `json:"filePath"`
	Committed bool   `json:"committed"`
	GitOutput string `json:"gitOutput,omitempty"`
}

// nowFunc is overridable in tests so filenames are deterministic.
var nowFunc = time.Now

// Save writes sql to a timestamped file inside folder (creating folder if
// it doesn't exist) and, if folder is inside a git working tree with git
// available on PATH, stages and commits it there. Committing is
// best-effort: a folder that isn't a git repo, or has no git binary, just
// gets the plain file — that's success, not an error, since "save it to a
// folder" was the request regardless of whether that folder happens to be
// version-controlled.
func Save(folder, name, sql, commitMessage string) (SaveResult, error) {
	if strings.TrimSpace(sql) == "" {
		return SaveResult{}, fmt.Errorf("nothing to save: migration script is empty")
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return SaveResult{}, fmt.Errorf("creating migrations folder: %w", err)
	}

	filename := fmt.Sprintf("%s_%s.sql", nowFunc().UTC().Format("20060102150405"), sanitizeName(name))
	path := filepath.Join(folder, filename)
	if err := os.WriteFile(path, []byte(sql), 0o644); err != nil {
		return SaveResult{}, fmt.Errorf("writing migration file: %w", err)
	}

	result := SaveResult{FilePath: path}
	if !isGitRepo(folder) {
		return result, nil
	}
	if _, err := exec.LookPath("git"); err != nil {
		return result, nil
	}
	if commitMessage == "" {
		commitMessage = "Add migration " + filename
	}
	if out, err := runGit(folder, "add", filename); err != nil {
		result.GitOutput = out
		return result, fmt.Errorf("git add %s: %w\n%s", filename, err, out)
	}
	out, err := runGit(folder, "commit", "-m", commitMessage)
	result.GitOutput = out
	if err != nil {
		return result, fmt.Errorf("git commit: %w\n%s", err, out)
	}
	result.Committed = true
	return result, nil
}

func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// sanitizeName reduces name to filename-safe characters, falling back to
// "migration" if nothing usable is left.
func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "migration"
	}
	return b.String()
}
