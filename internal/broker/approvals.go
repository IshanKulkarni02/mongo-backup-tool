package broker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

// Status is where an approval request is in its life.
type Status string

const (
	StatusPending  Status = "pending"  // waiting for the user
	StatusRunning  Status = "running"  // approved, executing
	StatusDone     Status = "done"     // executed successfully
	StatusFailed   Status = "failed"   // approved but execution failed
	StatusDenied   Status = "denied"   // refused by the user (or no operator to ask)
	StatusExpired  Status = "expired"  // nobody decided in time
	StatusCanceled Status = "canceled" // broker shut down
)

func (s Status) terminal() bool {
	return s == StatusDone || s == StatusFailed || s == StatusDenied || s == StatusExpired || s == StatusCanceled
}

// Approval is one change an agent asked DBHelm to make.
type Approval struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"` // sql | doc | restore
	Connection string     `json:"connection"`
	Database   string     `json:"database"`
	Statement  string     `json:"statement"` // the exact change, as shown to the user
	Risk       string     `json:"risk"`      // none | confirm | dangerous
	Reason     string     `json:"reason,omitempty"`
	Status     Status     `json:"status"`
	DecidedBy  string     `json:"decidedBy,omitempty"` // operator | autopilot
	CreatedAt  string     `json:"createdAt"`
	DecidedAt  string     `json:"decidedAt,omitempty"`
	Result     any        `json:"result,omitempty"`
	Error      *ErrorBody `json:"error,omitempty"`

	run    func(ctx context.Context) (any, error) // executes the change once approved
	safety bool                                   // take a safety snapshot before running
	done   chan struct{}                          // closed at a terminal status
}

// raw returns the live request for the executor. Its run/safety fields are
// set before Add and never change, so reading them without the lock is safe.
func (q *Queue) raw(id string) (*Approval, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	a, ok := q.items[id]
	return a, ok
}

// snapshot returns a copy safe to hand to another goroutine.
func (a *Approval) snapshot() Approval {
	c := *a
	c.run, c.done = nil, nil
	return c
}

// Queue holds approval requests in memory. Nothing about a request outlives
// the broker: an unanswered request is not carried into the next run.
type Queue struct {
	mu    sync.Mutex
	items map[string]*Approval
	order []string
}

func NewQueue() *Queue { return &Queue{items: map[string]*Approval{}} }

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return "req_" + hex.EncodeToString(b)
}

// Add registers a pending request.
func (q *Queue) Add(a *Approval) {
	a.ID = newID()
	a.Status = StatusPending
	a.CreatedAt = now().Format(time.RFC3339)
	a.done = make(chan struct{})
	q.mu.Lock()
	q.items[a.ID] = a
	q.order = append(q.order, a.ID)
	q.mu.Unlock()
}

func (q *Queue) Get(id string) (Approval, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	a, ok := q.items[id]
	if !ok {
		return Approval{}, false
	}
	return a.snapshot(), true
}

// List returns pending requests first (oldest first), then the most recent
// finished ones, at most limit of the finished.
func (q *Queue) List(finishedLimit int) []Approval {
	q.mu.Lock()
	defer q.mu.Unlock()
	var pending, finished []Approval
	for _, id := range q.order {
		a := q.items[id]
		if a.Status == StatusPending || a.Status == StatusRunning {
			pending = append(pending, a.snapshot())
		} else {
			finished = append(finished, a.snapshot())
		}
	}
	sort.SliceStable(finished, func(i, j int) bool { return finished[i].CreatedAt > finished[j].CreatedAt })
	if finishedLimit >= 0 && len(finished) > finishedLimit {
		finished = finished[:finishedLimit]
	}
	return append(pending, finished...)
}

// transition moves a request from pending to next, exactly once. It reports
// whether this call performed the transition (so a request can't be both
// approved and denied, or approved twice).
func (q *Queue) transition(id string, from Status, next Status, by string) (*Approval, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	a, ok := q.items[id]
	if !ok || a.Status != from {
		return a, false
	}
	a.Status = next
	if by != "" {
		a.DecidedBy = by
		a.DecidedAt = now().Format(time.RFC3339)
	}
	if next.terminal() {
		close(a.done)
	}
	return a, true
}

// finish records the outcome of an approved request's execution.
func (q *Queue) finish(id string, result any, eb *ErrorBody) {
	q.mu.Lock()
	defer q.mu.Unlock()
	a, ok := q.items[id]
	if !ok || a.Status.terminal() {
		return
	}
	if eb != nil {
		a.Status, a.Error = StatusFailed, eb
	} else {
		a.Status, a.Result = StatusDone, result
	}
	close(a.done)
}

// Wait blocks until the request reaches a terminal status, ctx ends, or d
// elapses, and returns the request's state at that moment.
func (q *Queue) Wait(ctx context.Context, id string, d time.Duration) (Approval, bool) {
	q.mu.Lock()
	a, ok := q.items[id]
	q.mu.Unlock()
	if !ok {
		return Approval{}, false
	}
	if d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-a.done:
		case <-t.C:
		case <-ctx.Done():
		}
	}
	return q.Get(id)
}

// expireStale marks pending requests older than ttl as expired.
func (q *Queue) expireStale(ttl time.Duration) []string {
	q.mu.Lock()
	var stale []string
	cutoff := now().Add(-ttl)
	for _, id := range q.order {
		a := q.items[id]
		if a.Status != StatusPending {
			continue
		}
		if t, err := time.Parse(time.RFC3339, a.CreatedAt); err == nil && t.Before(cutoff) {
			stale = append(stale, id)
		}
	}
	q.mu.Unlock()
	for _, id := range stale {
		q.transition(id, StatusPending, StatusExpired, "")
	}
	return stale
}

func (q *Queue) cancelAll() {
	q.mu.Lock()
	ids := append([]string(nil), q.order...)
	q.mu.Unlock()
	for _, id := range ids {
		q.transition(id, StatusPending, StatusCanceled, "")
	}
}
