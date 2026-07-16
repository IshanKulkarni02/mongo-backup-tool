package main

import (
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

type jobManager struct {
	mu       sync.Mutex
	jobs     map[string]*Job
	onUpdate func(Job) // set by App.startup once the Wails context exists
}

func newJobManager() *jobManager {
	return &jobManager{jobs: map[string]*Job{}}
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
