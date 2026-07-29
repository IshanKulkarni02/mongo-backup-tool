// Package depmanager detects the external tools mongobak depends on
// (currently the MongoDB Database Tools) and offers manual or automatic
// installation. It's shared by the CLI (doctor), the TUI's startup check,
// and — eventually — the desktop app's dependency modal, so all three
// present identical detection and install behavior.
package depmanager

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/mongotools"
)

// Dependency describes one external tool mongobak needs.
type Dependency struct {
	Name        string // binary name, e.g. "mongodump"
	Description string
}

// Required lists dependencies mongobak's backup/restore commands cannot
// function without. (Snapshots talk to MongoDB directly via the Go driver
// and need none of these.)
var Required = []Dependency{
	{Name: "mongodump", Description: "MongoDB Database Tools — used by `mongobak backup`"},
	{Name: "mongorestore", Description: "MongoDB Database Tools — used by `mongobak restore`"},
}

// Optional lists dependencies only one specific feature needs — mongobak's
// core backup/snapshot workflows work fully without them; only that one
// feature (remote sync) is affected if they're missing. Kept separate from
// Required so callers (doctor, the TUI's startup check, the desktop
// dependency modal) can present "this blocks you" versus "this unlocks an
// extra feature" distinctly rather than treating every gap as equally
// urgent.
var Optional = []Dependency{
	{Name: "git", Description: "Git — used by `mongobak remote` to sync a database's snapshot history"},
	{Name: "git-lfs", Description: "Git LFS — used by `mongobak remote` to track snapshot object content without bloating the repo"},
}

// Status is one dependency's detection result.
type Status struct {
	Dependency Dependency
	Installed  bool
	Path       string // resolved binary path, if installed
	Version    string // first line of `--version` output, if installed
}

// Check detects every required dependency.
func Check() []Status {
	out := make([]Status, len(Required))
	for i, d := range Required {
		out[i] = checkOne(d)
	}
	return out
}

// CheckOptional detects every optional (feature-specific, not
// core-blocking) dependency — currently git and git-lfs, needed only for
// `mongobak remote`.
func CheckOptional() []Status {
	out := make([]Status, len(Optional))
	for i, d := range Optional {
		out[i] = checkOptionalOne(d)
	}
	return out
}

func checkOptionalOne(d Dependency) Status {
	path, err := exec.LookPath(d.Name)
	if err != nil {
		return Status{Dependency: d, Installed: false}
	}
	out, _ := exec.Command(path, "version").CombinedOutput()
	if len(out) == 0 {
		// git itself takes --version, not version; git-lfs takes version.
		out, _ = exec.Command(path, "--version").CombinedOutput()
	}
	return Status{Dependency: d, Installed: true, Path: path, Version: firstLine(string(out))}
}

// AllInstalled reports whether every required dependency was found.
func AllInstalled(statuses []Status) bool {
	for _, s := range statuses {
		if !s.Installed {
			return false
		}
	}
	return true
}

// Missing filters statuses down to just the not-installed ones.
func Missing(statuses []Status) []Status {
	var out []Status
	for _, s := range statuses {
		if !s.Installed {
			out = append(out, s)
		}
	}
	return out
}

func checkOne(d Dependency) Status {
	path, err := mongotools.Find(d.Name)
	if err != nil {
		return Status{Dependency: d, Installed: false}
	}
	version, _ := mongotools.Version(d.Name)
	return Status{Dependency: d, Installed: true, Path: path, Version: firstLine(version)}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// mongoDBToolsErr is returned by AutoInstall when it can't proceed; the
// message always ends by pointing at ManualInstructions as the fallback.
func mongoDBToolsErr(format string, args ...any) error {
	return fmt.Errorf(format+" — see ManualInstructions for a fallback", args...)
}
