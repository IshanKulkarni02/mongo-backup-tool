package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestJobManagerRunCancelableCancelsContext(t *testing.T) {
	m := newJobManager()
	started := make(chan struct{})
	// The frontend only ever learns a job's final state from the
	// "job:update" callback (jobManager.jobs is cleared for a job as
	// soon as that update is sent — see TestJobManagerRemovesCompletedJob*
	// below), so this test observes completion the same way rather than
	// polling the map directly.
	updates := make(chan Job, 4)
	m.onUpdate = func(j Job) { updates <- j }

	id := m.runCancelable("test", func(ctx context.Context) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})

	<-started
	if ok := m.cancel(id); !ok {
		t.Fatal("expected cancel to find the running job")
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case j := <-updates:
			if j.ID != id {
				continue
			}
			if j.Status == JobFailed {
				if !errors.Is(context.Canceled, context.Canceled) || j.Message == "" {
					t.Fatalf("expected a cancellation message, got %q", j.Message)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for job to reflect cancellation")
		}
	}
}

func TestJobManagerCancelUnknownJobReturnsFalse(t *testing.T) {
	m := newJobManager()
	if m.cancel("does-not-exist") {
		t.Fatal("expected cancel of an unknown job to report false")
	}
}

func TestJobManagerCancelAfterCompletionReturnsFalse(t *testing.T) {
	m := newJobManager()
	done := make(chan struct{})
	id := m.runCancelable("test", func(ctx context.Context) (any, error) {
		return "ok", nil
	})
	go func() {
		for {
			m.mu.Lock()
			_, stillCancelable := m.cancels[id]
			m.mu.Unlock()
			if !stillCancelable {
				close(done)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("job never finished")
	}
	if m.cancel(id) {
		t.Fatal("expected cancel of an already-finished job to report false")
	}
}

// TestJobManagerRemovesCompletedJobFromMap is the regression test for
// #41: jobManager.jobs entries were never deleted, only cancels entries
// were cleaned up — every job run over the app's lifetime (SQL queries,
// cross-database searches, AI streams, etc.) left a permanent entry, an
// unbounded memory leak for a long-running session. finish must remove
// the job's entry once its terminal state has been reported.
func TestJobManagerRemovesCompletedJobFromMap(t *testing.T) {
	m := newJobManager()
	id := m.run("test", func() (any, error) { return "ok", nil })

	deadline := time.After(2 * time.Second)
	for {
		m.mu.Lock()
		_, stillTracked := m.jobs[id]
		m.mu.Unlock()
		if !stillTracked {
			return
		}
		select {
		case <-deadline:
			t.Fatal("job entry was never removed from jobManager.jobs after successful completion")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestJobManagerRemovesFailedJobFromMap confirms the same cleanup
// applies to a job that finishes with an error, not just a successful
// one.
func TestJobManagerRemovesFailedJobFromMap(t *testing.T) {
	m := newJobManager()
	id := m.run("test", func() (any, error) { return nil, errors.New("boom") })

	deadline := time.After(2 * time.Second)
	for {
		m.mu.Lock()
		_, stillTracked := m.jobs[id]
		m.mu.Unlock()
		if !stillTracked {
			return
		}
		select {
		case <-deadline:
			t.Fatal("job entry was never removed from jobManager.jobs after a failed completion")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestJobManagerWaitAllWaitsForRunningJob is the regression test for #63:
// app shutdown closed engine connections without waiting for in-flight
// background jobs, so a backup/restore mid-flight could have its
// connection torn out from under it. waitAll must not return while a job
// started via run() is still executing.
func TestJobManagerWaitAllWaitsForRunningJob(t *testing.T) {
	m := newJobManager()
	started := make(chan struct{})
	release := make(chan struct{})
	m.run("test", func() (any, error) {
		close(started)
		<-release
		return "ok", nil
	})

	<-started

	waitDone := make(chan struct{})
	go func() {
		m.waitAll(context.Background())
		close(waitDone)
	}()

	select {
	case <-waitDone:
		t.Fatal("waitAll returned while the job was still running")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("waitAll did not return after the job finished")
	}
}

// TestJobManagerWaitAllReturnsOnContextDeadline confirms waitAll doesn't
// hang app shutdown forever if a job never finishes — it gives up once
// the caller's context expires.
func TestJobManagerWaitAllReturnsOnContextDeadline(t *testing.T) {
	m := newJobManager()
	release := make(chan struct{})
	defer close(release)
	m.run("test", func() (any, error) {
		<-release
		return "ok", nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		m.waitAll(ctx)
		close(waitDone)
	}()

	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("waitAll did not respect the context deadline")
	}
}

// TestJobManagerWaitAllReturnsImmediatelyWhenIdle confirms waitAll doesn't
// block at all when there are no in-flight jobs — the common case at
// shutdown.
func TestJobManagerWaitAllReturnsImmediatelyWhenIdle(t *testing.T) {
	m := newJobManager()
	done := make(chan struct{})
	go func() {
		m.waitAll(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("waitAll blocked with no in-flight jobs")
	}
}

func TestJobManagerEmitsProgress(t *testing.T) {
	m := newJobManager()
	got := make(chan JobProgress, 1)
	m.onProgress = func(p JobProgress) { got <- p }
	j := m.start("test")

	m.progress(j.ID, "copy", 2, 10, "copied row")

	p := <-got
	if p.ID != j.ID || p.Phase != "copy" || p.Current != 2 || p.Total != 10 || p.Line != "copied row" {
		t.Fatalf("unexpected progress: %+v", p)
	}
}
