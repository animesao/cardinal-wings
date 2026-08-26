package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeCardinal is a minimal cardinal serve implementer for the Docker API
// subset that wings uses.
func fakeCardinal(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	containers := `[
	  {"Id":"ctr1","Names":["/web"],"Image":"nginx:alpine","State":"running",
	   "Ports":[{"PrivatePort":80,"PublicPort":8080,"Type":"tcp"}],
	   "Labels":{"app":"web"},"NetworkSettings":{"IPAddress":"10.0.2.2"}}
	]`
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(containers))
	})
	mux.HandleFunc("/containers/ctr1/json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
		  "Id":"ctr1","Name":"/web","Image":"nginx:alpine",
		  "State":{"Status":"running"},
		  "Config":{"Env":["PORT=80"]},
		  "HostConfig":{"RestartPolicy":{"Name":"always"},
		                "PortBindings":{"80/tcp":[{"HostPort":"8080"}]},
		                "Memory":268435456,"NanoCpus":1000000000},
		  "NetworkSettings":{"IPAddress":"10.0.2.2"},
		  "Mounts":[{"Type":"volume","Source":"data","Destination":"/data","RW":true}]
		}`))
	})
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("OK"))
	})
	actionCalled := false
	mux.HandleFunc("/containers/ctr1/stop", func(w http.ResponseWriter, r *http.Request) {
		actionCalled = true
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/containers/ctr1/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"container_id":"ctr1","memory_usage_bytes":123,"status":"running"}`))
	})
	mux.HandleFunc("/containers/create", func(w http.ResponseWriter, r *http.Request) {
		var body dockerCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.Image != "nginx:alpine" {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"Id":"new-ctr","Warnings":[]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	_ = actionCalled
	return srv
}

func TestClientListContainersAndInspect(t *testing.T) {
	c := NewClient(fakeCardinal(t).URL, "")
	ctx := context.Background()

	list, err := c.ListContainers(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 container, got %d", len(list))
	}
	s := list[0]
	if s.Name != "web" || s.Status != "running" || s.Image != "nginx:alpine" {
		t.Fatalf("summary wrong: %+v", s)
	}
	if len(s.Ports) != 1 || s.Ports[0] != "8080:80/tcp" {
		t.Fatalf("ports wrong: %v", s.Ports)
	}
	if s.IP != "10.0.2.2" {
		t.Fatalf("IP wrong: %q", s.IP)
	}

	d, err := c.Inspect(ctx, "ctr1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Restart != "always" || d.Memory != 268435456 || d.CPUs != 1.0 {
		t.Fatalf("detail wrong: %+v", d)
	}
	if len(d.Ports) != 1 || d.Ports[0].Host != 8080 || d.Ports[0].Container != 80 {
		t.Fatalf("detail ports wrong: %+v", d.Ports)
	}
	if len(d.Volumes) != 1 || d.Volumes[0].Source != "data" {
		t.Fatalf("volumes wrong: %+v", d.Volumes)
	}
}

func TestClientActionAndCreate(t *testing.T) {
	c := NewClient(fakeCardinal(t).URL, "")
	ctx := context.Background()

	if err := c.Action(ctx, "ctr1", "stop"); err != nil {
		t.Fatalf("action stop failed: %v", err)
	}
	if err := c.Action(ctx, "ctr1", "bogus"); err == nil {
		t.Fatal("bogus action should error")
	}

	res, err := c.Create(ctx, &CreateRequest{
		Name:  "nginx",
		Image: "nginx:alpine",
		Ports: []PortBinding{{Host: 8080, Container: 80, Protocol: "tcp"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != "new-ctr" {
		t.Fatalf("create id wrong: %q", res.ID)
	}
}

func TestClientStats(t *testing.T) {
	c := NewClient(fakeCardinal(t).URL, "")
	var out map[string]interface{}
	if err := c.Stats(context.Background(), "ctr1", &out); err != nil {
		t.Fatal(err)
	}
	if out["container_id"] != "ctr1" {
		t.Fatalf("stats wrong: %v", out)
	}
}

func TestClientErrorSurfacesStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/missing/json", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "")
	_, err := c.Inspect(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for missing container")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 in error, got %v", err)
	}
}
