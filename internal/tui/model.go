// Package tui implements mongobak's interactive terminal UI: an arrow-key
// driven interface over the same internal/* core the CLI uses, so behavior
// never diverges between the two. Launched by running `mongobak` with no
// subcommand.
package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/IshanKulkarni02/mongo-backup-tool/internal/config"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/depmanager"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/snapshot"
	"github.com/IshanKulkarni02/mongo-backup-tool/internal/store"
)

type screen int

const (
	screenDeps screen = iota
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
	nameInput textinput.Model
	uriInput  textinput.Model
	addFocus  int
	addErr    string

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

func initialModel() Model {
	name := textinput.New()
	name.Placeholder = "e.g. local"
	name.Focus()
	name.CharLimit = 64

	uri := textinput.New()
	uri.Placeholder = "mongodb://localhost:27017"
	uri.CharLimit = 256

	dbIn := textinput.New()
	dbIn.Placeholder = "database name"
	dbIn.CharLimit = 128

	msgIn := textinput.New()
	msgIn.Placeholder = "message (optional)"
	msgIn.CharLimit = 256

	return Model{
		screen:       screenDeps,
		nameInput:    name,
		uriInput:     uri,
		dbInput:      dbIn,
		messageInput: msgIn,
	}
}

func (m Model) Init() tea.Cmd {
	return checkDepsCmd
}
