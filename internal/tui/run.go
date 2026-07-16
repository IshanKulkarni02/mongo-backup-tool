package tui

import tea "github.com/charmbracelet/bubbletea"

// Run launches the interactive terminal UI. Called when mongobak is invoked
// with no subcommand.
func Run() error {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	programRef = p
	_, err := p.Run()
	return err
}
