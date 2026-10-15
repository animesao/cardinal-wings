package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateVolumeSource(t *testing.T) {
	for _, ok := range []string{"srv-abc123", "data", "my.volume-1", "pg_data"} {
		if err := validateVolumeSource(ok); err != nil {
			t.Errorf("expected %q to be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "/etc", "/etc:/data", "evil:/share", "../x", ".hidden", "-dash", "a/b", `c:\win`, "with space", "semi;colon"} {
		if err := validateVolumeSource(bad); err == nil {
			t.Errorf("expected %q to be rejected", bad)
		}
	}
}

func TestValidContainerName(t *testing.T) {
	for _, ok := range []string{"srv-abc", "web2", "a", "my.container-1_x"} {
		if !validContainerName(ok) {
			t.Errorf("expected %q to be accepted", ok)
		}
	}
	for _, bad := range []string{"", "../x", "a/b", "x?force=1", "x&y", " space", ".dot", "-dash", strings.Repeat("a", 129)} {
		if validContainerName(bad) {
			t.Errorf("expected %q to be rejected", bad)
		}
	}
}

func TestRedactHostPaths(t *testing.T) {
	dir := wingsDataDir()
	if dir == "" || dir == "/" {
		t.Skip("no data dir")
	}
	msg := redactHostPaths("mount failed at " + dir + "/overlay/x: permission denied")
	if strings.Contains(msg, dir) {
		t.Fatalf("data dir leaked: %q", msg)
	}
	if !strings.Contains(msg, "[data-dir]") {
		t.Fatalf("placeholder missing: %q", msg)
	}
	if got := redactHostPaths("plain message"); got != "plain message" {
		t.Fatalf("clean message altered: %q", got)
	}
}

func TestBodyLimitMiddleware(t *testing.T) {
	// Reads the whole body: nil error means the body survived the middleware.
	readAll := func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 16)
		for {
			_, err := r.Body.Read(buf)
			if err != nil {
				break
			}
		}
		w.Write([]byte("ok"))
	}

	// Small JSON passes untouched.
	req := httptest.NewRequest(http.MethodPost, "/v1/containers", strings.NewReader(`{"a":1}`))
	rr := httptest.NewRecorder()
	bodyLimitMiddleware(http.HandlerFunc(readAll)).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("small body = %d, want 200", rr.Code)
	}

	// Oversized JSON on an ordinary route must hit the cap.
	big := strings.NewReader(strings.Repeat("x", maxJSONBodyBytes+1))
	req = httptest.NewRequest(http.MethodPost, "/v1/containers", big)
	rr = httptest.NewRecorder()
	hitCap := false
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1<<20)
		for {
			_, err := r.Body.Read(buf)
			if err == nil {
				continue
			}
			hitCap = strings.Contains(err.Error(), "request body too large")
			break
		}
		w.Write([]byte("ok"))
	})
	bodyLimitMiddleware(probe).ServeHTTP(rr, req)
	if !hitCap {
		t.Fatal("oversized body was not capped")
	}

	// Streaming uploads are exempt (they enforce their own limits).
	big2 := strings.NewReader(strings.Repeat("x", 1024))
	req = httptest.NewRequest(http.MethodPost, "/v1/containers/abc/backup", big2)
	rr = httptest.NewRecorder()
	bodyLimitMiddleware(http.HandlerFunc(readAll)).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("exempt route = %d, want 200", rr.Code)
	}
}
