// Package depmanager detects the external tools mongobak depends on
// (currently the MongoDB Database Tools) and offers manual or automatic
// installation. It's shared by the CLI (doctor), the TUI's startup check,
// and — eventually — the desktop app's dependency modal, so all three
// present identical detection and install behavior.
package depmanager

import (
	"fmt"
	"strings"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/mongotools"
)

// Dependency describes one external tool mongobak needs.
type Dependency struct {
	Name        string // binary name, e.g. "mongodump"
	Description string
}

// Required lists every dependency mongobak's backup/restore commands need.
// (Snapshots talk to MongoDB directly via the Go driver and need none of these.)
var Required = []Dependency{
	{Name: "mongodump", Description: "MongoDB Database Tools — used by `mongobak backup`"},
	{Name: "mongorestore", Description: "MongoDB Database Tools — used by `mongobak restore`"},
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
