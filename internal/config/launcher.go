package config

// LauncherSettings persists the user's first-run choice between the
// terminal TUI and the desktop app, so the interactive chooser only ever
// appears once per machine. Choice is "terminal", "desktop", or empty
// (never chosen yet).
type LauncherSettings struct {
	Choice string `json:"choice,omitempty"`
}
