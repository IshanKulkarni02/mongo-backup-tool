package depmanager

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// releasesURL is the GitHub Releases page users land on when there's no
// package-manager install path available. Points at the current repo
// location — GitHub redirects automatically if it's ever renamed, so this
// stays correct either way.
const releasesURL = "https://github.com/IshanKulkarni02/mongo-backup-tool/releases/latest"

// DesktopAppStatus is the desktop app's detection result — a best-effort,
// per-OS installed-location check, not a package-manager query, since
// there's no single registry of "installed apps" across macOS/Windows/Linux
// the way there is for a PATH binary.
type DesktopAppStatus struct {
	Installed bool
	Path      string // the launchable path, when Installed
}

// CheckDesktopApp looks for the DBHelm desktop app in the usual per-OS
// install locations.
func CheckDesktopApp(ctx context.Context) DesktopAppStatus {
	switch runtime.GOOS {
	case "darwin":
		for _, dir := range []string{"/Applications", homeSubdir("Applications")} {
			p := filepath.Join(dir, "DBHelm.app")
			if pathExists(p) {
				return DesktopAppStatus{Installed: true, Path: p}
			}
		}
	case "windows":
		candidates := []string{
			filepath.Join(os.Getenv("ProgramFiles"), "DBHelm", "dbhelm.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "DBHelm", "dbhelm.exe"),
		}
		for _, p := range candidates {
			if p != "" && pathExists(p) {
				return DesktopAppStatus{Installed: true, Path: p}
			}
		}
	default: // linux and anything else
		if p, err := exec.LookPath("dbhelm-desktop"); err == nil {
			return DesktopAppStatus{Installed: true, Path: p}
		}
		for _, p := range []string{"/opt/dbhelm/dbhelm-desktop", homeSubdir(".local/share/dbhelm/dbhelm-desktop")} {
			if pathExists(p) {
				return DesktopAppStatus{Installed: true, Path: p}
			}
		}
	}
	return DesktopAppStatus{}
}

// LaunchDesktopApp opens the already-installed desktop app at path.
func LaunchDesktopApp(ctx context.Context, path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.CommandContext(ctx, "open", path).Run()
	case "windows":
		return exec.CommandContext(ctx, "cmd", "/c", "start", "", path).Run()
	default:
		return exec.CommandContext(ctx, path).Start()
	}
}

// AutoInstallDesktopApp gets the desktop app onto the system: a
// package-manager install where one exists (brew cask on macOS, winget on
// Windows), or opening the GitHub Releases page for the user to run the
// normal signed installer themselves otherwise. Deliberately never
// downloads and silently executes an installer binary itself — unlike
// AutoInstallOllama's install script, there's no single official one-line
// installer for this app to defer to, so a raw download-and-run here would
// be *this* code deciding to trust an installer.
func AutoInstallDesktopApp(ctx context.Context, onOutput func(line string)) error {
	switch runtime.GOOS {
	case "darwin":
		if brew, err := exec.LookPath("brew"); err == nil {
			if err := runStreamed(ctx, brew, []string{"install", "--cask", "dbhelm"}, onOutput); err == nil {
				return nil
			}
			onOutput("Homebrew cask install didn't succeed — opening the release page instead.")
		}
	case "windows":
		if winget, err := exec.LookPath("winget"); err == nil {
			args := []string{"install", "--id", "DBHelm.DBHelm", "-e", "--accept-package-agreements", "--accept-source-agreements"}
			if err := runStreamed(ctx, winget, args, onOutput); err == nil {
				return nil
			}
			onOutput("winget install didn't succeed — opening the release page instead.")
		}
	}
	return openInBrowser(ctx, releasesURL)
}

func openInBrowser(ctx context.Context, url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url)
	case "windows":
		cmd = exec.CommandContext(ctx, "cmd", "/c", "start", "", url)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("opening %s: %w (open it manually to download the desktop app)", url, err)
	}
	return nil
}

func homeSubdir(parts ...string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(append([]string{home}, parts...)...)
}

func pathExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}
