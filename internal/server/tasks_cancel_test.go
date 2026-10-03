package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSplitTaskPath(t *testing.T) {
	cases := []struct{ path, id, action string }{
		{"/v1/tasks/task-1", "task-1", ""},
		{"/v1/tasks/task-1/cancel", "task-1", "cancel"},
		{"/v1/tasks/task-1/retry", "task-1", "retry"},
		{"/v1/tasks/task-1/bogus", "task-1", "bogus"},
	}
	for _, c := range cases {
		id, action := splitTaskPath(c.path)
		if id != c.id || action != c.action {
			t.Errorf("splitTaskPath(%q) = (%q,%q), want (%q,%q)", c.path, id, action, c.id, c.action)
		}
	}
}

func TestTaskActionRouting(t *testing.T) {
	mux := http.NewServeMux()
	tasksRoutes(mux, nil)
	// Unknown action → 404 without touching auth.
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/task-1/bogus", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("bogus action = %d, want 404", rr.Code)
	}
	// Wrong method on cancel → 405 without touching auth.
	req = httptest.NewRequest(http.MethodGet, "/v1/tasks/task-1/cancel", nil)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET cancel = %d, want 405", rr.Code)
	}
	// Unknown task id on poll → 404.
	req = httptest.NewRequest(http.MethodGet, "/v1/tasks/task-does-not-exist", nil)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("unknown task = %d, want 404", rr.Code)
	}
}
