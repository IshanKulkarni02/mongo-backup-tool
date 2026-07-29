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
		// The initial branch name is explicitly pinned to "main" rather than
		// left to the local git installation's configured default (which
		// varies — "master" on an unconfigured system, whatever
		// init.defaultBranch says otherwise). Push/Pull/Clone all default to
		// "main" too (see cmd/remote.go's --branch flags); if the local
		// repo's actual first branch ended up named something else, the
		// very first `mongobak remote push` would fail outright with "src
		// refspec main does not match any" before any content ever reached
		// the remote.
		if _, err := run(scope, git, "init", "-b", "main"); err != nil {
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

	// identity.json (see internal/snapshot's scopeDir) records which
	// connection+database *this machine's* copy of the scope belongs to —
	// it must never be pushed, since cloning the same remote under a
	// different local connection name (a real, supported case) would
	// otherwise overwrite the correct local identity with the source
	// machine's.
	ignorePath := filepath.Join(scope, ".gitignore")
	ignoreLine := "identity.json\n"
	if existing, err := os.ReadFile(ignorePath); err != nil || string(existing) != ignoreLine {
		if err := os.WriteFile(ignorePath, []byte(ignoreLine), 0o644); err != nil {
			return fmt.Errorf("writing .gitignore: %w", err)
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
// git requires the target directory to not exist, or be genuinely empty —
// no exceptions for gitignored or otherwise-expected files. Every caller
// resolves scope via snapshot.ScopeDir first, which — as a side effect of
// merely looking the path up — creates the directory and writes
// identity.json (recording the connection+database this scope belongs to,
// so a directory-name collision can never be mistaken for the wrong scope).
// That one expected file is removed before cloning (it's never part of the
// pushed history — see Init's .gitignore — so nothing is lost, and a normal
// subsequent scope lookup re-creates it); anything else in the directory
// still means "already has content" and is rejected.
func Clone(url, scope, branch string) error {
	git, err := findGit()
	if err != nil {
		return err
	}
	if entries, err := os.ReadDir(scope); err == nil {
		for _, e := range entries {
			if e.Name() != "identity.json" {
				return fmt.Errorf("%s already has content in it", scope)
			}
		}
		if err := os.Remove(filepath.Join(scope, "identity.json")); err != nil && !os.IsNotExist(err) {
			return err
		}
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
