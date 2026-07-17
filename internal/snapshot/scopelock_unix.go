//go:build unix

package snapshot

import (
	"errors"
	"syscall"
)

// processAlive reports whether pid refers to a currently-running process,
// using signal 0 (no actual signal is sent — the kernel only checks
// existence and permission). ESRCH means no such process; any other
// outcome (success, or a permission error meaning the process exists but is
// owned by someone else) is treated as "alive" — reclaimIfStale should
// never guess a process is dead when it can't actually tell.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return !errors.Is(err, syscall.ESRCH)
}
