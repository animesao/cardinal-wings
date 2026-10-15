package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/animesao/cardinal-wings/internal/auth"
	"github.com/animesao/cardinal-wings/internal/config"
)

func TestMetricsRespectsKnob(t *testing.T) {
	prev := wingCfg
	defer func() { wingCfg = prev }()

	readonly := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)
		return r.WithContext(auth.WithRole(r.Context(), config.RoleReadOnly))
	}

	wingCfg = &config.Config{Remote: config.Remote{MetricsRequiresAuth: true}}
	rr := httptest.NewRecorder()
	handleMetrics(rr, readonly())
	if rr.Code != http.StatusForbidden {
		t.Fatalf("knob on: readonly got %d, want 403", rr.Code)
	}

	wingCfg = &config.Config{Remote: config.Remote{MetricsRequiresAuth: false}}
	rr = httptest.NewRecorder()
	handleMetrics(rr, readonly())
	if rr.Code != http.StatusOK {
		t.Fatalf("knob off: readonly got %d, want 200", rr.Code)
	}
}

func TestRegistryIsLocal(t *testing.T) {
	defer registerNodes(nil)
	registerNodes([]nodeEntry{{name: "local", local: true}, {name: "remote", local: false}})
	if !registry.isLocal("local") {
		t.Fatal("local not recognized")
	}
	if registry.isLocal("remote") {
		t.Fatal("remote recognized as local")
	}
	if registry.isLocal("missing") {
		t.Fatal("missing recognized as local")
	}
}
