package depmanager

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPathExists(t *testing.T) {
	if pathExists("") {
		t.Fatal("expected empty path to report false")
	}
	dir := t.TempDir()
	if pathExists(filepath.Join(dir, "nope")) {
		t.Fatal("expected nonexistent path to report false")
	}
	if !pathExists(dir) {
		t.Fatal("expected existing temp dir to report true")
	}
}

func TestHomeSubdir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := homeSubdir("Applications", "DBHelm.app")
	want := filepath.Join(home, "Applications", "DBHelm.app")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCheckDesktopAppNotInstalled(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("this check only exercises the darwin detection path")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	status := CheckDesktopApp(context.Background())
	if status.Installed {
		t.Fatalf("expected not installed in an empty fake $HOME, got %+v", status)
	}
}

func TestCheckDesktopAppFindsUserApplicationsBundle(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("this check only exercises the darwin detection path")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	bundle := filepath.Join(home, "Applications", "DBHelm.app")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("seeding fake app bundle: %v", err)
	}

	status := CheckDesktopApp(context.Background())
	if !status.Installed || status.Path != bundle {
		t.Fatalf("expected detection of %s, got %+v", bundle, status)
	}
}
