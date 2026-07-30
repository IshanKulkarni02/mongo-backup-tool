package filelock

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const (
	testTimeout  = 5 * time.Second
	testStaleAge = 2 * time.Minute
)

// TestWithSerializesConcurrentWrites proves the race this package closes:
// without the lock, N goroutines each doing load->append->save against
// the same file would lose updates — the last writer to save wins,
// silently dropping the others' appends. With With serializing the
// critical section, all N appends must survive.
func TestWithSerializesConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "data.txt")
	lockPath := filepath.Join(dir, "data.lock")

	const n = 30
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := With(lockPath, testTimeout, testStaleAge, "test data", func() error {
				existing, _ := os.ReadFile(dataPath)
				return os.WriteFile(dataPath, append(existing, byte('a'+i%26)), 0o644)
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("With: %v", err)
		}
	}

	data, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != n {
		t.Fatalf("file has %d bytes after %d concurrent locked appends, want %d (a lost update means the lock isn't actually serializing writes)", len(data), n, n)
	}
}

func TestWithCleansUpLockFileOnSuccess(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "data.lock")

	if err := With(lockPath, testTimeout, testStaleAge, "test data", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock file still present after With returned, stat err: %v", err)
	}
}

// TestWithReclaimsLockFromDeadPID is the regression test for the
// "permanently stale lock" gap: a lock file left behind by a process that
// no longer exists must be reclaimed quickly (via the PID-liveness
// check), not just eventually after the full retry timeout.
func TestWithReclaimsLockFromDeadPID(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "data.lock")

	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skipf("could not run a throwaway child process to get a dead PID: %v", err)
	}
	deadPID := cmd.Process.Pid

	content := fmt.Sprintf("pid=%d locked-at=%s\n", deadPID, time.Now().Format(time.RFC3339))
	if err := os.WriteFile(lockPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(lockPath, now, now); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	err := With(lockPath, testTimeout, testStaleAge, "test data", func() error { return nil })
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("With did not reclaim the dead-PID lock: %v", err)
	}
	if elapsed >= testTimeout {
		t.Errorf("With took %s to reclaim a dead-PID lock — expected fast reclaim well under the %s contention timeout", elapsed, testTimeout)
	}
}

func TestWithReclaimsAgedLock(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "data.lock")

	content := fmt.Sprintf("pid=%d locked-at=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	if err := os.WriteFile(lockPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * testStaleAge)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}

	if err := With(lockPath, testTimeout, testStaleAge, "test data", func() error { return nil }); err != nil {
		t.Fatalf("With did not reclaim the aged lock: %v", err)
	}
}

func TestWithDoesNotReclaimALiveRecentLock(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "data.lock")

	content := fmt.Sprintf("pid=%d locked-at=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	if err := os.WriteFile(lockPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	err := With(lockPath, testTimeout, testStaleAge, "test data", func() error {
		t.Fatal("fn ran, meaning the live/recent lock was wrongly reclaimed")
		return nil
	})
	if err == nil {
		t.Fatal("expected With to time out contending against a live, recent lock, got nil error")
	}
	os.Remove(lockPath)
}
