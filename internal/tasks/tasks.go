// Package tasks provides a tiny in-memory async job manager. Long-running
// operations (blueprint install, image pull) start asynchronously, return a
// task id immediately, and the panel polls /v1/tasks/{id} for status.
package tasks

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Status enumerates the lifecycle of a task.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// Task is one async operation.
type Task struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Status     Status    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Error      string    `json:"error,omitempty"`
	Output     string    `json:"output,omitempty"`
}

// Manager runs and tracks async tasks.
type Manager struct {
	mu    sync.Mutex
	tasks map[string]*Task
	seq   uint64
	ttl   time.Duration
}

// NewManager builds a task manager; ttl controls how long finished tasks are
// kept before cleanup (0 keeps them forever).
func NewManager(ttl time.Duration) *Manager {
	return &Manager{tasks: map[string]*Task{}, ttl: ttl}
}

// Submit queues a job and returns its id. The job runs in a background
// goroutine; fn receives a context that is cancelled when the task is
// cancelled via Cancel.
func (m *Manager) Submit(kind string, fn func(ctx context.Context) (string, error)) string {
	m.mu.Lock()
	m.seq++
	id := fmt.Sprintf("task-%d", m.seq)
	t := &Task{ID: id, Kind: kind, Status: StatusQueued, CreatedAt: time.Now()}
	m.tasks[id] = t
	m.mu.Unlock()

	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		m.mu.Lock()
		t.Status = StatusRunning
		t.StartedAt = time.Now()
		m.mu.Unlock()

		out, err := fn(ctx)

		m.mu.Lock()
		t.FinishedAt = time.Now()
		if err != nil {
			t.Status = StatusFailed
			t.Error = err.Error()
		} else {
			t.Status = StatusSucceeded
			t.Output = out
		}
		m.mu.Unlock()
	}()

	return id
}

// Get returns a copy of a task, or nil.
func (m *Manager) Get(id string) (*Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return nil, false
	}
	cp := *t
	return &cp, true
}

// List returns all tasks, newest first.
func (m *Manager) List() []Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		out = append(out, *t)
	}
	// Newest first (insertion order is by seq, so reverse).
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Prune removes finished tasks older than the manager TTL.
func (m *Manager) Prune() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ttl <= 0 {
		return
	}
	cutoff := time.Now().Add(-m.ttl)
	for id, t := range m.tasks {
		if !t.FinishedAt.IsZero() && t.FinishedAt.Before(cutoff) {
			delete(m.tasks, id)
		}
	}
}
