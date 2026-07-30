// Package tui implements dbhelm's interactive terminal UI: an arrow-key
// driven interface over the same internal/* core the CLI uses, so behavior
// never diverges between the two. Launched by running `dbhelm` with no
// subcommand.
package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/IshanKulkarni02/dbhelm/internal/config"
	"github.com/IshanKulkarni02/dbhelm/internal/depmanager"
	"github.com/IshanKulkarni02/dbhelm/internal/snapshot"
	"github.com/IshanKulkarni02/dbhelm/internal/store"
)

type screen int

const (
	screenLauncherChoice screen = iota
	screenDeps
	screenConnections
	screenAddConnection
	screenDatabases
	screenMenu
	screenList // generic snapshot/backup list, purpose-driven
	screenMessageInput
	screenConfirm
	screenProgress
	screenResult
)

type menuAction int

const (
	actionSnapshotCreate menuAction = iota
	actionSnapshotLog
	actionSnapshotDiffLive
	actionSnapshotRestore
	actionBackupCreate
	actionBackupList
	actionBackupRestore
	actionOpenDesktopApp
)

type menuItem struct {
	label  string
	action menuAction
}

type listPurpose int

const (
	listPurposeSnapshotView listPurpose = iota
	listPurposeSnapshotRestore
	listPurposeSnapshotDiffLive
	listPurposeBackupView
	listPurposeBackupRestore
)

// Model is the TUI's single root state machine. A handful of screens with
// real branching logic (dependency check, add-connection form, the
// generic list) is small enough that one model with a `screen` field stays
// easier to follow than a deeply composed sub-model tree.
type Model struct {
	screen   screen
	width    int
	height   int
	quitting bool

	// screenLauncherChoice
	launcherCursor int

	// screenDeps
	depStatuses   []depmanager.Status
	depCursor     int
	depLog        []string
	depBusy       bool
	depDone       bool
	depShowManual bool

	// screenConnections
	connections []config.Connection
	connCursor  int
	connErr     string

	// screenAddConnection
	nameInput   textinput.Model
	uriInput    textinput.Model
	engineInput textinput.Model
	addFocus    int
	addErr      string

	// screenDatabases
	connection config.Connection
	databases  []string
	dbCursor   int
	dbInput    textinput.Model
	dbTyping   bool
	dbErr      string

	// picked scope
	database string

	// screenMenu
	menuCursor int
	menuItems  []menuItem

	// screenMessageInput (snapshot message)
	messageInput  textinput.Model
	pendingAction menuAction

	// screenList
	listPurpose listPurpose
	snapshots   []snapshot.Summary
	backups     []store.Backup
	listCursor  int
	listErr     string

	// screenConfirm
	confirmPrompt   string
	confirmYesMsg   tea.Cmd
	confirmNoScreen screen

	// screenProgress / screenResult
	progressText string
	resultLines  []string
	resultIsErr  bool
	resultBack   screen
}

func initialModel(forceChooser bool) Model {
	startScreen := screenDeps
	if forceChooser {
		startScreen = screenLauncherChoice
	} else if cfg, err := config.Load(); err == nil && cfg.Launcher.Choice == "" {
		// First run (or config predates the launcher choice) — ask once,
		// then remember it (see handleLauncherChoiceKey). Returning users
		// go straight to the dependency check, same as before this screen
		// existed.
		startScreen = screenLauncherChoice
	}

	name := textinput.New()
	name.Placeholder = "e.g. local"
	name.Focus()
	name.CharLimit = 64

	uri := textinput.New()
	uri.Placeholder = "mongodb://localhost:27017"
	uri.CharLimit = 256

	engineIn := textinput.New()
	engineIn.Placeholder = "mongodb (default), postgres, mysql, or sqlite"
	engineIn.CharLimit = 32

	dbIn := textinput.New()
	dbIn.Placeholder = "database name"
	dbIn.CharLimit = 128

	msgIn := textinput.New()
	msgIn.Placeholder = "message (optional)"
	msgIn.CharLimit = 256

	return Model{
		screen:       startScreen,
		nameInput:    name,
		uriInput:     uri,
		engineInput:  engineIn,
		dbInput:      dbIn,
		messageInput: msgIn,
	}
}

func (m Model) Init() tea.Cmd {
	if m.screen == screenLauncherChoice {
		return nil
	}
	return checkDepsCmd
}
