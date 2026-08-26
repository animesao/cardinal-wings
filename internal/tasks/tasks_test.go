package tasks

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSubmitAndPoll(t *testing.T) {
	m := NewManager(0)
	id := m.Submit("test", func(ctx context.Context) (string, error) {
		return "hello", nil
	})

	// Poll until succeeded.
	deadline := time.Now().Add(2 * time.Second)
	for {
		tk, ok := m.Get(id)
		if !ok {
			t.Fatal("task missing")
		}
		if tk.Status == StatusSucceeded {
			if tk.Output != "hello" {
				t.Errorf("output = %q, want hello", tk.Output)
			}
			if tk.Kind != "test" {
				t.Errorf("kind = %q, want test", tk.Kind)
			}
			break
		}
		if tk.Status == StatusFailed {
			t.Fatalf("task failed: %s", tk.Error)
		}
		if time.Now().After(deadline) {
			t.Fatalf("task stuck in %s", tk.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTaskFailure(t *testing.T) {
	m := NewManager(0)
	want := errors.New("boom")
	id := m.Submit("test", func(ctx context.Context) (string, error) {
		return "", want
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		tk, _ := m.Get(id)
		if tk.Status == StatusFailed {
			if tk.Error != want.Error() {
				t.Errorf("error = %q, want %q", tk.Error, want.Error())
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("task stuck in %s", tk.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPrune(t *testing.T) {
	m := NewManager(50 * time.Millisecond)
	m.Submit("test", func(ctx context.Context) (string, error) { return "x", nil })
	time.Sleep(100 * time.Millisecond)
	m.Prune()
	if got := len(m.List()); got != 0 {
		t.Errorf("pruned list len = %d, want 0", got)
	}
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/tasks.json"

	m := NewManager(0).WithPersistence(path)
	m.Submit("blueprint_install", func(ctx context.Context) (string, error) {
		return "done", nil
	})
	// Wait for the task to finish and persist.
	deadline := time.Now().Add(2 * time.Second)
	for {
		list := m.List()
		if len(list) == 1 && list[0].Status == StatusSucceeded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("task did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// New manager on the same file should see the finished task.
	m2 := NewManager(0).WithPersistence(path)
	list := m2.List()
	if len(list) != 1 {
		t.Fatalf("restored tasks = %d, want 1", len(list))
	}
	if list[0].Kind != "blueprint_install" || list[0].Output != "done" {
		t.Errorf("restored task = %+v", list[0])
	}
}

func TestListNewestFirst(t *testing.T) {
	m := NewManager(0)
	a := m.Submit("a", func(ctx context.Context) (string, error) { return "", nil })
	b := m.Submit("b", func(ctx context.Context) (string, error) { return "", nil })
	list := m.List()
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}
	if list[0].ID != b || list[1].ID != a {
		t.Errorf("order = [%s %s], want [%s %s]", list[0].ID, list[1].ID, b, a)
	}
}
