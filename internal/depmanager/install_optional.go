package depmanager

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

func optionalDepErr(name string, format string, args ...any) error {
	return fmt.Errorf(name+": "+format+" — see ManualInstructions for a fallback", args...)
}

// AutoInstallOptional installs one of Optional's dependencies (currently
// "git" or "git-lfs") via the OS's package manager. Always an explicit,
// user-initiated action — never run silently — matching AutoInstall's
// contract for the required MongoDB tools.
//
// Unlike AutoInstall (which is darwin/windows-only, since
// mongodb-database-tools' packaging varies too much across Linux distros to
// automate safely), git-lfs's package name is "git-lfs" on essentially
// every mainstream distro's default repos (Debian/Ubuntu's apt, Fedora/RHEL's
// dnf/yum), so Linux is included here for that one dependency specifically —
// this is a narrower, better-grounded claim than "automate any Linux
// dependency," not a reversal of that earlier reasoning.
func AutoInstallOptional(ctx context.Context, name string, onOutput func(line string)) error {
	switch name {
	case "git":
		return optionalDepErr(name, "installing git itself isn't automated — it's a prerequisite most systems already have (Xcode Command Line Tools on macOS, base install on most Linux distros, Git for Windows); install it from https://git-scm.com/downloads if it's genuinely missing")
	case "git-lfs":
		return autoInstallGitLFS(ctx, onOutput)
	default:
		return fmt.Errorf("%q is not an optional dependency this function knows how to install", name)
	}
}

func autoInstallGitLFS(ctx context.Context, onOutput func(string)) error {
	switch runtime.GOOS {
	case "darwin":
		brew, err := exec.LookPath("brew")
		if err != nil {
			return optionalDepErr("git-lfs", "homebrew isn't installed (brew not found on PATH)")
		}
		if err := runStreamed(ctx, brew, []string{"install", "git-lfs"}, onOutput); err != nil {
			return optionalDepErr("git-lfs", "brew install failed: %w", err)
		}
		return nil
	case "windows":
		winget, err := exec.LookPath("winget")
		if err != nil {
			return optionalDepErr("git-lfs", "winget isn't available on this system")
		}
		args := []string{"install", "--id", "GitHub.GitLFS", "-e", "--accept-package-agreements", "--accept-source-agreements"}
		if err := runStreamed(ctx, winget, args, onOutput); err != nil {
			return optionalDepErr("git-lfs", "winget install failed: %w", err)
		}
		return nil
	case "linux":
		if apt, err := exec.LookPath("apt-get"); err == nil {
			if err := runStreamed(ctx, apt, []string{"install", "-y", "git-lfs"}, onOutput); err != nil {
				return optionalDepErr("git-lfs", "apt-get install failed (may need to be run as root/sudo): %w", err)
			}
			return nil
		}
		if dnf, err := exec.LookPath("dnf"); err == nil {
			if err := runStreamed(ctx, dnf, []string{"install", "-y", "git-lfs"}, onOutput); err != nil {
				return optionalDepErr("git-lfs", "dnf install failed (may need to be run as root/sudo): %w", err)
			}
			return nil
		}
		if yum, err := exec.LookPath("yum"); err == nil {
			if err := runStreamed(ctx, yum, []string{"install", "-y", "git-lfs"}, onOutput); err != nil {
				return optionalDepErr("git-lfs", "yum install failed (may need to be run as root/sudo): %w", err)
			}
			return nil
		}
		return optionalDepErr("git-lfs", "no supported package manager found (looked for apt-get, dnf, yum)")
	default:
		return optionalDepErr("git-lfs", "automatic install isn't supported on %s", runtime.GOOS)
	}
}
