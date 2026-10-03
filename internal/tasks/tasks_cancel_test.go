package tasks

import (
	"context"
	"errors"
	"testing"
	"time"
)

func waitStatus(t *testing.T, m *Manager, id string, want Status) *Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		tk, ok := m.Get(id)
		if !ok {
			t.Fatalf("task %s missing", id)
		}
		if tk.Status == want {
			return tk
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s stuck in %s, want %s", id, tk.Status, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCancelRunning(t *testing.T) {
	m := NewManager(0)
	started := make(chan struct{})
	id := m.Submit("test", func(ctx context.Context) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	})
	<-started
	if !m.Cancel(id) {
		t.Fatalf("Cancel(%s) = false, want true", id)
	}
	tk := waitStatus(t, m, id, StatusCanceled)
	if tk.Error == "" {
		t.Error("canceled task should carry an error note")
	}
}

func TestCancelUnknownOrFinished(t *testing.T) {
	m := NewManager(0)
	if m.Cancel("task-999") {
		t.Error("Cancel(unknown) = true, want false")
	}
	id := m.Submit("test", func(ctx context.Context) (string, error) { return "x", nil })
	waitStatus(t, m, id, StatusSucceeded)
	if m.Cancel(id) {
		t.Error("Cancel(finished) = true, want false")
	}
}

func TestRetryFailed(t *testing.T) {
	m := NewManager(0)
	calls := 0
	id := m.Submit("test", func(ctx context.Context) (string, error) {
		calls++
		return "", errors.New("boom")
	})
	waitStatus(t, m, id, StatusFailed)
	newID, ok := m.Retry(id)
	if !ok {
		t.Fatalf("Retry(%s) = false, want true", id)
	}
	if newID == id {
		t.Fatalf("Retry returned same id %s", id)
	}
	waitStatus(t, m, newID, StatusFailed)
	if calls != 2 {
		t.Errorf("job ran %d times, want 2 (retry must re-execute)", calls)
	}
}

func TestRetryUnknownOrRunning(t *testing.T) {
	m := NewManager(0)
	if _, ok := m.Retry("task-999"); ok {
		t.Error("Retry(unknown) = true, want false")
	}
	started := make(chan struct{})
	id := m.Submit("test", func(ctx context.Context) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	})
	<-started
	if _, ok := m.Retry(id); ok {
		t.Error("Retry(running) = true, want false")
	}
	m.Cancel(id)
}
