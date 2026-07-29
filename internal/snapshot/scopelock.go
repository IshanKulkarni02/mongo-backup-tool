package snapshot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// scopeLockTimeout bounds how long acquiring a scope's cross-process lock
// waits (retrying) before giving up — matches boltOpenTimeout's UX (a
// bounded, actionable failure rather than an indefinite hang).
const scopeLockTimeout = 5 * time.Second

// scopeLockStaleAge is the fallback threshold (used where the recorded
// holder PID can't be checked for liveness — see processAlive) beyond
// which an existing lock file is presumed abandoned by a crashed or killed
// process, rather than genuinely still held. Set well above any realistic
// legitimate critical section this lock ever wraps (fast local JSON/backend
// I/O — index read/write, GC's doc-ref sweep; never the Mongo network scan
// itself, which happens outside the lock) so a slow-but-alive holder is
// never mistaken for a dead one.
const scopeLockStaleAge = 2 * time.Minute

func scopeLockPath(scope string) string { return filepath.Join(scope, ".index.lock") }

// withScopeLock runs fn while holding an exclusive, cross-process advisory
// lock on the scope directory's manifest+index publish critical section
// (read index -> mutate -> write index). The bbolt backend already gets
// this protection for free — bbolt's own file lock is held for an entire
// Create/GC/Tag call, so two bolt-backed operations on the same scope can
// never interleave their index reads and writes. The fs backend (used for
// Git remote sync) has no equivalent: two processes could each load
// index.json, append their own change, and one save clobbers the other's.
// This lock closes that gap for both backends uniformly — redundant but
// harmless where bbolt already serializes, newly protective where it
// doesn't.
//
// Implemented as exclusive file creation (O_CREATE|O_EXCL) rather than a
// real OS advisory lock (flock/LockFileEx): those need different syscalls
// per platform and this project has no Windows environment to verify a
// LockFileEx-based implementation against, so a portable, dependency-free
// mechanism was chosen deliberately over an unverifiable one. The
// stale-lock risk that approach carries (a lock file left behind by a
// process that crashed before removing it) is handled explicitly by
// reclaimIfStale below, rather than accepted as a gap.
func withScopeLock(scope string, fn func() error) error {
	path := scopeLockPath(scope)
	deadline := time.Now().Add(scopeLockTimeout)

	var f *os.File
	for {
		var err error
		f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("acquiring scope lock %s: %w", path, err)
		}
		if reclaimIfStale(path) {
			continue // the stale lock is gone; retry acquiring immediately
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("this connection+database's snapshot index is already being updated by another mongobak operation — wait for it to finish and try again. If you're certain no other mongobak process is running (e.g. after a crash), delete the lock file manually and retry: %s", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
	fmt.Fprintf(f, "pid=%d locked-at=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	f.Close()

	defer os.Remove(path)
	return fn()
}

// reclaimIfStale removes path if it's demonstrably an abandoned lock from a
// crashed or killed process, rather than one genuinely still held, and
// reports whether it did. Two independent signals, checked in order:
//
//  1. The recorded holder PID is no longer running (processAlive, platform-
//     specific — see scopelock_unix.go/scopelock_other.go). This is the
//     precise, fast-recovery path where it's available.
//  2. The lock file is simply older than scopeLockStaleAge. This is the
//     portable fallback for platforms/situations where PID liveness can't
//     be checked (e.g. this project has no way to verify a Windows
//     implementation) — coarser and slower to recover, but never
//     unboundedly stuck, and safe against reclaiming a slow-but-alive
//     holder because the threshold is set well above any real critical
//     section's duration.
//
// Never reclaims a lock that fails both checks — i.e. a live PID and an
// age still under the threshold is left alone, exactly matching a
// genuinely active concurrent operation.
func reclaimIfStale(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false // already gone (another process just reclaimed or released it) — let the caller's retry handle it
	}

	if pid, ok := readLockPID(path); ok && !processAlive(pid) {
		os.Remove(path)
		return true
	}

	if time.Since(info.ModTime()) > scopeLockStaleAge {
		os.Remove(path)
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
