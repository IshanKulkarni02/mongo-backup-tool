package main

import (
	"fmt"
	"sync"
	"testing"
)

// TestAddConnectionConcurrentCallsDontLoseWrites guards against #22's
// central failure scenario: Wails dispatches every exported method call
// (AddConnection included) in its own goroutine, so a user adding several
// connections in quick succession — or one add landing while the startup
// credential-migration goroutine is still saving — used to race on
// config.json, with whichever Save landed last silently discarding
// earlier connections. AddConnection now goes through config.Update,
// which serializes the whole load-mutate-save sequence.
func TestAddConnectionConcurrentCallsDontLoseWrites(t *testing.T) {
	withTempConfigDir(t)
	a := NewApp()
	t.Cleanup(a.engines.Close)

	const n = 30
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := a.AddConnection(ConnectionInput{
				Name: fmt.Sprintf("conn-%d", i),
				URI:  "mongodb://localhost:27017",
			})
			if err != nil {
				t.Errorf("AddConnection: %v", err)
			}
		}(i)
	}
	wg.Wait()

	conns, err := a.ListConnections()
	if err != nil {
		t.Fatalf("ListConnections: %v", err)
	}
	if len(conns) != n {
		t.Fatalf("expected %d connections after %d concurrent AddConnection calls, got %d", n, n, len(conns))
	}
	for i := 0; i < n; i++ {
		found := false
		for _, c := range conns {
			if c.Name == fmt.Sprintf("conn-%d", i) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("conn-%d is missing — its AddConnection was lost to a race", i)
		}
	}
}
