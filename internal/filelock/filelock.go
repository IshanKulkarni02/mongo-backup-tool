// Package filelock provides a cross-process advisory lock backed by a
// plain lock file, for serializing a read-modify-write critical section
// (load a JSON file, mutate it, save it back) against concurrent dbhelm
// processes — not just concurrent goroutines within one process, which a
// plain sync.Mutex would already handle. Extracted from
// internal/snapshot's original scope-lock implementation so
// internal/scheduler could reuse the same mechanism rather than
// reimplementing it.
package filelock

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// With runs fn while holding an exclusive lock at lockPath, implemented as
// exclusive file creation (O_CREATE|O_EXCL) rather than a real OS advisory
// lock (flock/LockFileEx): those need different syscalls per platform and
// this project has no Windows environment to verify a LockFileEx-based
// implementation against, so a portable, dependency-free mechanism was
// chosen deliberately over an unverifiable one. The stale-lock risk that
// approach carries (a lock file left behind by a process that crashed
// before removing it) is handled explicitly by reclaimIfStale, rather
// than accepted as a gap.
//
// Acquisition retries for up to timeout, reclaiming (removing) an
// existing lock file first if it's demonstrably abandoned — see
// reclaimIfStale. subject names what's being protected, used only in the
// error message if acquisition times out contending against a lock that's
// still genuinely held (e.g. "the scheduler's schedules.json").
func With(lockPath string, timeout, staleAge time.Duration, subject string, fn func() error) error {
	deadline := time.Now().Add(timeout)

	var f *os.File
	for {
		var err error
		f, err = os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("acquiring lock %s: %w", lockPath, err)
		}
		if reclaimIfStale(lockPath, staleAge) {
			continue // the stale lock is gone; retry acquiring immediately
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s is already being updated by another dbhelm operation — wait for it to finish and try again. If you're certain no other dbhelm process is running (e.g. after a crash), delete the lock file manually and retry: %s", subject, lockPath)
		}
		time.Sleep(20 * time.Millisecond)
	}
	fmt.Fprintf(f, "pid=%d locked-at=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	f.Close()

	defer os.Remove(lockPath)
	return fn()
}

// reclaimIfStale removes lockPath if it's demonstrably an abandoned lock
// from a crashed or killed process, rather than one genuinely still held,
// and reports whether it did. Two independent signals, checked in order:
//
//  1. The recorded holder PID is no longer running (processAlive,
//     platform-specific — see filelock_unix.go/filelock_other.go). This
//     is the precise, fast-recovery path where it's available.
//  2. The lock file is simply older than staleAge. This is the portable
//     fallback for platforms/situations where PID liveness can't be
//     checked — coarser and slower to recover, but never unboundedly
//     stuck, and safe against reclaiming a slow-but-alive holder as long
//     as staleAge is set well above any real critical section's duration.
//
// Never reclaims a lock that fails both checks — i.e. a live PID and an
// age still under the threshold is left alone, exactly matching a
// genuinely active concurrent operation.
func reclaimIfStale(lockPath string, staleAge time.Duration) bool {
	info, err := os.Stat(lockPath)
	if err != nil {
		return false // already gone (another process just reclaimed or released it) — let the caller's retry handle it
	}

	if pid, ok := readLockPID(lockPath); ok && !processAlive(pid) {
		os.Remove(lockPath)
		return true
	}

	if time.Since(info.ModTime()) > staleAge {
		os.Remove(lockPath)
		return true
	}

	return false
}

func readLockPID(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	var pid int
	if _, err := fmt.Sscanf(string(data), "pid=%d", &pid); err != nil {
		return 0, false
	}
	return pid, true
}
