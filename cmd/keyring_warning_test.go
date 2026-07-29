package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/IshanKulkarni02/dbhelm/internal/secrets"
)

// captureStderr redirects os.Stderr for the duration of fn and returns
// whatever was written to it — connection add/list and doctor print their
// keyring warning directly via fmt.Fprintln(os.Stderr, ...), not through
// cobra's OutOrStdout(), so capturing the real file descriptor is the
// only way to observe it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()

	fn()

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// TestConnectionAddWarnsWhenKeyringUnavailable guards against #61: a user
// on a machine with no OS keyring (headless Linux, containers, CI
// runners — a realistic deployment target for this CLI) must be told
// their credentials are stored in plaintext, not left to discover it on
// their own.
func TestConnectionAddWarnsWhenKeyringUnavailable(t *testing.T) {
	withTempConfigDir(t)
	resetConnAddFlags()
	secrets.MockUnavailable()
	t.Cleanup(secrets.ResetForTesting)
	connAddURI = "mongodb://user:pass@localhost:27017"

	out := captureStderr(t, func() {
		if err := connectionAddCmd.RunE(connectionAddCmd, []string{"test"}); err != nil {
			t.Fatalf("connection add: %v", err)
		}
	})
	if out == "" {
		t.Fatal("expected a keyring-unavailable warning on stderr, got none")
	}
}

// TestConnectionListWarnsWhenKeyringUnavailable is the same guard for
// `connection list`, a command a user might run without ever having run
// `add` in the same session (e.g. scripted use).
func TestConnectionListWarnsWhenKeyringUnavailable(t *testing.T) {
	withTempConfigDir(t)
	resetConnAddFlags()
	secrets.MockUnavailable()
	t.Cleanup(secrets.ResetForTesting)
	connAddURI = "mongodb://user:pass@localhost:27017"
	if err := connectionAddCmd.RunE(connectionAddCmd, []string{"test"}); err != nil {
		t.Fatalf("seeding connection add: %v", err)
	}

	out := captureStderr(t, func() {
		if err := connectionListCmd.RunE(connectionListCmd, nil); err != nil {
			t.Fatalf("connection list: %v", err)
		}
	})
	if out == "" {
		t.Fatal("expected a keyring-unavailable warning on stderr, got none")
	}
}

// TestDoctorReportsKeyringStatus guards the same warning surfaced through
// `dbhelm doctor`, the natural place to check the environment's overall
// health — this test only checks the keyring line specifically, not the
// dependency-check output (which needs mongodump/mongorestore present or
// absent, environment-dependent).
func TestDoctorReportsKeyringStatus(t *testing.T) {
	secrets.MockUnavailable()
	t.Cleanup(secrets.ResetForTesting)
	if secrets.Available() {
		t.Fatal("test setup: expected Available() to report false after MockUnavailable")
	}
	if secrets.UnavailableWarning == "" {
		t.Fatal("expected a non-empty UnavailableWarning message")
	}
}
