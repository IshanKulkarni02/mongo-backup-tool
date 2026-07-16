package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorAccent = lipgloss.Color("39")
	colorMuted  = lipgloss.Color("240")
	colorError  = lipgloss.Color("203")
	colorOK     = lipgloss.Color("42")

	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	selectedStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	mutedStyle    = lipgloss.NewStyle().Foreground(colorMuted)
	errorStyle    = lipgloss.NewStyle().Foreground(colorError)
	okStyle       = lipgloss.NewStyle().Foreground(colorOK)
	helpStyle     = lipgloss.NewStyle().Foreground(colorMuted)
	labelStyle    = lipgloss.NewStyle().Bold(true)
)

func cursorPrefix(selected bool) string {
	if selected {
		return selectedStyle.Render("> ")
	}
	return "  "
}
