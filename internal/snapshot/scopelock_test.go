package snapshot

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestWithScopeLockSerializesConcurrentIndexWrites proves the race
// scopelock.go closes: without the lock, N goroutines each doing
// loadIndex -> append -> saveIndex against the same fs-backed scope (which,
// unlike bbolt, has no OpenBackend-level file lock at all) would lose
// updates — the last writer to save wins, silently dropping the others'
// appends. With withScopeLock serializing the critical section, all N
// appends must survive.
func TestWithScopeLockSerializesConcurrentIndexWrites(t *testing.T) {
	withTestScope(t)
	scope, err := scopeDir("lock-test", "lockdb")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveIndex(scope, &scopeIndex{}); err != nil {
		t.Fatal(err)
	}

	const n = 30
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := withScopeLock(scope, func() error {
				idx, err := loadIndex(scope)
				if err != nil {
					return err
				}
				idx.Snapshots = append(idx.Snapshots, Summary{ID: fmt.Sprintf("snap-%d", i)})
				return saveIndex(scope, idx)
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("withScopeLock: %v", err)
		}
	}

	final, err := loadIndex(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(final.Snapshots) != n {
		t.Fatalf("index has %d snapshots after %d concurrent locked appends, want %d (a lost update means the lock isn't actually serializing writes)", len(final.Snapshots), n, n)
	}
	seen := map[string]bool{}
	for _, s := range final.Snapshots {
		seen[s.ID] = true
	}
	if len(seen) != n {
		t.Errorf("only %d distinct snapshot IDs survived, want %d", len(seen), n)
	}
}

func TestWithScopeLockCleansUpLockFileOnSuccess(t *testing.T) {
	withTestScope(t)
	scope, err := scopeDir("lock-test2", "lockdb2")
	if err != nil {
		t.Fatal(err)
	}
	if err := withScopeLock(scope, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := loadIndex(scope); err != nil {
		t.Fatal(err)
	}
	// The lock file itself must not linger after a successful release.
	entries, err := os.ReadDir(scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == ".index.lock" {
			t.Fatalf("lock file still present after withScopeLock returned")
		}
	}
}

// TestWithScopeLockReclaimsLockFromDeadPID is the regression test for the
// "permanently stale lock" gap: a lock file left behind by a process that
// no longer exists must be reclaimed quickly (via the PID-liveness check),
// not just eventually after the full retry timeout — and definitely not
// left to block every future operation on this scope forever.
func TestWithScopeLockReclaimsLockFromDeadPID(t *testing.T) {
	withTestScope(t)
	scope, err := scopeDir("lock-dead-pid", "lockdb3")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveIndex(scope, &scopeIndex{}); err != nil {
		t.Fatal(err)
	}

	// A PID that is guaranteed to no longer be running: spawn a trivial
	// child process and wait for it to exit (and be reaped) before reusing
	// its PID value here.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skipf("could not run a throwaway child process to get a dead PID: %v", err)
	}
	deadPID := cmd.Process.Pid

	lockPath := scopeLockPath(scope)
	content := fmt.Sprintf("pid=%d locked-at=%s\n", deadPID, time.Now().Format(time.RFC3339))
	if err := os.WriteFile(lockPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// Recent mtime — if the age-based fallback were the only thing that
	// could reclaim this, it would NOT trigger yet (well under
	// scopeLockStaleAge). Only the PID-liveness path should reclaim it.
	now := time.Now()
	if err := os.Chtimes(lockPath, now, now); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	err = withScopeLock(scope, func() error { return nil })
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("withScopeLock did not reclaim the dead-PID lock: %v", err)
	}
	if elapsed >= scopeLockTimeout {
		t.Errorf("withScopeLock took %s to reclaim a dead-PID lock — expected fast reclaim well under the %s contention timeout, not a full wait", elapsed, scopeLockTimeout)
	}
}

// TestWithScopeLockReclaimsAgedLock covers the portable fallback path: a
// lock file whose recorded PID genuinely is still alive (this test process
// itself) but whose age exceeds scopeLockStaleAge must still be reclaimed —
// this is what protects platforms/situations where PID liveness can't be
// checked (see scopelock_other.go) from a permanently stuck lock.
func TestWithScopeLockReclaimsAgedLock(t *testing.T) {
	withTestScope(t)
	scope, err := scopeDir("lock-aged", "lockdb4")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveIndex(scope, &scopeIndex{}); err != nil {
		t.Fatal(err)
	}

	lockPath := scopeLockPath(scope)
	// This process's own PID: genuinely alive, so the PID-liveness check
	// alone must NOT be what reclaims this lock.
	content := fmt.Sprintf("pid=%d locked-at=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	if err := os.WriteFile(lockPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * scopeLockStaleAge)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}

	if err := withScopeLock(scope, func() error { return nil }); err != nil {
		t.Fatalf("withScopeLock did not reclaim the aged lock: %v", err)
	}
}

// TestWithScopeLockDoesNotReclaimALiveRecentLock is the safety-net check:
// a lock that's both recently created AND held by a live PID must never be
// reclaimed out from under its legitimate holder — confirms the two
// reclaim conditions are actually conjunctive-safe (neither one alone is
// enough), not accidentally over-eager.
func TestWithScopeLockDoesNotReclaimALiveRecentLock(t *testing.T) {
	withTestScope(t)
	scope, err := scopeDir("lock-live", "lockdb5")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveIndex(scope, &scopeIndex{}); err != nil {
		t.Fatal(err)
	}

	lockPath := scopeLockPath(scope)
	content := fmt.Sprintf("pid=%d locked-at=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	if err := os.WriteFile(lockPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	err = withScopeLock(scope, func() error {
		t.Fatalf("fn ran, meaning the live/recent lock was wrongly reclaimed")
		return nil
	})
	if err == nil {
		t.Fatalf("expected withScopeLock to time out contending against a live, recent lock, got nil error")
	}
	// Clean up the lock file this test planted, so it doesn't leak into
	// other tests via a shared temp root.
	os.Remove(filepath.Join(scope, ".index.lock"))
}
