package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/IshanKulkarni02/dbhelm/internal/depmanager"
	"github.com/IshanKulkarni02/dbhelm/internal/engine"
)

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenLauncherChoice:
		return m.handleLauncherChoiceKey(msg)
	case screenDeps:
		return m.handleDepsKey(msg)
	case screenConnections:
		return m.handleConnectionsKey(msg)
	case screenAddConnection:
		return m.handleAddConnectionKey(msg)
	case screenDatabases:
		return m.handleDatabasesKey(msg)
	case screenMenu:
		return m.handleMenuKey(msg)
	case screenMessageInput:
		return m.handleMessageInputKey(msg)
	case screenList:
		return m.handleListKey(msg)
	case screenConfirm:
		return m.handleConfirmKey(msg)
	case screenResult:
		return m.handleResultKey(msg)
	}
	return m, nil
}

// --- launcher choice ---

func (m Model) handleLauncherChoiceKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.launcherCursor > 0 {
			m.launcherCursor--
		}
	case "down", "j":
		if m.launcherCursor < 1 {
			m.launcherCursor++
		}
	case "enter":
		return m.chooseLauncher()
	case "esc":
		// Quick-dismiss defaults to the lightweight option rather than
		// asking again next run.
		m.launcherCursor = 0
		return m.chooseLauncher()
	}
	return m, nil
}

func (m Model) chooseLauncher() (tea.Model, tea.Cmd) {
	if m.launcherCursor == 0 {
		m.screen = screenDeps
		return m, chooseTerminalCmd
	}
	// Read by desktopAppResolvedMsg's handler once chooseDesktopAppCmd
	// resolves, so the outcome screen knows where "continue" goes back to.
	m.resultBack = screenDeps
	m.screen = screenProgress
	m.progressText = "Opening the desktop app..."
	return m, chooseDesktopAppCmd
}

// --- deps ---

func (m Model) depChoices() []string {
	choices := []string{"View manual install instructions"}
	if depmanager.AutoInstallAvailable() {
		choices = append(choices, "Install automatically")
	}
	choices = append(choices, "Continue anyway (snapshots don't need these)")
	return choices
}

func (m Model) handleDepsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if depmanager.AllInstalled(m.depStatuses) {
		return m, nil
	}
	choices := m.depChoices()
	switch msg.Type {
	case tea.KeyUp:
		if m.depCursor > 0 {
			m.depCursor--
		}
	case tea.KeyDown:
		if m.depCursor < len(choices)-1 {
			m.depCursor++
		}
	case tea.KeyEnter:
		switch choices[m.depCursor] {
		case "Install automatically":
			m.depBusy = true
			m.depLog = []string{"Installing..."}
			return m, autoInstallDepsCmd
		case "Continue anyway (snapshots don't need these)":
			m.screen = screenConnections
			return m, loadConnectionsCmd
		default: // "View manual install instructions"
			m.depShowManual = true
		}
	case tea.KeyEsc:
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

// --- connections ---

func (m Model) handleConnectionsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.connCursor > 0 {
			m.connCursor--
		}
	case "down", "j":
		if m.connCursor < len(m.connections)-1 {
			m.connCursor++
		}
	case "a":
		m.screen = screenAddConnection
		m.addFocus = 0
		m.addErr = ""
		m.nameInput.SetValue("")
		m.uriInput.SetValue("")
		m.engineInput.SetValue("")
		m.nameInput.Focus()
		m.uriInput.Blur()
		m.engineInput.Blur()
	case "enter":
		if len(m.connections) == 0 {
			return m, nil
		}
		m.connection = m.connections[m.connCursor]
		m.screen = screenDatabases
		m.databases = nil
		m.dbCursor = 0
		m.dbErr = ""
		m.dbTyping = false
		return m, loadDatabasesCmd(m.connection.URI)
	case "esc", "q":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

// --- add connection ---

func (m Model) handleAddConnectionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.screen = screenConnections
		return m, nil
	case tea.KeyTab, tea.KeyDown:
		m.addFocus = (m.addFocus + 1) % 3
		m.syncAddFocus()
		return m, nil
	case tea.KeyShiftTab, tea.KeyUp:
		m.addFocus = (m.addFocus + 2) % 3
		m.syncAddFocus()
		return m, nil
	case tea.KeyEnter:
		name := m.nameInput.Value()
		uri := m.uriInput.Value()
		if name == "" || uri == "" {
			m.addErr = "both a name and a URI are required"
			return m, nil
		}
		engineID := m.engineInput.Value()
		if engineID != "" {
			if _, err := engine.Lookup(engineID); err != nil {
				m.addErr = err.Error()
				return m, nil
			}
		}
		m.addErr = ""
		// Move off screenAddConnection immediately, before the save
		// completes — matching handleMessageInputKey's pattern for every
		// other async action in this TUI. Left at screenAddConnection, a
		// key-repeat or a fast double Enter before connectionSavedMsg
		// arrives would dispatch saveConnectionCmd twice concurrently,
		// each running its own unsynchronized config.Load-mutate-Save
		// against the same config.json — a lost-update race.
		// connectionSavedMsg's handler routes back to screenAddConnection
		// on error so the form (and addErr) are still there to fix and
		// retry.
		m.screen = screenProgress
		m.progressText = "Saving connection..."
		return m, saveConnectionCmd(name, uri, engineID)
	}

	var cmd tea.Cmd
	switch m.addFocus {
	case 0:
		m.nameInput, cmd = m.nameInput.Update(msg)
	case 1:
		m.uriInput, cmd = m.uriInput.Update(msg)
	default:
		m.engineInput, cmd = m.engineInput.Update(msg)
	}
	return m, cmd
}

