package engine

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

type fakeSession struct {
	closed int32
}

func (f *fakeSession) Ping(ctx context.Context) error { return nil }
func (f *fakeSession) ListDatabases(ctx context.Context) ([]string, error) {
	return nil, nil
}
func (f *fakeSession) ListNamespaces(ctx context.Context, database string) ([]NamespaceInfo, error) {
	return nil, nil
}
func (f *fakeSession) Close(ctx context.Context) error {
	atomic.StoreInt32(&f.closed, 1)
	return nil
}
func (f *fakeSession) isClosed() bool { return atomic.LoadInt32(&f.closed) == 1 }

type fakeEngine struct {
	opens   int32
	fail    bool
	created []*fakeSession
}

func (e *fakeEngine) ID() string         { return "fake" }
func (e *fakeEngine) Capabilities() Caps { return Caps{} }
func (e *fakeEngine) Open(ctx context.Context, cfg ConnConfig) (Session, error) {
	atomic.AddInt32(&e.opens, 1)
	if e.fail {
		return nil, fmt.Errorf("boom")
	}
	s := &fakeSession{}
	e.created = append(e.created, s)
	return s, nil
}

func newTestManager(eng *fakeEngine) *Manager {
	return NewManager(func(name string) (ConnConfig, Engine, error) {
		return ConnConfig{Name: name}, eng, nil
	})
}

func TestManagerAcquireCachesSession(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestManager(eng)
	defer m.Close()

	s1, release1, err := m.Acquire(context.Background(), "local")
	if err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	s2, release2, err := m.Acquire(context.Background(), "local")
	if err != nil {
		t.Fatalf("acquire 2: %v", err)
	}
	if s1 != s2 {
		t.Fatalf("expected the same cached session, got different instances")
	}
	if atomic.LoadInt32(&eng.opens) != 1 {
		t.Fatalf("expected exactly 1 Open call, got %d", eng.opens)
	}
	release1()
	release2()
}

func TestManagerAcquirePropagatesOpenError(t *testing.T) {
	eng := &fakeEngine{fail: true}
	m := newTestManager(eng)
	defer m.Close()

	_, _, err := m.Acquire(context.Background(), "local")
	if err == nil {
		t.Fatal("expected an error from a failing engine")
	}
}

func TestManagerEvictIdleClosesUnusedSessions(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestManager(eng)
	defer m.Close()

	_, release, err := m.Acquire(context.Background(), "local")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	release()

	if len(eng.created) != 1 {
		t.Fatalf("expected 1 session created, got %d", len(eng.created))
	}
	sess := eng.created[0]

	// Not idle yet: eviction sweep at "now" shouldn't touch a freshly used session.
	m.evictIdle(time.Now())
	if sess.isClosed() {
		t.Fatal("session closed before its idle TTL elapsed")
	}

	// Simulate the TTL elapsing by sweeping far enough in the future.
	m.evictIdle(time.Now().Add(2 * defaultIdleTTL))
	if !sess.isClosed() {
		t.Fatal("expected idle session to be closed by the reaper")
	}

	// A subsequent Acquire must open a fresh session, not reuse the closed one.
	s2, release2, err := m.Acquire(context.Background(), "local")
	if err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	defer release2()
	if s2 == sess {
		t.Fatal("expected a new session after eviction, got the closed one")
	}
	if atomic.LoadInt32(&eng.opens) != 2 {
		t.Fatalf("expected 2 total Open calls, got %d", eng.opens)
	}
}

func TestManagerEvictIdleSkipsSessionsInUse(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestManager(eng)
	defer m.Close()

	_, release, err := m.Acquire(context.Background(), "local")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	sess := eng.created[0]

	m.evictIdle(time.Now().Add(2 * defaultIdleTTL))
	if sess.isClosed() {
		t.Fatal("in-use session must not be closed by the idle reaper")
	}
	release()
}

func TestManagerInvalidateClosesIdleSession(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestManager(eng)
	defer m.Close()

	_, release, err := m.Acquire(context.Background(), "local")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	release()

	m.Invalidate("local")
	if !eng.created[0].isClosed() {
		t.Fatal("expected Invalidate to close the idle session")
	}

	// Acquire after invalidation must open a fresh session.
	_, release2, err := m.Acquire(context.Background(), "local")
	if err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	defer release2()
	if atomic.LoadInt32(&eng.opens) != 2 {
		t.Fatalf("expected 2 Open calls after invalidation, got %d", eng.opens)
	}
}

func TestManagerInvalidateWhileInUseClosesOnRelease(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestManager(eng)
	defer m.Close()

	_, release, err := m.Acquire(context.Background(), "local")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	sess := eng.created[0]

	m.Invalidate("local")
	if sess.isClosed() {
		t.Fatal("session in use must not close immediately on Invalidate")
	}
	release()
	if !sess.isClosed() {
		t.Fatal("expected session to close once released after Invalidate")
	}
}

func TestManagerCloseClosesAllSessions(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestManager(eng)

	_, release, err := m.Acquire(context.Background(), "a")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	release()
	_, release2, err := m.Acquire(context.Background(), "b")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	release2()

	m.Close()
	for i, s := range eng.created {
		if !s.isClosed() {
			t.Fatalf("session %d not closed after Manager.Close", i)
		}
	}

	if _, _, err := m.Acquire(context.Background(), "a"); err == nil {
		t.Fatal("expected Acquire to fail after Manager.Close")
	}
}
