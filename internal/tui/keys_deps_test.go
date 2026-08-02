package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/IshanKulkarni02/dbhelm/internal/depmanager"
)

// depsModelWithMissingDeps returns a Model on screenDeps with at least
// one dependency reported missing, so depChoices includes "Install
// automatically" and AllInstalled's early-return in handleDepsKey doesn't
// short-circuit before the busy check does.
func depsModelWithMissingDeps() Model {
	return Model{
		screen: screenDeps,
		depStatuses: []depmanager.Status{
			{Dependency: depmanager.Dependency{Name: "mongodump"}, Installed: false},
		},
	}
}

// TestHandleDepsKeyIgnoresInputWhileBusy is the regression test for #54:
// handleDepsKey never checked a busy flag before processing keys, so a
// user could navigate away from screenDeps mid-install (e.g. via
// "Continue anyway") and later get forcibly pulled back once the
// background install finished, or press Enter again to fire a second
// concurrent depmanager.AutoInstall(). Every key must now be a no-op
// while depBusy is true.
func TestHandleDepsKeyIgnoresInputWhileBusy(t *testing.T) {
	m := depsModelWithMissingDeps()
	m.depBusy = true
	m.depCursor = 0

	cases := []tea.KeyMsg{
		{Type: tea.KeyEnter},
		{Type: tea.KeyDown},
		{Type: tea.KeyUp},
		{Type: tea.KeyEsc},
	}
	for _, key := range cases {
		next, cmd := m.handleDepsKey(key)
		nm := next.(Model)
		if nm.screen != screenDeps {
			t.Fatalf("key %v changed screen away from screenDeps to %v while busy", key.Type, nm.screen)
		}
		if nm.depCursor != m.depCursor {
			t.Fatalf("key %v changed depCursor from %d to %d while busy", key.Type, m.depCursor, nm.depCursor)
		}
		if !nm.depBusy {
			t.Fatalf("key %v cleared depBusy on its own", key.Type)
		}
		if nm.quitting {
			t.Fatalf("key %v (esc) set quitting while busy — esc must be ignored, not treated as quit", key.Type)
		}
		if cmd != nil {
			t.Fatalf("key %v returned a non-nil cmd while busy (a second Enter must not re-launch autoInstallDepsCmd)", key.Type)
		}
	}
}

// TestHandleDepsKeyProcessesInputWhenNotBusy confirms the busy guard
// doesn't block ordinary navigation once no install is running — the
// common case must keep working.
func TestHandleDepsKeyProcessesInputWhenNotBusy(t *testing.T) {
	m := depsModelWithMissingDeps()
	m.depBusy = false
	m.depCursor = 0

	next, _ := m.handleDepsKey(tea.KeyMsg{Type: tea.KeyDown})
	nm := next.(Model)
	if nm.depCursor != 1 {
		t.Fatalf("expected depCursor to advance to 1 when not busy, got %d", nm.depCursor)
	}
}

// TestHandleDepsKeyEnterLaunchesInstallOnce confirms the ordinary
// "Install automatically" path still sets depBusy and returns a non-nil
// command the first time, so the busy guard above isn't masking a
// regression where it never gets set in the first place. Skipped where
// depmanager doesn't offer automatic install at all (its own OS check,
// not something this test should second-guess).
func TestHandleDepsKeyEnterLaunchesInstallOnce(t *testing.T) {
	if !depmanager.AutoInstallAvailable() {
		t.Skip("depmanager doesn't offer automatic install on this OS")
	}
	m := depsModelWithMissingDeps()
	choices := m.depChoices()
	idx := -1
	for i, c := range choices {
		if c == "Install automatically" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("depChoices() didn't include \"Install automatically\": %v", choices)
	}
	m.depCursor = idx

	next, cmd := m.handleDepsKey(tea.KeyMsg{Type: tea.KeyEnter})
	nm := next.(Model)
	if !nm.depBusy {
		t.Fatal("expected depBusy to be set after choosing Install automatically")
	}
	if cmd == nil {
		t.Fatal("expected a non-nil command to launch the install")
	}
}
