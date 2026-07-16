package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/depmanager"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.quitting = true
			return m, tea.Quit
		}
		return m.handleKey(msg)

	case depsCheckedMsg:
		m.depStatuses = msg.statuses
		if depmanager.AllInstalled(m.depStatuses) {
			m.depDone = true
			m.screen = screenConnections
			return m, loadConnectionsCmd
		}
		return m, nil

	case depsInstallLineMsg:
		m.depLog = append(m.depLog, msg.line)
		return m, nil

	case depsInstallDoneMsg:
		m.depBusy = false
		if msg.err != nil {
			m.depLog = append(m.depLog, "Install failed: "+msg.err.Error())
			return m, nil
		}
		m.depLog = append(m.depLog, "Done. Re-checking...")
		return m, checkDepsCmd

	case connectionsLoadedMsg:
		m.connections = msg.conns
		if msg.err != nil {
			m.connErr = msg.err.Error()
		}
		if m.connCursor >= len(m.connections) {
			m.connCursor = max(0, len(m.connections)-1)
		}
		return m, nil

	case connectionSavedMsg:
		if msg.err != nil {
			m.addErr = msg.err.Error()
			return m, nil
		}
		m.screen = screenConnections
		return m, loadConnectionsCmd

	case databasesLoadedMsg:
		m.databases = msg.dbs
		if msg.err != nil {
			m.dbErr = msg.err.Error()
		}
		return m, nil

	case snapshotsLoadedMsg:
		m.snapshots = msg.items
		m.listCursor = 0
		if msg.err != nil {
			m.listErr = msg.err.Error()
		}
		return m, nil

	case backupsLoadedMsg:
		m.backups = msg.items
		m.listCursor = 0
		if msg.err != nil {
			m.listErr = msg.err.Error()
		}
		return m, nil

	case actionDoneMsg:
		m.screen = screenResult
		m.resultBack = screenMenu
		m.resultIsErr = msg.err != nil
		if msg.err != nil {
			m.resultLines = []string{msg.err.Error()}
		} else {
			m.resultLines = msg.lines
		}
		return m, nil
	}

	return m, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
