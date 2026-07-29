package testmongod

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLooksLikePortConflict is a regression test for #42: Start needs to
// tell a genuine port-bind race (worth retrying with a fresh port) apart
// from an unrelated startup failure (where retrying would just waste
// time), based on mongod's own log output.
func TestLooksLikePortConflict(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"address already in use", "Failed to unlink socket file\nAddress already in use\n", true},
		{"case insensitive", "ADDRESS ALREADY IN USE", true},
		{"failed to set up listener", "Failed to set up listener: SocketException", true},
		{"failed to set up sockets", "Failed to set up sockets during startup.", true},
		{"error binding to port", "Error binding to port 27017", true},
		{"unrelated crash", "Fatal assertion 12345 at src/mongo/db/storage/wiredtiger.cpp", false},
		{"empty log", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "mongod.log")
			if err := os.WriteFile(logPath, []byte(c.content), 0o644); err != nil {
				t.Fatalf("writing log: %v", err)
			}
			if got := looksLikePortConflict(logPath); got != c.want {
				t.Errorf("looksLikePortConflict(%q) = %v, want %v", c.content, got, c.want)
			}
		})
	}
}

// TestLooksLikePortConflictMissingLog confirms a missing log file (e.g.
// the process never got far enough to create one) is treated as "not a
// recognizable port conflict" rather than erroring.
func TestLooksLikePortConflictMissingLog(t *testing.T) {
	if looksLikePortConflict(filepath.Join(t.TempDir(), "does-not-exist.log")) {
		t.Fatal("expected a missing log file to report false, not true")
	}
}

// TestWaitForMongodReturnsPromptlyOnProcessExit confirms waitForMongod
// doesn't wait out its full internal deadline once the process has
// already exited — it should notice via the exited channel and return
// right away with a descriptive error, which is what lets Start's retry
// loop react quickly to a lost port race instead of stalling for 20
// seconds per attempt.
func TestWaitForMongodReturnsPromptlyOnProcessExit(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "mongod.log")
	if err := os.WriteFile(logPath, []byte("Address already in use\n"), 0o644); err != nil {
		t.Fatalf("writing log: %v", err)
	}

	exited := make(chan struct{})
	close(exited) // simulates a process that already exited before mongod could bind

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	// A URI nothing is listening on, so a ping would otherwise have to
	// time out on its own — proving the exited-channel check is what
	// short-circuits this, not a lucky fast ping failure.
	err := waitForMongod(ctx, "mongodb://127.0.0.1:1/?directConnection=true&serverSelectionTimeoutMS=200", logPath, exited)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error when the process already exited")
	}
	if elapsed > 1*time.Second {
		t.Fatalf("waitForMongod took %s to notice the process had exited; expected it to return promptly", elapsed)
	}
}
