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
const service = "dbhelm"

// ErrNotFound is returned by Get when no secret exists under the key.
var ErrNotFound = errors.New("secret not found")

// UnavailableWarning is shown wherever a caller needs to tell the user
// their credentials are unprotected on this machine (no OS keyring means
// Save falls back to storing them in plaintext, in config.json — see
// internal/config/credentials.go's stripCredentials). The desktop app
// already surfaces this in the Connections view; CLI/TUI call sites
// should show it too whenever !Available(), since a headless server or
// container is exactly the environment most likely to lack a keyring.
const UnavailableWarning = "no system keyring is available on this machine — database and SSH credentials are stored in plaintext in config.json, protected only by its owner-only file permissions (0600), not encryption. Set up a keyring (e.g. gnome-keyring or a Secret Service provider on Linux) for stronger protection."

var (
	probeOnce sync.Once
	probeOK   bool

	// cache avoids re-hitting the OS store (a subprocess exec on macOS) for
	// every config load in the same process. Guarded by cacheMu.
	cacheMu sync.Mutex
	cache   = map[string]string{}

	// failKeys, when non-nil, makes Set fail for exactly these keys —
	// test-only, for simulating a keyring backend that silently fails a
	// subset of writes (the real-world failure mode issue #62 guards
	// against: MigrateCredentials must not report success for a secret
	// whose Set actually failed).
	failKeysMu sync.Mutex
	failKeys   map[string]bool
)

// Available reports whether a working system keyring exists. The first
// call probes with a set/get/delete round-trip; the result is cached for
// the process lifetime.
func Available() bool {
	probeOnce.Do(func() {
		const probeKey = "dbhelm-keyring-probe"
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
	failKeysMu.Lock()
	shouldFail := failKeys[key]
	failKeysMu.Unlock()
	if shouldFail {
		return errors.New("secrets: mock failure for " + key)
	}
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
	failKeysMu.Lock()
	failKeys = nil
	failKeysMu.Unlock()
}

// MockFailFor makes Set fail for exactly the given keys, for testing a
// partial keyring-write-failure path. Call after MockInit; pass no keys
// (or call ResetForTesting) to clear.
func MockFailFor(keys ...string) {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	failKeysMu.Lock()
	failKeys = m
	failKeysMu.Unlock()
}

// MockUnavailable forces Available() to report false for the rest of the
// process, for testing the plaintext-fallback/warning path without
// depending on whether the real test environment happens to have a
// working keyring. Callers should defer ResetForTesting so this doesn't
// leak into unrelated tests sharing the same test binary.
func MockUnavailable() {
	probeOnce.Do(func() {})
	probeOK = false
	cacheMu.Lock()
	cache = map[string]string{}
	cacheMu.Unlock()
}

// ResetForTesting clears the cached Available() probe result and cache,
// letting a subsequent call re-probe the real keyring (or be re-mocked
// via MockInit/MockUnavailable) — for tests that call MockInit/
// MockUnavailable and need to avoid leaking that forced state into other
// tests sharing the same test binary process.
func ResetForTesting() {
	probeOnce = sync.Once{}
	probeOK = false
	cacheMu.Lock()
	cache = map[string]string{}
	cacheMu.Unlock()
}
