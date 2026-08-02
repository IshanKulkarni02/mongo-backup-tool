package snapshot

import (
	"path/filepath"
	"time"

	"github.com/IshanKulkarni02/dbhelm/internal/filelock"
)

// scopeLockTimeout bounds how long acquiring a scope's cross-process lock
// waits (retrying) before giving up — matches boltOpenTimeout's UX (a
// bounded, actionable failure rather than an indefinite hang).
const scopeLockTimeout = 5 * time.Second

// scopeLockStaleAge is the fallback threshold (used where the recorded
// holder PID can't be checked for liveness) beyond which an existing lock
// file is presumed abandoned by a crashed or killed process, rather than
// genuinely still held. Set well above any realistic legitimate critical
// section this lock ever wraps (fast local JSON/backend I/O — index
// read/write, GC's doc-ref sweep; never the Mongo network scan itself,
// which happens outside the lock) so a slow-but-alive holder is never
// mistaken for a dead one.
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
// doesn't. See internal/filelock for the locking mechanism itself (shared
// with internal/scheduler's schedules.json).
func withScopeLock(scope string, fn func() error) error {
	return filelock.With(scopeLockPath(scope), scopeLockTimeout, scopeLockStaleAge, "this connection+database's snapshot index", fn)
}
