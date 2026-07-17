package engine

import (
	"fmt"
	"sort"
	"sync"
)

var (
	regMu    sync.RWMutex
	registry = map[string]Engine{}
)

// Register makes an engine available by its ID. Engine packages call this
// from init(), so importing an engine package (even blank) registers it —
// the same pattern as database/sql drivers.
func Register(e Engine) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := registry[e.ID()]; dup {
		panic(fmt.Sprintf("engine: Register called twice for %q", e.ID()))
	}
	registry[e.ID()] = e
}

// Lookup returns the engine registered under id.
func Lookup(id string) (Engine, error) {
	regMu.RLock()
	defer regMu.RUnlock()
	e, ok := registry[id]
	if !ok {
		return nil, fmt.Errorf("unsupported database engine %q", id)
	}
	return e, nil
}

// IDs returns every registered engine ID, sorted.
func IDs() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(registry))
	for id := range registry {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
