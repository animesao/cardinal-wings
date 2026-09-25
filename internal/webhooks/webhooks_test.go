package webhooks

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/animesao/cardinal-wings/internal/config"
)

func TestMatches(t *testing.T) {
	cases := []struct {
		list  []string
		event string
		want  bool
	}{
		{[]string{"task.completed"}, "task.completed", true},
		{[]string{"task.completed"}, "container.event", false},
		{[]string{"*"}, "anything", true},
		{[]string{"a", "b"}, "b", true},
	}
	for _, c := range cases {
		if got := matches(c.list, c.event); got != c.want {
			t.Errorf("matches(%v, %q) = %v, want %v", c.list, c.event, got, c.want)
		}
	}
}

func TestFireDelivers(t *testing.T) {
	received := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Webhook-Secret") != "s3cret" {
			t.Errorf("missing secret header")
		}
		received <- r.Header.Get("X-Webhook-Secret")
	}))
	defer srv.Close()

	n := New([]config.Webhook{{Name: "t", URL: srv.URL, Events: []string{"task.completed"}, Secret: "s3cret", Enabled: true}})
	n.Fire("task.completed", map[string]string{"id": "t1"})

	select {
	case got := <-received:
		if got != "s3cret" {
			t.Errorf("secret = %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("webhook not delivered")
	}
}

func TestFireSkipsUnsubscribed(t *testing.T) {
	n := New([]config.Webhook{{Name: "t", URL: "http://127.0.0.1:1", Events: []string{"other"}, Enabled: true}})
	// Should not panic or block even though the URL is dead.
	n.Fire("task.completed", nil)
	time.Sleep(50 * time.Millisecond) // let any (nonexistent) goroutine spin down
}

func TestFireDisabledHookSkipped(t *testing.T) {
	n := New([]config.Webhook{{Name: "t", URL: "http://127.0.0.1:1", Events: []string{"*"}, Enabled: false}})
	n.Fire("task.completed", nil)
	time.Sleep(50 * time.Millisecond)
}
