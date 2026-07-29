//go:build !unix

package snapshot

// peakRSSBytes has no portable implementation outside unix (Getrusage's
// shape differs on Windows) — the load test still runs and asserts
// correctness, it just skips the RSS-boundedness assertions there.
func peakRSSBytes() int64 { return 0 }
