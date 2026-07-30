package tui

import tea "github.com/charmbracelet/bubbletea"

// Run launches the interactive terminal UI. Called when dbhelm is invoked
// with no subcommand.
func Run() error {
	return run(false)
}

// RunChooser launches the TUI straight at the terminal/desktop-app chooser,
// regardless of any previously saved choice — used by `dbhelm launcher` to
// let a user revisit the decision.
func RunChooser() error {
	return run(true)
}

func run(forceChooser bool) error {
	p := tea.NewProgram(initialModel(forceChooser), tea.WithAltScreen())
	programRef = p
	_, err := p.Run()
	return err
}