func (m *Model) syncAddFocus() {
	m.nameInput.Blur()
	m.uriInput.Blur()
	m.engineInput.Blur()
	switch m.addFocus {
	case 0:
		m.nameInput.Focus()
	case 1:
		m.uriInput.Focus()
	default:
		m.engineInput.Focus()
	}
}

// --- databases ---

func (m Model) handleDatabasesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.dbTyping {
		switch msg.Type {
		case tea.KeyEnter:
			name := m.dbInput.Value()
			if name == "" {
				return m, nil
			}
			m.database = name
			m.screen = screenMenu
			m.menuCursor = 0
			m.menuItems = buildMenuItems()
			return m, nil
		case tea.KeyEsc:
			m.dbTyping = false
			return m, nil
		}
		var cmd tea.Cmd
		m.dbInput, cmd = m.dbInput.Update(msg)
		return m, cmd
	}

	total := len(m.databases) + 1 // +1 for "type a database name"
	switch msg.String() {
	case "up", "k":
		if m.dbCursor > 0 {
			m.dbCursor--
		}
	case "down", "j":
		if m.dbCursor < total-1 {
			m.dbCursor++
		}
	case "enter":
		if m.dbCursor == len(m.databases) {
			m.dbTyping = true
			m.dbInput.SetValue("")
			m.dbInput.Focus()
			return m, nil
		}
		if m.dbCursor < len(m.databases) {
			m.database = m.databases[m.dbCursor]
			m.screen = screenMenu
			m.menuCursor = 0
			m.menuItems = buildMenuItems()
		}
	case "esc":
		m.screen = screenConnections
	}
	return m, nil
}

// --- menu ---

func buildMenuItems() []menuItem {
	return []menuItem{
		{"Snapshot: create", actionSnapshotCreate},
		{"Snapshot: history", actionSnapshotLog},
		{"Snapshot: diff vs. live", actionSnapshotDiffLive},
		{"Snapshot: restore", actionSnapshotRestore},
		{"Backup: create", actionBackupCreate},
		{"Backup: list", actionBackupList},
		{"Backup: restore", actionBackupRestore},
		{"Open desktop app (SQL, AI, dashboards, and more)", actionOpenDesktopApp},
	}
}

func (m Model) handleMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.menuCursor > 0 {
			m.menuCursor--
		}
	case "down", "j":
		if m.menuCursor < len(m.menuItems)-1 {
			m.menuCursor++
		}
	case "enter":
		return m.runMenuAction(m.menuItems[m.menuCursor].action)
	case "esc":
		m.screen = screenDatabases
	}
	return m, nil
}

