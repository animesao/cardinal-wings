package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/animesao/cardinal-wings/internal/agent"
	"github.com/animesao/cardinal-wings/internal/auth"
)

// volumesRoutes mounts local-only volume endpoints. Like networks, volumes
// have no serve API, so wings delegates to the local `cardinal volume` CLI;
// remote nodes (?node=) are rejected with 501.
func volumesRoutes(mux *http.ServeMux, mw *auth.Middleware) {
	mux.HandleFunc("/v1/volumes", func(w http.ResponseWriter, r *http.Request) {
		if remoteNode(r) {
			writeErr(w, http.StatusNotImplemented, ErrUpstream, "volumes are local-only (no serve API on remote nodes)")
			return
		}
		switch r.Method {
		case http.MethodGet:
			out, err := agent.VolumeLs(r.Context())
			if err != nil {
				writeErr(w, http.StatusBadGateway, ErrUpstream, "volume ls: %s", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"output": out})
		case http.MethodPost:
			var req struct {
				Name   string            `json:"name"`
				Driver string            `json:"driver,omitempty"`
				Labels map[string]string `json:"labels,omitempty"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
				return
			}
			mw.AdminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				out, err := agent.VolumeCreate(r.Context(), req.Name, req.Driver, req.Labels)
				if err != nil {
					writeErr(w, http.StatusBadGateway, ErrUpstream, "volume create: %s", err.Error())
					return
				}
				writeJSON(w, http.StatusCreated, map[string]string{"name": req.Name, "output": out})
			})).ServeHTTP(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	mux.HandleFunc("/v1/volumes/", func(w http.ResponseWriter, r *http.Request) {
		if remoteNode(r) {
			writeErr(w, http.StatusNotImplemented, ErrUpstream, "volumes are local-only (no serve API on remote nodes)")
			return
		}
		name, _ := splitVolumePath(r.URL.Path)
		if name == "" {
			writeError(w, http.StatusNotFound, "missing volume name")
			return
		}
		if name == "prune" {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			mw.AdminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				out, err := agent.VolumePrune(r.Context())
				if err != nil {
					writeErr(w, http.StatusBadGateway, ErrUpstream, "volume prune: %s", err.Error())
					return
				}
				writeJSON(w, http.StatusOK, map[string]string{"output": out})
			})).ServeHTTP(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			out, err := agent.VolumeInspect(r.Context(), name)
			if err != nil {
				writeErr(w, http.StatusBadGateway, ErrUpstream, "volume inspect %s: %s", name, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"name": name, "output": out})
		case http.MethodDelete:
			mw.AdminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := agent.VolumeRemove(r.Context(), name); err != nil {
					writeErr(w, http.StatusBadGateway, ErrUpstream, "volume rm %s: %s", name, err.Error())
					return
				}
				writeJSON(w, http.StatusOK, map[string]string{"removed": name})
			})).ServeHTTP(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

// splitVolumePath splits /v1/volumes/{name}.
func splitVolumePath(path string) (name, action string) {
	name = strings.TrimPrefix(path, "/v1/volumes/")
	return name, ""
}
