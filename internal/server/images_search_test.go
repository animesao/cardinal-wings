package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestImageSearchRouteRegistered(t *testing.T) {
	mux := http.NewServeMux()
	imageRoutes(mux, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/images/search?q=nginx", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code == http.StatusNotFound {
		t.Fatalf("route /v1/images/search not registered (got 404)")
	}
}

func TestImageSearchRequiresQuery(t *testing.T) {
	mux := http.NewServeMux()
	imageRoutes(mux, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/images/search", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("GET /v1/images/search without q = %d, want 400", rr.Code)
	}
}
