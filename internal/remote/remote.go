// Package remote implements pushing/pulling a snapshot scope's history to a
// Git remote (e.g. GitHub), with Git LFS tracking the compressed object
// blobs so the repository doesn't bloat or hit GitHub's file-size limits.
//
// This only works with the fs storage backend (see internal/snapshot):
// LFS needs individually addressable blob files, which is exactly what
// that backend's one-file-per-hash layout provides. The bbolt backend is a
// single opaque database file that git would treat as 100%-changed on
// every commit, so it's not compatible with this package.
package remote

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func findGit() (string, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("git not found on PATH — install it to use remote sync")
	}
	return path, nil
}

func findGitLFS() (string, error) {
	path, err := exec.LookPath("git-lfs")
	if err != nil {
		return "", fmt.Errorf("git-lfs not found on PATH — install it from https://git-lfs.com to use remote sync")
	}
	return path, nil
}

func run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// IsInitialized reports whether a scope directory already has a Git repository.
func IsInitialized(scope string) bool {
	_, err := os.Stat(filepath.Join(scope, ".git"))
	return err == nil
}

// Init sets up a scope directory as a Git repository with Git LFS tracking
// the compressed object blobs. Safe to call on an already-initialized scope
// (no-op). Git LFS is required for this to be practical at any real scale —
// if it's not installed, Init still creates a plain git repo (so `doctor`
// can report the gap) but returns an error naming what's missing.
func Init(scope string) error {
	git, err := findGit()
	if err != nil {
		return err
	}

	if !IsInitialized(scope) {
		if _, err := run(scope, git, "init"); err != nil {
			return fmt.Errorf("git init: %w", err)
		}
	}

	lfs, lfsErr := findGitLFS()
	if lfsErr != nil {
		return lfsErr
	}
	if _, err := run(scope, lfs, "install", "--local"); err != nil {
		return fmt.Errorf("git lfs install: %w", err)
	}

	attrPath := filepath.Join(scope, ".gitattributes")
	attrs := "objects/**/*.zst filter=lfs diff=lfs merge=lfs -text\n"
	if existing, err := os.ReadFile(attrPath); err != nil || string(existing) != attrs {
		if err := os.WriteFile(attrPath, []byte(attrs), 0o644); err != nil {
			return fmt.Errorf("writing .gitattributes: %w", err)
		}
	}

	return nil
}

// AddRemote adds a named git remote, or updates its URL if the name already exists.
func AddRemote(scope, name, url string) error {
	git, err := findGit()
	if err != nil {
		return err
	}
	if _, err := run(scope, git, "remote", "add", name, url); err != nil {
		if _, err := run(scope, git, "remote", "set-url", name, url); err != nil {
			return fmt.Errorf("setting remote %s: %w", name, err)
		}
	}
	return nil
}

// Push commits everything currently in the scope directory and pushes it to
// the given remote/branch.
func Push(scope, remoteName, branch, message string) error {
	git, err := findGit()
	if err != nil {
		return err
	}
	if _, err := run(scope, git, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if out, err := run(scope, git, "commit", "-m", message); err != nil && !strings.Contains(out, "nothing to commit") {
		return fmt.Errorf("git commit: %w", err)
	}
	if _, err := run(scope, git, "push", "-u", remoteName, branch); err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	return nil
}

// Pull fetches and merges from the given remote/branch.
func Pull(scope, remoteName, branch string) error {
	git, err := findGit()
	if err != nil {
		return err
	}
	if _, err := run(scope, git, "pull", remoteName, branch); err != nil {
		return fmt.Errorf("git pull: %w", err)
	}
	return nil
}

// Clone clones an existing remote snapshot history into a scope directory,
// checking out the given branch explicitly rather than relying on the
// remote's default HEAD — a freshly created bare or remote repo may still
// default to "master" (or have no default at all) even though mongobak
// always pushes to "main", which would otherwise clone an empty tree.
//
// git itself permits cloning into a directory that already exists as long
// as it's empty — which is exactly what happens here, since ScopeDir always
// creates the (empty) scope directory as a side effect of looking it up —
// so this only rejects a scope that already has content in it.
func Clone(url, scope, branch string) error {
	git, err := findGit()
	if err != nil {
		return err
	}
	if entries, err := os.ReadDir(scope); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s already has content in it", scope)
	}
	parent := filepath.Dir(scope)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if _, err := run(parent, git, "clone", "--branch", branch, url, scope); err != nil {
		return fmt.Errorf("git clone: %w", err)
	}

	// A plain `git clone` only materializes real LFS content (rather than
	// leaving pointer files in place) if LFS's smudge filter is already
	// registered globally on this machine, which isn't guaranteed. Register
	// it locally in the fresh clone and force-pull the real objects
	// regardless of that global state.
	if lfs, lfsErr := findGitLFS(); lfsErr == nil {
		if _, err := run(scope, lfs, "install", "--local"); err != nil {
			return fmt.Errorf("git lfs install: %w", err)
		}
		if _, err := run(scope, lfs, "pull"); err != nil {
			return fmt.Errorf("git lfs pull: %w", err)
		}
	}
	return nil
}
