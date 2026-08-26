package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlePing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/ping", nil)
	rr := httptest.NewRecorder()
	handlePing(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Body.String(); got != "pong\n" {
		t.Fatalf("body = %q, want %q", got, "pong\n")
	}
}

func TestHandlePingMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/ping", nil)
	rr := httptest.NewRecorder()
	handlePing(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleVersion(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/version", nil)
	rr := httptest.NewRecorder()
	handleVersion(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["name"] != "cardinal-wings" {
		t.Fatalf("name = %v, want cardinal-wings", body["name"])
	}
}

func TestSplitRef(t *testing.T) {
	cases := []struct {
		path, ref, action string
	}{
		{"/v1/containers/abc123", "abc123", ""},
		{"/v1/containers/abc123/logs", "abc123", "logs"},
		{"/v1/containers/abc123/exec", "abc123", "exec"},
	}
	for _, c := range cases {
		ref, action := splitRef(c.path)
		if ref != c.ref || action != c.action {
			t.Errorf("splitRef(%q) = (%q,%q), want (%q,%q)", c.path, ref, action, c.ref, c.action)
		}
	}
}
