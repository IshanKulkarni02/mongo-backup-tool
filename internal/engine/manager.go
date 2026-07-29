package engine

import (
	"context"
	"sync"
	"time"
)

// ResolveFunc maps a saved profile name to its connection config and
// engine. Injected so the manager stays decoupled from config storage.
type ResolveFunc func(name string) (ConnConfig, Engine, error)

// Manager caches one live Session per profile so interactive surfaces
// (browsing, relationship navigation) don't pay a fresh connect per call.
// Sessions are refcounted while in use and reaped after sitting idle.
type Manager struct {
	resolve ResolveFunc
	idleTTL time.Duration

	mu       sync.Mutex
	sessions map[string]*managedSession
	stop     chan struct{}
	closed   bool
}

type managedSession struct {
	sess     Session
	refs     int
	lastUsed time.Time
	doomed   bool // Invalidate()d while in use: close on last release
}

const defaultIdleTTL = 5 * time.Minute

// NewManager creates a manager and starts its idle reaper.
func NewManager(resolve ResolveFunc) *Manager {
	m := &Manager{
		resolve:  resolve,
		idleTTL:  defaultIdleTTL,
		sessions: map[string]*managedSession{},
		stop:     make(chan struct{}),
	}
	go m.reap()
	return m
}

// Acquire returns the cached session for a profile, opening one if needed.
// The caller must call the returned release func when done with the
// session (typically via defer); the session itself must not be Closed by
// the caller — the manager owns its lifecycle.
func (m *Manager) Acquire(ctx context.Context, name string) (Session, func(), error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, nil, context.Canceled
	}
	if ms, ok := m.sessions[name]; ok && !ms.doomed {
		ms.refs++
		ms.lastUsed = time.Now()
		m.mu.Unlock()
		return ms.sess, m.releaseFunc(ms), nil
	}
	m.mu.Unlock()

	// Open outside the lock so a slow connect doesn't block other profiles.
	cfg, eng, err := m.resolve(name)
	if err != nil {
		return nil, nil, err
	}
	sess, err := eng.Open(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		sess.Close(context.Background())
		return nil, nil, context.Canceled
	}
	// Another caller may have opened the same profile while we were
	// connecting — prefer theirs and discard ours.
	if ms, ok := m.sessions[name]; ok && !ms.doomed {
		ms.refs++
		ms.lastUsed = time.Now()
		m.mu.Unlock()
		sess.Close(context.Background())
		return ms.sess, m.releaseFunc(ms), nil
	}
	ms := &managedSession{sess: sess, refs: 1, lastUsed: time.Now()}
	m.sessions[name] = ms
	m.mu.Unlock()
	return sess, m.releaseFunc(ms), nil
}

func (m *Manager) releaseFunc(ms *managedSession) func() {
	released := false
	return func() {
		m.mu.Lock()
		if released {
			m.mu.Unlock()
			return
		}
		released = true
		ms.refs--
		ms.lastUsed = time.Now()
		closeNow := ms.doomed && ms.refs == 0
		m.mu.Unlock()
		if closeNow {
			ms.sess.Close(context.Background())
		}
	}
}

// Invalidate drops a profile's cached session (e.g. after the connection
// was edited or removed). If the session is currently in use it's closed
// once the last holder releases it.
func (m *Manager) Invalidate(name string) {
	m.mu.Lock()
	ms, ok := m.sessions[name]
	if ok {
		delete(m.sessions, name)
		ms.doomed = true
	}
	idle := ok && ms.refs == 0
	m.mu.Unlock()
	if idle {
		ms.sess.Close(context.Background())
	}
}

// Close shuts down the reaper and every cached session.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	close(m.stop)
	toClose := make([]Session, 0, len(m.sessions))
	for name, ms := range m.sessions {
		delete(m.sessions, name)
		ms.doomed = true
		if ms.refs == 0 {
			toClose = append(toClose, ms.sess)
		}
	}
	m.mu.Unlock()
	for _, s := range toClose {
		s.Close(context.Background())
	}
}

func (m *Manager) reap() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case now := <-ticker.C:
			m.evictIdle(now)
		}
	}
}

// evictIdle closes sessions that have no holders and haven't been used
// within the idle TTL. Split out from reap so tests can drive it directly.
func (m *Manager) evictIdle(now time.Time) {
	m.mu.Lock()
	var toClose []Session
	for name, ms := range m.sessions {
		if ms.refs == 0 && now.Sub(ms.lastUsed) > m.idleTTL {
			delete(m.sessions, name)
			toClose = append(toClose, ms.sess)
		}
	}
	m.mu.Unlock()
	for _, s := range toClose {
		s.Close(context.Background())
	}
}
