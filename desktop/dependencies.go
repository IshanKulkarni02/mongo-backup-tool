package main

import (
	"context"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/depmanager"
)

// DependencyStatus mirrors depmanager.Status with a JSON-friendly shape for
// the frontend.
type DependencyStatus struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
	Version     string `json:"version,omitempty"`
}

// CheckDependencies reports the status of every required external tool.
func (a *App) CheckDependencies() []DependencyStatus {
	statuses := depmanager.Check()
	out := make([]DependencyStatus, len(statuses))
	for i, s := range statuses {
		out[i] = DependencyStatus{
			Name:        s.Dependency.Name,
			Description: s.Dependency.Description,
			Installed:   s.Installed,
			Version:     s.Version,
		}
	}
	return out
}

// ManualInstallInstructions returns copy-pasteable install instructions for
// the current OS.
func (a *App) ManualInstallInstructions() []string {
	return depmanager.ManualInstructions()
}

// AutoInstallAvailable reports whether automatic install has a real
// implementation on this OS (macOS/Windows; Linux is manual-only).
func (a *App) AutoInstallAvailable() bool {
	return depmanager.AutoInstallAvailable()
}

// InstallDependencies starts an automatic install as a background job,
// streaming progress lines via "job:update" events (Result holds the final
// line list once done). Always an explicit, user-initiated action from the
// dependency modal — never run on startup without the user choosing it.
func (a *App) InstallDependencies() string {
	return a.jobs.run("deps-install", func() (any, error) {
		var lines []string
		err := depmanager.AutoInstall(context.Background(), func(line string) {
			lines = append(lines, line)
		})
		if err != nil {
			return nil, err
		}
		return lines, nil
	})
}
