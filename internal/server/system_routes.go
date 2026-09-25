package server

import (
	"net/http"

	"github.com/animesao/cardinal-wings/internal/agent"
	"github.com/animesao/cardinal-wings/internal/auth"
)

// systemRoutes mounts host-level endpoints: disk usage, prune and a raw
// cardinal /info passthrough. All of them accept ?node= like the rest.
func systemRoutes(mux *http.ServeMux, mw *auth.Middleware) {
	mux.HandleFunc("/v1/system/df", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if r.URL.Query().Get("node") != "" && r.URL.Query().Get("node") != "local" {
			c, ok := clientFor(w, r)
			if !ok {
				return
			}
			info, err := c.Info(r.Context())
			if err != nil {
				writeErr(w, http.StatusBadGateway, ErrUpstream, "node info: %s", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, info)
			return
		}
		out, err := agent.SystemDF(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, ErrUpstream, "system df: %s", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"output": out})
	})

	mux.HandleFunc("/v1/system/prune", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		mw.AdminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, ok := clientFor(w, r)
			if !ok {
				return
			}
			// Prefer the daemon API; fall back to the CLI when it is missing.
			if out, err := c.Prune(r.Context()); err == nil {
				writeJSON(w, http.StatusOK, out)
				return
			}
			out, err := agent.SystemPrune(r.Context())
			if err != nil {
				writeErr(w, http.StatusBadGateway, ErrUpstream, "system prune: %s", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"output": out})
		})).ServeHTTP(w, r)
	})

	mux.HandleFunc("/v1/info", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		c, ok := clientFor(w, r)
		if !ok {
			return
		}
		info, err := c.Info(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, ErrUpstream, "info: %s", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, info)
	})
}
