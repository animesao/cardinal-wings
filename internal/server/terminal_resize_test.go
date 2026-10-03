package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionSizeDefaultAndSet(t *testing.T) {
	s := &terminalSession{subs: map[chan string]struct{}{}, done: make(chan struct{})}
	s.cols, s.rows = defaultTermCols, defaultTermRows
	if c, r := s.size(); c != 80 || r != 24 {
		t.Fatalf("default size = %dx%d, want 80x24", c, r)
	}
	if err := s.setSize(120, 40); err != nil {
		t.Fatalf("setSize: %v", err)
	}
	if c, r := s.size(); c != 120 || r != 40 {
		t.Fatalf("size = %dx%d, want 120x40", c, r)
	}
}

func TestSessionSizeRejectsBad(t *testing.T) {
	s := &terminalSession{subs: map[chan string]struct{}{}, done: make(chan struct{})}
	for _, tc := range [][2]int{{0, 24}, {80, 0}, {-1, 24}, {80, 5000}, {100000, 24}} {
		if err := s.setSize(tc[0], tc[1]); err == nil {
			t.Errorf("setSize(%d,%d) accepted, want error", tc[0], tc[1])
		}
	}
}

func TestParseWSInput(t *testing.T) {
	ok, cols, rows := parseWSInput([]byte(`{"type":"resize","cols":120,"rows":40}`))
	if !ok || cols != 120 || rows != 40 {
		t.Fatalf("resize msg = (%v,%d,%d), want (true,120,40)", ok, cols, rows)
	}
	for _, msg := range []string{"hello", `{"type":"resize"}`, `{"type":"other","cols":1,"rows":1}`, "{bad json", `{"type":"resize","cols":0,"rows":24}`} {
		if ok, _, _ := parseWSInput([]byte(msg)); ok {
			t.Errorf("msg %q parsed as resize, want stdin", msg)
		}
	}
}

func TestTerminalResizeRoutingLocked(t *testing.T) {
	ref, action := splitRef("/v1/containers/abc/terminal/resize")
	if ref != "abc" || action != "terminal/resize" {
		t.Fatalf("splitRef = (%q,%q)", ref, action)
	}
	if !isMutating(action, http.MethodPost) {
		t.Error("terminal/resize POST should be mutating (admin)")
	}
}

func TestHandleTerminalResize(t *testing.T) {
	id := "resize-test-ctr"
	s := &terminalSession{id: id, subs: map[chan string]struct{}{}, done: make(chan struct{})}
	s.cols, s.rows = defaultTermCols, defaultTermRows
	terminals.sessions[id] = s
	defer delete(terminals.sessions, id)

	body, _ := json.Marshal(map[string]int{"cols": 100, "rows": 30})
	req := httptest.NewRequest(http.MethodPost, "/v1/containers/"+id+"/terminal/resize", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handleTerminalResize(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("resize = %d (%s), want 200", rr.Code, rr.Body.String())
	}
	if c, r := s.size(); c != 100 || r != 30 {
		t.Fatalf("session size = %dx%d, want 100x30", c, r)
	}

	// Unknown session → 404.
	req = httptest.NewRequest(http.MethodPost, "/v1/containers/nope-session/terminal/resize", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	handleTerminalResize(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown session resize = %d, want 404", rr.Code)
	}

	// Bad dims → 400.
	req = httptest.NewRequest(http.MethodPost, "/v1/containers/"+id+"/terminal/resize", bytes.NewReader([]byte(`{"cols":0,"rows":24}`)))
	rr = httptest.NewRecorder()
	handleTerminalResize(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad dims resize = %d, want 400", rr.Code)
	}
}
