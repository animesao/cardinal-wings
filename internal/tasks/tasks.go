// Package tasks provides a tiny in-memory async job manager. Long-running
// operations (blueprint install, image pull) start asynchronously, return a
// task id immediately, and the panel polls /v1/tasks/{id} for status.
package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
) // Task is one async operation.
type Task struct {
	Seq        uint64    `json:"-"`
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Status     Status    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Error      string    `json:"error,omitempty"`
	Output     string    `json:"output,omitempty"`
	Progress   []string  `json:"progress,omitempty"`
}

// Manager runs and tracks async tasks.
type Manager struct {
	mu    sync.Mutex
	tasks map[string]*Task
	seq   uint64
	ttl   time.Duration
	path  string // optional JSON persistence file
}

// NewManager builds a task manager; ttl controls how long finished tasks are
// kept before cleanup (0 keeps them forever).
func NewManager(ttl time.Duration) *Manager {
	return &Manager{tasks: map[string]*Task{}, ttl: ttl}
}

// WithPersistence writes finished tasks to path so they survive a daemon
// restart. Returns the manager for chaining.
func (m *Manager) WithPersistence(path string) *Manager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.path = path
	if data, err := os.ReadFile(path); err == nil {
		var saved []Task
		if json.Unmarshal(data, &saved) == nil {
			for _, t := range saved {
				m.tasks[t.ID] = &t
				if t.Seq >= m.seq {
					m.seq = t.Seq
				}
			}
		}
	}
	return m
}

// saveLocked writes the current tasks to the persistence file, if configured.
// Only finished tasks are persisted (running ones die with the process).
func (m *Manager) saveLocked() {
	if m.path == "" {
		return
	}
	var out []Task
	for _, t := range m.tasks {
		if !t.FinishedAt.IsZero() {
			out = append(out, *t)
		}
	}
	data, err := json.Marshal(out)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return
	}
	_ = os.WriteFile(m.path, data, 0600)
}

// ProgressFunc receives live progress lines while a task runs.
type ProgressFunc func(line string)

// Submit queues a job and returns its id. The job runs in a background
// goroutine; fn receives a context that is cancelled when the task is
// cancelled via Cancel.
func (m *Manager) Submit(kind string, fn func(ctx context.Context) (string, error)) string {
	return m.submit(kind, nil, fn)
}

// SubmitLines is Submit for jobs that emit live progress: fn is called with
// the task context and a callback; each line the callback receives is
// appended to the task's Progress slice so a panel can show a live log.
func (m *Manager) SubmitLines(kind string, fn func(ctx context.Context, onLine ProgressFunc) (string, error)) string {
	return m.submit(kind, fn, nil)
}

func (m *Manager) submit(kind string, fnLines func(ctx context.Context, onLine ProgressFunc) (string, error), fn func(ctx context.Context) (string, error)) string {
	m.mu.Lock()
	m.seq++
	id := fmt.Sprintf("task-%d", m.seq)
	t := &Task{Seq: m.seq, ID: id, Kind: kind, Status: StatusQueued, CreatedAt: time.Now()}
	m.tasks[id] = t
	m.mu.Unlock()

	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		m.mu.Lock()
		t.Status = StatusRunning
		t.StartedAt = time.Now()
		m.mu.Unlock()

		var out string
		var err error
		if fnLines != nil {
			out, err = fnLines(ctx, func(line string) {
				m.mu.Lock()
				t.Progress = append(t.Progress, line)
				m.mu.Unlock()
			})
		} else {
			out, err = fn(ctx)
		}

		m.mu.Lock()
		t.FinishedAt = time.Now()
		if err != nil {
			t.Status = StatusFailed
			t.Error = err.Error()
		} else {
			t.Status = StatusSucceeded
			t.Output = out
		}
		m.saveLocked()
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
	// Newest first.
	sort.Slice(out, func(i, j int) bool { return out[i].Seq > out[j].Seq })
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
	m.saveLocked()
}