func (m Model) runMenuAction(action menuAction) (tea.Model, tea.Cmd) {
	switch action {
	case actionSnapshotCreate:
		m.pendingAction = action
		m.screen = screenMessageInput
		m.messageInput.SetValue("")
		m.messageInput.Focus()
		return m, nil

	case actionSnapshotLog:
		m.screen = screenList
		m.listPurpose = listPurposeSnapshotView
		m.listErr = ""
		m.snapshots = nil
		m.listCursor = 0
		return m, loadSnapshotsCmd(m.connection.Name, m.database)

	case actionSnapshotDiffLive:
		m.screen = screenList
		m.listPurpose = listPurposeSnapshotDiffLive
		m.listErr = ""
		m.snapshots = nil
		m.listCursor = 0
		return m, loadSnapshotsCmd(m.connection.Name, m.database)

	case actionSnapshotRestore:
		m.screen = screenList
		m.listPurpose = listPurposeSnapshotRestore
		m.listErr = ""
		m.snapshots = nil
		m.listCursor = 0
		return m, loadSnapshotsCmd(m.connection.Name, m.database)

	case actionBackupCreate:
		m.screen = screenProgress
		m.progressText = "Backing up..."
		return m, createBackupCmd(m.connection.Name, m.connection.URI, m.database)

	case actionBackupList:
		m.screen = screenList
		m.listPurpose = listPurposeBackupView
		m.listErr = ""
		m.backups = nil
		m.listCursor = 0
		return m, loadBackupsCmd

	case actionBackupRestore:
		m.screen = screenList
		m.listPurpose = listPurposeBackupRestore
		m.listErr = ""
		m.backups = nil
		m.listCursor = 0
		return m, loadBackupsCmd

	case actionOpenDesktopApp:
		m.resultBack = screenMenu
		m.screen = screenProgress
		m.progressText = "Opening the desktop app..."
		return m, chooseDesktopAppCmd
	}
	return m, nil
}

// --- message input (snapshot create) ---

func (m Model) handleMessageInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		message := m.messageInput.Value()
		m.screen = screenProgress
		m.progressText = "Taking snapshot..."
		return m, createSnapshotCmd(m.connection, m.database, message)
	case tea.KeyEsc:
		m.screen = screenMenu
		return m, nil
	}
	var cmd tea.Cmd
	m.messageInput, cmd = m.messageInput.Update(msg)
	return m, cmd
}

// --- generic list (snapshots or backups) ---

func (m Model) listLen() int {
	switch m.listPurpose {
	case listPurposeBackupView, listPurposeBackupRestore:
		return len(m.backups)
	default:
		return len(m.snapshots)
	}
}

func (m Model) listIsViewOnly() bool {
	return m.listPurpose == listPurposeSnapshotView || m.listPurpose == listPurposeBackupView
}

func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := m.listLen()
	switch msg.String() {
	case "up", "k":
		if m.listCursor > 0 {
			m.listCursor--
		}
	case "down", "j":
		if m.listCursor < n-1 {
			m.listCursor++
		}
	case "enter":
		if n == 0 || m.listIsViewOnly() {
			return m, nil
		}
		return m.selectListItem()
	case "esc":
		m.screen = screenMenu
	}
	return m, nil
}

func (m Model) selectListItem() (tea.Model, tea.Cmd) {
	switch m.listPurpose {
	case listPurposeSnapshotDiffLive:
		id := m.snapshots[m.listCursor].ID
		m.screen = screenProgress
		m.progressText = "Diffing against live database..."
		return m, diffLiveCmd(m.connection, m.database, id)

	case listPurposeSnapshotRestore:
		id := m.snapshots[m.listCursor].ID
		m.confirmPrompt = fmt.Sprintf("Restore snapshot %s into %s/%s in place?\nA safety snapshot of the current state is taken automatically first.", shortID(id), m.connection.Name, m.database)
		m.confirmYesMsg = restoreSnapshotCmd(m.connection, m.database, id)
		m.confirmNoScreen = screenList
		m.screen = screenConfirm
		return m, nil

	case listPurposeBackupRestore:
		id := m.backups[m.listCursor].ID
		m.confirmPrompt = fmt.Sprintf("Restore backup %s into %s in place? This overwrites existing data\nand cannot be automatically undone.", shortID(id), m.connection.Name)
		m.confirmYesMsg = restoreBackupCmd(m.connection.Name, m.connection.URI, id)
		m.confirmNoScreen = screenList
		m.screen = screenConfirm
		return m, nil
	}
	return m, nil
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// --- confirm ---

func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.screen = screenProgress
		m.progressText = "Working..."
		return m, m.confirmYesMsg
	case "n", "N", "esc":
		m.screen = m.confirmNoScreen
	}
	return m, nil
}

// --- result ---

func (m Model) handleResultKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter, tea.KeyEsc:
		m.screen = m.resultBack
		if m.resultBack == screenDeps {
			// Reached from the desktop-app chooser (see
			// chooseDesktopAppCmd) — the dep check never ran yet since
			// Init() skips it for screenLauncherChoice.
			return m, checkDepsCmd
		}
	}
	return m, nil
}
