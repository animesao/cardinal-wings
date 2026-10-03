package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSplitNetworkPath(t *testing.T) {
	id, action := splitNetworkPath("/v1/networks/mynet")
	if id != "mynet" || action != "" {
		t.Fatalf("got %q %q", id, action)
	}
}

func TestSplitVolumePath(t *testing.T) {
	id, action := splitVolumePath("/v1/volumes/data")
	if id != "data" || action != "" {
		t.Fatalf("got %q %q", id, action)
	}
	id, action = splitVolumePath("/v1/volumes/prune")
	if id != "prune" || action != "" {
		t.Fatalf("got %q %q", id, action)
	}
}

func TestNetworksRoutesRegistered(t *testing.T) {
	mux := http.NewServeMux()
	networksRoutes(mux, nil)
	volumesRoutes(mux, nil)
	for _, p := range []string{"/v1/networks", "/v1/volumes"} {
		req := httptest.NewRequest(http.MethodOptions, p, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code == http.StatusNotFound {
			t.Errorf("route %s not registered", p)
		}
	}
}

func TestNetvolRemoteNodeRejected(t *testing.T) {
	mux := http.NewServeMux()
	networksRoutes(mux, nil)
	// Remote node: no serve API for networks → 501, not a panic.
	req := httptest.NewRequest(http.MethodGet, "/v1/networks?node=node-1", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotImplemented {
		t.Errorf("remote networks = %d, want 501", rr.Code)
	}
}
