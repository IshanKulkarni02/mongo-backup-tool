package migrations

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func hasGit(t *testing.T) bool {
	t.Helper()
	_, err := exec.LookPath("git")
	return err == nil
}

func TestSaveWritesFileWithoutGitRepo(t *testing.T) {
	dir := t.TempDir()
	res, err := Save(dir, "add users email", "ALTER TABLE users ADD COLUMN email TEXT;", "")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if res.Committed {
		t.Fatal("expected Committed=false for a non-git folder")
	}
	data, err := os.ReadFile(res.FilePath)
	if err != nil {
		t.Fatalf("reading saved file: %v", err)
	}
	if string(data) != "ALTER TABLE users ADD COLUMN email TEXT;" {
		t.Fatalf("unexpected file content: %q", data)
	}
	if !strings.HasSuffix(res.FilePath, "_add_users_email.sql") {
		t.Fatalf("expected sanitized name in filename, got %q", res.FilePath)
	}
}

func TestSaveCreatesFolderIfMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "migrations")
	res, err := Save(dir, "init", "CREATE TABLE t (id INT);", "")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(res.FilePath); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

func TestSaveRejectsEmptySQL(t *testing.T) {
	if _, err := Save(t.TempDir(), "empty", "   ", ""); err == nil {
		t.Fatal("expected an error for an empty migration script")
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"add users email":  "add_users_email",
		"":                 "migration",
		"!!!":              "migration",
		"drop-old_table 2": "drop_old_table_2",
	}
	for in, want := range cases {
		if got := sanitizeName(in); got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSaveCommitsWhenFolderIsGitRepo(t *testing.T) {
	if !hasGit(t) {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initGitRepo(t, dir)

	res, err := Save(dir, "add index", "CREATE INDEX idx ON t(x);", "add idx migration")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !res.Committed {
		t.Fatalf("expected the migration to be committed, git output: %s", res.GitOutput)
	}

	out, err := exec.Command("git", "-C", dir, "log", "--oneline", "-1").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v (%s)", err, out)
	}
	if !strings.Contains(string(out), "add idx migration") {
		t.Fatalf("expected commit message in log, got: %s", out)
	}

	status, err := exec.Command("git", "-C", dir, "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	if strings.TrimSpace(string(status)) != "" {
		t.Fatalf("expected a clean working tree after commit, got: %s", status)
	}
}

func TestSaveDoesNotCommitWhenNotGitRepo(t *testing.T) {
	dir := t.TempDir() // no `git init`
	res, err := Save(dir, "plain", "SELECT 1;", "")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if res.Committed {
		t.Fatal("expected no commit for a plain (non-git) folder")
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	// An initial commit so the very first migration commit isn't the
	// repo's root commit (closer to how a real migrations folder behaves).
	seed := filepath.Join(dir, ".gitkeep")
	if err := os.WriteFile(seed, []byte(""), 0o644); err != nil {
		t.Fatalf("seeding .gitkeep: %v", err)
	}
	run("add", ".gitkeep")
	run("commit", "-q", "-m", "initial")
}
