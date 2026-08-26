package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/animesao/cardinal-wings/internal/auth"
	"github.com/animesao/cardinal-wings/internal/config"
	"github.com/animesao/cardinal-wings/internal/runtime"
)

func TestFilterContainers(t *testing.T) {
	list := []runtime.Summary{
		{Name: "web", Image: "nginx:latest", Status: "running"},
		{Name: "db", Image: "postgres:16", Status: "exited"},
		{Name: "worker", Image: "redis:7", Status: "running"},
	}

	cases := []struct {
		name  string
		query string
		want  int
	}{
		{"no filter", "", 3},
		{"state running", "state=running", 2},
		{"state stopped", "state=stopped", 1},
		{"image substring", "image=postgres", 1},
		{"search name", "search=web", 1},
		{"search image case-insensitive", "search=REDIS", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := url.ParseQuery(tc.query)
			got := filterContainers(list, q)
			if len(got) != tc.want {
				t.Errorf("filterContainers(%q) = %d, want %d", tc.query, len(got), tc.want)
			}
		})
	}
}

func TestNodeRegistryRouting(t *testing.T) {
	// A fake remote client targeting a dead address is fine: byName only
	// looks up, it never dials.
	registerNodes([]nodeEntry{
		{name: "local", local: true, client: runtime.NewClient("http://127.0.0.1:1", "")},
		{name: "node-1", client: runtime.NewClient("http://127.0.0.1:2", "")},
	})

	// Default (no ?node=) resolves to the local client.
	req := httptest.NewRequest(http.MethodGet, "/v1/containers", nil)
	c, ok := clientFor(nil, req)
	if !ok {
		t.Fatal("clientFor default should resolve")
	}
	if c.Base() != "http://127.0.0.1:1" {
		t.Errorf("default client base = %s, want local", c.Base())
	}

	// Named node resolves.
	req2 := httptest.NewRequest(http.MethodGet, "/v1/containers?node=node-1", nil)
	c2, ok := clientFor(nil, req2)
	if !ok {
		t.Fatal("clientFor node-1 should resolve")
	}
	if c2.Base() != "http://127.0.0.1:2" {
		t.Errorf("node-1 base = %s, want remote", c2.Base())
	}

	// Unknown node writes a 404 error envelope.
	rr := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/v1/containers?node=ghost", nil)
	_, ok = clientFor(rr, req3)
	if ok {
		t.Fatal("clientFor ghost should not resolve")
	}
	if rr.Code != http.StatusNotFound {
		t.Errorf("unknown node status = %d, want 404", rr.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad error JSON: %v", err)
	}
	if _, ok := body["error"]; !ok {
		t.Errorf("expected error envelope, got %v", body)
	}
}

func TestHandleSelf(t *testing.T) {
	// Admin role in context.
	cfg := config.Default()
	cfg.Keys = []config.APIKey{{Name: "a", Key: "k", Role: config.RoleAdmin}}
	ctx := auth.WithRole(httptest.NewRequest(http.MethodGet, "/v1/self", nil).Context(), config.RoleAdmin)
	req := httptest.NewRequest(http.MethodGet, "/v1/self", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	handleSelf(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if body["role"] != "admin" || body["admin"] != true {
		t.Errorf("self = %v, want admin role", body)
	}

	// No role in context -> 401.
	req2 := httptest.NewRequest(http.MethodGet, "/v1/self", nil)
	rr2 := httptest.NewRecorder()
	handleSelf(rr2, req2)
	if rr2.Code != http.StatusUnauthorized {
		t.Errorf("no-role status = %d, want 401", rr2.Code)
	}
}

func TestPaginate(t *testing.T) {
	list := []runtime.Summary{
		{Name: "a", Status: "running"},
		{Name: "b", Status: "running"},
		{Name: "c", Status: "exited"},
		{Name: "d", Status: "exited"},
	}

	// Default page: limit 100, everything included.
	q, _ := url.ParseQuery("")
	p := paginate(list, q)
	if len(p.items) != 4 || p.limit != 100 || p.offset != 0 {
		t.Errorf("default page = %d items (limit %d, offset %d), want 4/100/0", len(p.items), p.limit, p.offset)
	}

	// limit=2
	q2, _ := url.ParseQuery("limit=2")
	p2 := paginate(list, q2)
	if len(p2.items) != 2 || p2.items[0].Name != "a" || p2.items[1].Name != "b" {
		t.Errorf("limit=2 page = %v", p2.items)
	}

	// offset=2
	q3, _ := url.ParseQuery("limit=2&offset=2")
	p3 := paginate(list, q3)
	if len(p3.items) != 2 || p3.items[0].Name != "c" {
		t.Errorf("offset=2 page = %v", p3.items)
	}

	// offset past end
	q4, _ := url.ParseQuery("offset=99")
	p4 := paginate(list, q4)
	if len(p4.items) != 0 {
		t.Errorf("offset past end = %v, want empty", p4.items)
	}

	// limit capped at 1000
	q5, _ := url.ParseQuery("limit=5000")
	p5 := paginate(list, q5)
	if p5.limit != 1000 {
		t.Errorf("limit cap = %d, want 1000", p5.limit)
	}
}

func TestCodeForStatus(t *testing.T) {
	if codeForStatus(http.StatusNotFound) != ErrNotFound {
		t.Error("404 should map to not_found")
	}
	if codeForStatus(http.StatusBadGateway) != ErrUpstream {
		t.Error("502 should map to upstream_error")
	}
	if codeForStatus(599) != ErrInternal {
		t.Error("unknown status should map to internal")
	}
}
