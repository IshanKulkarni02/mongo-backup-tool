//go:build unix

package snapshot

import (
	"runtime"
	"syscall"
)

// peakRSSBytes returns this process's peak resident set size so far, for
// the load test's memory-boundedness assertions. Darwin reports
// Rusage.Maxrss in bytes; Linux reports it in kilobytes — normalized here so
// callers get bytes on both.
func peakRSSBytes() int64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	if runtime.GOOS == "linux" {
		return ru.Maxrss * 1024
	}
	return ru.Maxrss
}
