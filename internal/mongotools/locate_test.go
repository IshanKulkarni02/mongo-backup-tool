package mongotools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testToolBase = "dbhelmtesttool"

// withEmptyPATH clears PATH for the duration of the test so exec.LookPath
// can't accidentally find a real binary and short-circuit the fallback-dir
// search these tests are trying to exercise.
func withEmptyPATH(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", "")
}

func withFallbackDir(t *testing.T, dir string) {
	t.Helper()
	orig := fallbackDirs
	fallbackDirs = func() []string { return []string{dir} }
	t.Cleanup(func() { fallbackDirs = orig })
}

// TestFindSkipsNonExecutableFallbackFile is the regression test for #78:
// fileExists only checked !info.IsDir(), so a stray non-executable file
// matching the binary name in a fallback directory (e.g. downloaded
// without the executable bit set) was accepted as "found" — Find would
// return that path, and the caller's later exec.Command would fail with
// a confusing permission-denied error instead of Find's own clear
// "install the Database Tools" message.
func TestFindSkipsNonExecutableFallbackFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable-bit check is a no-op on windows (executability signaled by .exe extension)")
	}
	withEmptyPATH(t)
	t.Setenv("DBHELM_"+strings.ToUpper(testToolBase)+"_PATH", "")

	dir := t.TempDir()
	path := filepath.Join(dir, testToolBase)
	if err := os.WriteFile(path, []byte("not actually a binary"), 0o644); err != nil {
		t.Fatalf("seeding non-executable file: %v", err)
	}
	withFallbackDir(t, dir)

	_, err := Find(testToolBase)
	if err == nil {
		t.Fatal("expected Find to report not-found for a non-executable candidate, got success")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected the clear \"not found\" message, got: %v", err)
	}
}

// TestFindAcceptsExecutableFallbackFile confirms the ordinary case still
// works: a real executable file in a fallback directory is found.
func TestFindAcceptsExecutableFallbackFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fallback dirs on windows are versioned MongoDB install dirs, not a plain executable-bit scenario")
	}
	withEmptyPATH(t)
	t.Setenv("DBHELM_"+strings.ToUpper(testToolBase)+"_PATH", "")

	dir := t.TempDir()
	path := filepath.Join(dir, testToolBase)
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatalf("seeding executable file: %v", err)
	}
	withFallbackDir(t, dir)

	got, err := Find(testToolBase)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got != path {
		t.Fatalf("Find returned %q, want %q", got, path)
	}
}

// TestFindRejectsNonExecutableEnvVarOverride confirms the same
// executable-bit check applies to the DBHELM_*_PATH override, with an
// error message that distinguishes "not executable" from "does not
// exist" rather than reusing the same misleading message for both.
func TestFindRejectsNonExecutableEnvVarOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable-bit check is a no-op on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, testToolBase)
	if err := os.WriteFile(path, []byte("not actually a binary"), 0o644); err != nil {
		t.Fatalf("seeding non-executable file: %v", err)
	}
	t.Setenv("DBHELM_"+strings.ToUpper(testToolBase)+"_PATH", path)

	_, err := Find(testToolBase)
	if err == nil {
		t.Fatal("expected an error for a non-executable env-var override path")
	}
	if !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("expected a \"not executable\" message, got: %v", err)
	}
}

// TestFindAcceptsExecutableEnvVarOverride confirms the happy path for the
// env-var override still works.
func TestFindAcceptsExecutableEnvVarOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable-bit check is a no-op on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, testToolBase)
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatalf("seeding executable file: %v", err)
	}
	t.Setenv("DBHELM_"+strings.ToUpper(testToolBase)+"_PATH", path)

	got, err := Find(testToolBase)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got != path {
		t.Fatalf("Find returned %q, want %q", got, path)
	}
}
