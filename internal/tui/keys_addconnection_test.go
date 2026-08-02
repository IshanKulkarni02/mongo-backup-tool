package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"

	"github.com/IshanKulkarni02/dbhelm/internal/secrets"
)

func addConnectionModel(name, uri string) Model {
	nameInput := textinput.New()
	nameInput.SetValue(name)
	uriInput := textinput.New()
	uriInput.SetValue(uri)
	engineInput := textinput.New()
	return Model{
		screen:      screenAddConnection,
		nameInput:   nameInput,
		uriInput:    uriInput,
		engineInput: engineInput,
	}
}

// TestHandleAddConnectionKeyEnterMovesOffFormImmediately is the
// regression test for #55: pressing Enter with valid input dispatched
// saveConnectionCmd but left m.screen at screenAddConnection, so a
// key-repeat or a fast double Enter before connectionSavedMsg arrived
// could dispatch a second concurrent saveConnectionCmd — each running
// its own unsynchronized config.Load-mutate-Save against the same
// config.json, a lost-update race. Enter must move off the form
// immediately, before the save completes.
func TestHandleAddConnectionKeyEnterMovesOffFormImmediately(t *testing.T) {
	m := addConnectionModel("local", "mongodb://localhost:27017")

	next, cmd := m.handleAddConnectionKey(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(Model)

	if nm.screen == screenAddConnection {
		t.Fatal("expected Enter to move off screenAddConnection immediately, but it stayed")
	}
	if nm.screen != screenProgress {
		t.Fatalf("expected screenProgress after Enter, got %v", nm.screen)
	}
	if cmd == nil {
		t.Fatal("expected a non-nil command to save the connection")
	}
}

// TestHandleKeyIgnoresSecondEnterWhileSaving confirms the actual
// double-submit protection end-to-end through the top-level handleKey
// dispatcher (not handleAddConnectionKey directly): once the first Enter
// has moved the model to screenProgress, handleKey routes a second Enter
// nowhere (screenProgress has no case in that dispatcher's switch), so
// no second saveConnectionCmd can be dispatched no matter how fast the
// key repeats.
func TestHandleKeyIgnoresSecondEnterWhileSaving(t *testing.T) {
	m := addConnectionModel("local", "mongodb://localhost:27017")

	first, cmd1 := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m1 := first.(Model)
	if m1.screen != screenProgress {
		t.Fatalf("expected screenProgress after first Enter, got %v", m1.screen)
	}
	if cmd1 == nil {
		t.Fatal("expected the first Enter to dispatch a save command")
	}

	second, cmd2 := m1.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := second.(Model)
	if m2.screen != screenProgress {
		t.Fatalf("expected to remain on screenProgress after a second Enter, got %v", m2.screen)
	}
	if cmd2 != nil {
		t.Fatal("expected the second Enter (while still saving) to dispatch no command at all")
	}
}

// TestConnectionSavedMsgRoutesBackToFormOnError confirms the form (and
// its error message) is reachable again after a failed save, since Enter
// now moves off screenAddConnection before the result is known.
func TestConnectionSavedMsgRoutesBackToFormOnError(t *testing.T) {
	m := addConnectionModel("local", "mongodb://localhost:27017")
	m.screen = screenProgress

	next, _ := m.Update(connectionSavedMsg{err: errTestSave})
	nm := next.(Model)

	if nm.screen != screenAddConnection {
		t.Fatalf("expected to return to screenAddConnection on save error, got %v", nm.screen)
	}
	if nm.addErr == "" {
		t.Fatal("expected addErr to be set after a save error")
	}
}

// TestConnectionSavedMsgGoesToConnectionsOnSuccess confirms the ordinary
// success path still moves on to screenConnections.
func TestConnectionSavedMsgGoesToConnectionsOnSuccess(t *testing.T) {
	m := addConnectionModel("local", "mongodb://localhost:27017")
	m.screen = screenProgress

	next, cmd := m.Update(connectionSavedMsg{})
	nm := next.(Model)

	if nm.screen != screenConnections {
		t.Fatalf("expected screenConnections after a successful save, got %v", nm.screen)
	}
	if cmd == nil {
		t.Fatal("expected loadConnectionsCmd to be dispatched after a successful save")
	}
}

// TestConnectionSavedMsgSetsWarningWithoutKeyring is the regression test
// for #61: the CLI already warns on `connection add`/`list` when no OS
// keyring is available, but the TUI's own add-connection flow persisted
// credentials in plaintext with no equivalent warning shown anywhere.
func TestConnectionSavedMsgSetsWarningWithoutKeyring(t *testing.T) {
	secrets.MockUnavailable()
	t.Cleanup(secrets.ResetForTesting)

	m := addConnectionModel("local", "mongodb://localhost:27017")
	m.screen = screenProgress

	next, _ := m.Update(connectionSavedMsg{})
	nm := next.(Model)

	if nm.connWarning == "" {
		t.Fatal("expected connWarning to be set after a successful save with no keyring available")
	}
}

// TestConnectionSavedMsgNoWarningWithKeyring confirms the warning is only
// shown when it's actually true — a successful save with a working
// keyring must not scare the user with a plaintext-storage warning.
func TestConnectionSavedMsgNoWarningWithKeyring(t *testing.T) {
	secrets.MockInit()
	t.Cleanup(secrets.ResetForTesting)

	m := addConnectionModel("local", "mongodb://localhost:27017")
	m.screen = screenProgress

	next, _ := m.Update(connectionSavedMsg{})
	nm := next.(Model)

	if nm.connWarning != "" {
		t.Fatalf("expected no connWarning with a working keyring, got %q", nm.connWarning)
	}
}

var errTestSave = &testSaveError{"boom"}

type testSaveError struct{ msg string }

func (e *testSaveError) Error() string { return e.msg }
