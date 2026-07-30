package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateArgRejectsOptionLikeValues ensures a value that looks like a
// git option (e.g. a URL of "--upload-pack=some-command") is rejected
// before it ever reaches exec.Command, rather than being interpreted as a
// flag by whatever git subcommand receives it.
func TestValidateArgRejectsOptionLikeValues(t *testing.T) {
	cases := []string{
		"--upload-pack=touch /tmp/pwned",
		"-x",
		"",
	}
	for _, v := range cases {
		if err := validateArg("test arg", v); err == nil {
			t.Errorf("validateArg(%q) = nil, want error", v)
		}
	}
}

func TestValidateArgAllowsNormalValues(t *testing.T) {
	cases := []string{"origin", "main", "https://github.com/example/repo.git", "feature/foo"}
	for _, v := range cases {
		if err := validateArg("test arg", v); err != nil {
			t.Errorf("validateArg(%q) = %v, want nil", v, err)
		}
	}
}

// TestAddRemoteRejectsInjectionAttempt confirms the public entry points
// actually enforce validateArg, not just the helper in isolation.
func TestAddRemoteRejectsInjectionAttempt(t *testing.T) {
	requireGitAndLFS(t)
	scope := t.TempDir()
	runGit(t, scope, "init", "-q", "-b", "main")

	if err := AddRemote(scope, "--upload-pack=evil", "https://example.com/repo.git"); err == nil {
		t.Fatal("AddRemote accepted an option-like remote name")
	}
	if err := AddRemote(scope, "origin", "--upload-pack=evil"); err == nil {
		t.Fatal("AddRemote accepted an option-like URL")
	}
}

// TestAddRemoteStillWorksWithNormalValues guards against the "--"
// separators added alongside validateArg silently breaking the ordinary
// (non-malicious) case.
func TestAddRemoteStillWorksWithNormalValues(t *testing.T) {
	requireGitAndLFS(t)
	scope := t.TempDir()
	runGit(t, scope, "init", "-q", "-b", "main")

	bareDir := t.TempDir()
	runGit(t, bareDir, "init", "--bare", "-q", "--initial-branch=main")

	if err := AddRemote(scope, "origin", bareDir); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	out := runGit(t, scope, "remote", "-v")
	if !strings.Contains(out, bareDir) {
		t.Fatalf("remote -v output %q does not contain %q", out, bareDir)
	}
}

func TestCloneRejectsInjectionAttempt(t *testing.T) {
	requireGitAndLFS(t)
	target := filepath.Join(t.TempDir(), "scope")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Clone("--upload-pack=evil", target, "main"); err == nil {
		t.Fatal("Clone accepted an option-like URL")
	}
}
