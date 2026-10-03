package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/animesao/cardinal-wings/internal/agent"
	"github.com/animesao/cardinal-wings/internal/auth"
)

// networksRoutes mounts local-only network endpoints. cardinal serve has no
// network API, so wings delegates to the local `cardinal network` CLI;
// remote nodes (?node=) are rejected with 501.
func networksRoutes(mux *http.ServeMux, mw *auth.Middleware) {
	mux.HandleFunc("/v1/networks", func(w http.ResponseWriter, r *http.Request) {
		if remoteNode(r) {
			writeErr(w, http.StatusNotImplemented, ErrUpstream, "networks are local-only (no serve API on remote nodes)")
			return
		}
		switch r.Method {
		case http.MethodGet:
			out, err := agent.NetworkLs(r.Context())
			if err != nil {
				writeErr(w, http.StatusBadGateway, ErrUpstream, "network ls: %s", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"output": out})
		case http.MethodPost:
			var req struct {
				Name   string `json:"name"`
				Subnet string `json:"subnet,omitempty"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
				return
			}
			mw.AdminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				out, err := agent.NetworkCreate(r.Context(), req.Name, req.Subnet)
				if err != nil {
					writeErr(w, http.StatusBadGateway, ErrUpstream, "network create: %s", err.Error())
					return
				}
				writeJSON(w, http.StatusCreated, map[string]string{"name": req.Name, "output": out})
			})).ServeHTTP(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	mux.HandleFunc("/v1/networks/", func(w http.ResponseWriter, r *http.Request) {
		if remoteNode(r) {
			writeErr(w, http.StatusNotImplemented, ErrUpstream, "networks are local-only (no serve API on remote nodes)")
			return
		}
		name, _ := splitNetworkPath(r.URL.Path)
		if name == "" {
			writeError(w, http.StatusNotFound, "missing network name")
			return
		}
		switch r.Method {
		case http.MethodGet:
			out, err := agent.NetworkInspect(r.Context(), name)
			if err != nil {
				writeErr(w, http.StatusBadGateway, ErrUpstream, "network inspect %s: %s", name, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"name": name, "output": out})
		case http.MethodDelete:
			mw.AdminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := agent.NetworkRemove(r.Context(), name); err != nil {
					writeErr(w, http.StatusBadGateway, ErrUpstream, "network rm %s: %s", name, err.Error())
					return
				}
				writeJSON(w, http.StatusOK, map[string]string{"removed": name})
			})).ServeHTTP(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

// splitNetworkPath splits /v1/networks/{name}.
func splitNetworkPath(path string) (name, action string) {
	name = strings.TrimPrefix(path, "/v1/networks/")
	return name, ""
}

// remoteNode reports whether the request targets a non-local node.
func remoteNode(r *http.Request) bool {
	node := r.URL.Query().Get("node")
	return node != "" && node != "local"
}
