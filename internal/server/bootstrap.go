package server

import (
	"context"
	"net/http"
	"time"

	"github.com/animesao/cardinal-wings/internal/agent"
	"github.com/animesao/cardinal-wings/internal/auth"
)

// bootstrapRoutes exposes a narrow administrative operation instead of a
// general shell: it installs Cardinal's persistent boot supervisor and starts
// it immediately. This is idempotent and safe to call after node setup.
func bootstrapRoutes(mux *http.ServeMux, mw *auth.Middleware) {
	mux.HandleFunc("/v1/bootstrap/ensure", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		mw.AdminOnly(http.HandlerFunc(handleBootstrapEnsure)).ServeHTTP(w, r)
	})
}

func handleBootstrapEnsure(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := agent.BootstrapEnsure(ctx); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "bootstrap ensure: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"service": "cardinal-bootstrap",
		"enabled": true,
		"started": true,
	})
}
