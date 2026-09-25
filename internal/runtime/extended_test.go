package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fakeExtended(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/ctr1/top", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Titles":["PID","CMD"],"Processes":["1 sleep"]}`))
	})
	mux.HandleFunc("/containers/ctr1/wait", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"StatusCode":0}`))
	})
	mux.HandleFunc("/containers/ctr1/changes", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	mux.HandleFunc("/containers/ctr1/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-tar")
		_, _ = w.Write([]byte("tar-bytes"))
	})
	mux.HandleFunc("/containers/ctr1/rename", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/images/nginx:latest/history", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"ID":"sha256:abc"}]`))
	})
	mux.HandleFunc("/images/nginx:latest/get", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-tar")
		_, _ = w.Write([]byte("img-tar"))
	})
	mux.HandleFunc("/system/prune", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/cluster/replicas", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"container_id":"c1"}`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	})
	mux.HandleFunc("/cluster/replicas/c1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"removed"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestExtendedEndpoints(t *testing.T) {
	c := NewClient(fakeExtended(t).URL, "")
	ctx := context.Background()

	if _, err := c.Top(ctx, "ctr1", "aux"); err != nil {
		t.Fatalf("top: %v", err)
	}
	if _, err := c.Wait(ctx, "ctr1"); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if _, err := c.Changes(ctx, "ctr1"); err != nil {
		t.Fatalf("changes: %v", err)
	}
	if err := c.Rename(ctx, "ctr1", "web2"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := c.History(ctx, "nginx:latest"); err != nil {
		t.Fatalf("history: %v", err)
	}
	if _, err := c.Prune(ctx); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, err := c.ReplicaCreate(ctx, map[string]interface{}{"image": "nginx:alpine"}); err != nil {
		t.Fatalf("replica create: %v", err)
	}
	if _, err := c.ReplicaRemove(ctx, "c1"); err != nil {
		t.Fatalf("replica remove: %v", err)
	}
	resp, err := c.Proxy(ctx, "GET", "/containers/ctr1/export")
	if err != nil {
		t.Fatalf("proxy export: %v", err)
	}
	resp.Body.Close()
	resp2, err := c.Proxy(ctx, "GET", "/images/nginx:latest/get")
	if err != nil {
		t.Fatalf("proxy image get: %v", err)
	}
	resp2.Body.Close()
}
