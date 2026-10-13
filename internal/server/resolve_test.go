package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/animesao/cardinal-wings/internal/runtime"
)

// stubServe answers inspect for one named container: /containers/web/json
// reports the on-disk ID ctr-abc123, everything else 404s.
func stubServe(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/web/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Id":"ctr-abc123","Name":"/web","Image":"nginx:alpine","State":{"Status":"running"}}`))
	})
	mux.HandleFunc("/containers/ctr-abc123/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Id":"ctr-abc123","Name":"/web","Image":"nginx:alpine","State":{"Status":"running"}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func withLocalClient(t *testing.T, c *runtime.Client) {
	t.Helper()
	prev := defaultClient
	defaultClient = c
	t.Cleanup(func() { defaultClient = prev })
}

func TestResolveContainerIDByName(t *testing.T) {
	c := runtime.NewClient(stubServe(t).URL, "")
	if got := resolveContainerID(context.Background(), c, "web"); got != "ctr-abc123" {
		t.Fatalf("name resolved to %q, want ctr-abc123", got)
	}
}

func TestResolveContainerIDPassThrough(t *testing.T) {
	c := runtime.NewClient(stubServe(t).URL, "")
	// An ID resolves to itself.
	if got := resolveContainerID(context.Background(), c, "ctr-abc123"); got != "ctr-abc123" {
		t.Fatalf("id resolved to %q, want ctr-abc123", got)
	}
	// Unknown refs fall back to the raw value (previous behaviour).
	if got := resolveContainerID(context.Background(), c, "missing"); got != "missing" {
		t.Fatalf("unknown ref resolved to %q, want missing", got)
	}
	// Nil client and empty ref never touch the network.
	if got := resolveContainerID(context.Background(), nil, "web"); got != "web" {
		t.Fatalf("nil client changed ref to %q", got)
	}
	if got := resolveContainerID(context.Background(), c, ""); got != "" {
		t.Fatalf("empty ref changed to %q", got)
	}
}

func TestResolveLocalIDUsesDefaultClient(t *testing.T) {
	withLocalClient(t, runtime.NewClient(stubServe(t).URL, ""))
	if got := resolveLocalID("web"); got != "ctr-abc123" {
		t.Fatalf("resolveLocalID(web) = %q, want ctr-abc123", got)
	}
}

func TestResolveLocalIDWithoutClient(t *testing.T) {
	withLocalClient(t, nil)
	if got := resolveLocalID("web"); got != "web" {
		t.Fatalf("resolveLocalID without client = %q, want web", got)
	}
}
