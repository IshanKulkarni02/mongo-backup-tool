package main

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// JobStatus is the lifecycle state of an async operation (snapshot create,
// restore, backup, etc.) run from the desktop app.
type JobStatus string

const (
	JobRunning JobStatus = "running"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"
)

// Job is pushed to the frontend via the "job:update" event as it changes,
// so the UI shows live progress instead of polling.
type Job struct {
	ID      string    `json:"id"`
	Type    string    `json:"type"`
	Status  JobStatus `json:"status"`
	Message string    `json:"message,omitempty"`
	Result  any       `json:"result,omitempty"`
}

// JobProgress is a live, non-terminal update for a background job.
type JobProgress struct {
	ID      string `json:"id"`
	Phase   string `json:"phase"`
	Current int64  `json:"current"`
	Total   int64  `json:"total"`
	Line    string `json:"line,omitempty"`
}

type jobManager struct {
	mu         sync.Mutex
	jobs       map[string]*Job
	cancels    map[string]context.CancelFunc
	onUpdate   func(Job) // set by App.startup once the Wails context exists
	onProgress func(JobProgress)
}

func newJobManager() *jobManager {
	return &jobManager{jobs: map[string]*Job{}, cancels: map[string]context.CancelFunc{}}
}

func (m *jobManager) progress(id, phase string, current, total int64, line string) {
	m.mu.Lock()
	_, exists := m.jobs[id]
	progress := m.onProgress
	m.mu.Unlock()
	if exists && progress != nil {
		progress(JobProgress{ID: id, Phase: phase, Current: current, Total: total, Line: line})
	}
}

func (m *jobManager) start(jobType string) *Job {
	m.mu.Lock()
	j := &Job{ID: uuid.NewString(), Type: jobType, Status: JobRunning}
	m.jobs[j.ID] = j
	update := m.onUpdate
	m.mu.Unlock()
	if update != nil {
		update(*j)
	}
	return j
}

func (m *jobManager) finish(id string, err error, result any) {
	m.mu.Lock()
	j, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	if err != nil {
		j.Status = JobFailed
		j.Message = err.Error()
	} else {
		j.Status = JobDone
		j.Result = result
	}
	snapshot := *j
	update := m.onUpdate
	m.mu.Unlock()
	if update != nil {
		update(snapshot)
	}
}

// run starts fn on its own goroutine and reports the job's completion via
// the onUpdate callback (a Wails event to the frontend), returning the new
// job's ID immediately so the caller isn't blocked.
func (m *jobManager) run(jobType string, fn func() (any, error)) string {
	j := m.start(jobType)
	go func() {
		result, err := fn()
		m.finish(j.ID, err, result)
	}()
	return j.ID
}

// runCancelable is like run, but fn receives a context that Cancel(id) can
// cancel mid-flight — the mechanism a long-running ad-hoc query uses so a
// user can actually stop it instead of waiting it out.
func (m *jobManager) runCancelable(jobType string, fn func(ctx context.Context) (any, error)) string {
	j := m.start(jobType)
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancels[j.ID] = cancel
	m.mu.Unlock()
	go func() {
		result, err := fn(ctx)
		m.mu.Lock()
		delete(m.cancels, j.ID)
		m.mu.Unlock()
		m.finish(j.ID, err, result)
	}()
	return j.ID
}

// cancel cancels a running cancelable job, reporting whether one was found.
// Canceling a job that already finished (or was never cancelable) is not
// an error — it just has nothing to do.
func (m *jobManager) cancel(id string) bool {
	m.mu.Lock()
	cancelFn, ok := m.cancels[id]
	m.mu.Unlock()
	if !ok {
		return false
	}
	cancelFn()
	return true
}
