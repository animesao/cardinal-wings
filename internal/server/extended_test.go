package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsMutatingNewActions(t *testing.T) {
	mutating := []string{"rename", "commit", "ports/add", "ports/remove", "wait"}
	for _, a := range mutating {
		if !isMutating(a, http.MethodPost) {
			t.Errorf("isMutating(%q, POST) = false, want true", a)
		}
	}
	reads := []string{"top", "changes", "export", "ports"}
	for _, a := range reads {
		if isMutating(a, http.MethodGet) {
			t.Errorf("isMutating(%q, GET) = true, want false (read)", a)
		}
	}
}

func TestSplitRefNewActions(t *testing.T) {
	cases := []struct{ path, ref, action string }{
		{"/v1/containers/abc/rename", "abc", "rename"},
		{"/v1/containers/abc/top", "abc", "top"},
		{"/v1/containers/abc/wait", "abc", "wait"},
		{"/v1/containers/abc/changes", "abc", "changes"},
		{"/v1/containers/abc/export", "abc", "export"},
		{"/v1/containers/abc/commit", "abc", "commit"},
		{"/v1/containers/abc/ports/add", "abc", "ports/add"},
	}
	for _, c := range cases {
		ref, action := splitRef(c.path)
		if ref != c.ref || action != c.action {
			t.Errorf("splitRef(%q) = (%q,%q), want (%q,%q)", c.path, ref, action, c.ref, c.action)
		}
	}
}

func TestSplitImageNewActions(t *testing.T) {
	action, ref := splitImagePath("/v1/images/nginx:latest/history")
	if action != "history" || ref != "nginx:latest" {
		t.Errorf("splitImagePath history = (%q,%q)", action, ref)
	}
	action, ref = splitImagePath("/v1/images/nginx:latest/verify")
	if action != "verify" || ref != "nginx:latest" {
		t.Errorf("splitImagePath verify = (%q,%q)", action, ref)
	}
	if !isImageMutating("verify", http.MethodPost) {
		t.Error("verify POST should be mutating (admin)")
	}
	if isImageMutating("history", http.MethodGet) {
		t.Error("history GET should not be mutating")
	}
}

func TestSystemRoutesRegistered(t *testing.T) {
	mux := http.NewServeMux()
	systemRoutes(mux, nil)
	for _, p := range []string{"/v1/system/df", "/v1/system/prune", "/v1/info"} {
		req := httptest.NewRequest(http.MethodOptions, p, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code == http.StatusNotFound {
			t.Errorf("route %s not registered", p)
		}
	}
}
