// Package secrets stores sensitive values (database passwords, API keys)
// in the operating system's credential store — macOS Keychain, Windows
// Credential Manager, or the Linux Secret Service — instead of plaintext
// config files. On systems without a usable keyring (e.g. headless Linux
// without D-Bus), Available reports false and callers fall back to the
// legacy 0600-file behavior.
package secrets

import (
	"errors"
	"sync"

	"github.com/zalando/go-keyring"
)

// service namespaces every entry this tool writes into the OS store.
const service = "mongobak"

// ErrNotFound is returned by Get when no secret exists under the key.
var ErrNotFound = errors.New("secret not found")

var (
	probeOnce sync.Once
	probeOK   bool

	// cache avoids re-hitting the OS store (a subprocess exec on macOS) for
	// every config load in the same process. Guarded by cacheMu.
	cacheMu sync.Mutex
	cache   = map[string]string{}
)

// Available reports whether a working system keyring exists. The first
// call probes with a set/get/delete round-trip; the result is cached for
// the process lifetime.
func Available() bool {
	probeOnce.Do(func() {
		const probeKey = "mongobak-keyring-probe"
		if err := keyring.Set(service, probeKey, "ok"); err != nil {
			return
		}
		v, err := keyring.Get(service, probeKey)
		_ = keyring.Delete(service, probeKey)
		probeOK = err == nil && v == "ok"
	})
	return probeOK
}

// Set stores a secret under key.
func Set(key, value string) error {
	if err := keyring.Set(service, key, value); err != nil {
		return err
	}
	cacheMu.Lock()
	cache[key] = value
	cacheMu.Unlock()
	return nil
}

// Get retrieves a secret, returning ErrNotFound if it doesn't exist.
func Get(key string) (string, error) {
	cacheMu.Lock()
	if v, ok := cache[key]; ok {
		cacheMu.Unlock()
		return v, nil
	}
	cacheMu.Unlock()

	v, err := keyring.Get(service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	cacheMu.Lock()
	cache[key] = v
	cacheMu.Unlock()
	return v, nil
}

// Delete removes a secret. Deleting a missing key is not an error.
func Delete(key string) error {
	cacheMu.Lock()
	delete(cache, key)
	cacheMu.Unlock()
	err := keyring.Delete(service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// MockInit replaces the OS keyring with an in-memory store for tests.
func MockInit() {
	keyring.MockInit()
	probeOnce.Do(func() {})
	probeOK = true
	cacheMu.Lock()
	cache = map[string]string{}
	cacheMu.Unlock()
}
