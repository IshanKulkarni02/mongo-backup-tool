package main

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestRecordQueryHistoryConcurrentCallsDontLoseEntries guards against
// #22: RunSQLQuery/RunSQLQueryJob/RunSQLExecute each call
// recordQueryHistory after their query finishes, and Wails dispatches
// each of those RPCs in its own goroutine, so several queries finishing
// close together used to race on queryhistory.json and silently drop
// entries.
func TestRecordQueryHistoryConcurrentCallsDontLoseEntries(t *testing.T) {
	withTempConfigDir(t)
	a := &App{}

	const n = 30
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			recordQueryHistory("local", "app", fmt.Sprintf("SELECT %d", i), 1, time.Millisecond, nil)
		}(i)
	}
	wg.Wait()

	entries, err := a.ListQueryHistory("", 0)
	if err != nil {
		t.Fatalf("ListQueryHistory: %v", err)
	}
	if len(entries) != n {
		t.Fatalf("expected %d history entries after %d concurrent calls, got %d", n, n, len(entries))
	}
}
