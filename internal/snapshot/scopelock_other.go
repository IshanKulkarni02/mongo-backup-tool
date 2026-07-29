//go:build !unix

package snapshot

// processAlive has no portable, dependency-free implementation outside
// unix in the standard library (Windows needs OpenProcess/GetExitCodeProcess
// via syscall or golang.org/x/sys, and this project has no Windows
// environment to verify that against). Always reporting "alive" here means
// staleness recovery on Windows falls back entirely to reclaimIfStale's
// age-based check, which is still bounded (scopeLockStaleAge), just slower
// to recover than the PID-checked path.
func processAlive(pid int) bool { return true }
