package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/animesao/cardinal-wings/internal/agent"
	"github.com/animesao/cardinal-wings/internal/auth"
)

// servicesRoutes mounts /v1/services and /v1/functions. These delegate to the
// `cardinal service` / `cardinal fn` CLI on the local node (the orchestrator
// lives in cardinal; wings just exposes it to the panel).
func servicesRoutes(mux *http.ServeMux, mw *auth.Middleware) {
	mux.HandleFunc("/v1/services", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			out, err := agent.ServiceList(r.Context())
			if err != nil {
				writeErr(w, http.StatusBadGateway, ErrUpstream, "service ls: %s", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"services": out})
		case http.MethodPost:
			mw.AdminOnly(http.HandlerFunc(handleServiceCreate)).ServeHTTP(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	mux.HandleFunc("/v1/services/", func(w http.ResponseWriter, r *http.Request) {
		action, name := splitServicePath(r.URL.Path)
		if action == "scale" {
			mw.AdminOnly(http.HandlerFunc(handleServiceScale)).ServeHTTP(w, r)
			return
		}
		if action == "remove" || (action == "" && r.Method == http.MethodDelete) {
			mw.AdminOnly(http.HandlerFunc(handleServiceRemove)).ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusNotFound, "service not found: "+name)
	})

	mux.HandleFunc("/v1/functions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			out, err := agent.FnList(r.Context())
			if err != nil {
				writeErr(w, http.StatusBadGateway, ErrUpstream, "fn ls: %s", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"functions": out})
		case http.MethodPost:
			mw.AdminOnly(http.HandlerFunc(handleFnDeploy)).ServeHTTP(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	mux.HandleFunc("/v1/functions/", func(w http.ResponseWriter, r *http.Request) {
		action, name := splitFnPath(r.URL.Path)
		switch {
		case action == "" && r.Method == http.MethodPost:
			mw.AdminOnly(http.HandlerFunc(handleFnInvoke)).ServeHTTP(w, r)
		case action == "invoke" && r.Method == http.MethodPost:
			mw.AdminOnly(http.HandlerFunc(handleFnInvoke)).ServeHTTP(w, r)
		case action == "remove" && r.Method == http.MethodDelete:
			mw.AdminOnly(http.HandlerFunc(handleFnRemove)).ServeHTTP(w, r)
		default:
			writeError(w, http.StatusNotFound, "function not found: "+name)
		}
	})
}

// splitServicePath splits /v1/services/{name}[/{action}].
func splitServicePath(path string) (action, name string) {
	trimmed := strings.TrimPrefix(path, "/v1/services/")
	parts := strings.SplitN(trimmed, "/", 2)
	name = parts[0]
	if len(parts) == 2 {
		action = parts[1]
	}
	return action, name
}

// splitFnPath splits /v1/functions/{name}[/{action}].
func splitFnPath(path string) (action, name string) {
	trimmed := strings.TrimPrefix(path, "/v1/functions/")
	parts := strings.SplitN(trimmed, "/", 2)
	name = parts[0]
	if len(parts) == 2 {
		action = parts[1]
	}
	return action, name
}

func handleServiceCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string   `json:"name"`
		Image    string   `json:"image"`
		Replicas int      `json:"replicas"`
		Ports    []string `json:"ports,omitempty"`
		Env      []string `json:"env,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Name == "" || req.Image == "" {
		writeError(w, http.StatusBadRequest, "name and image required")
		return
	}
	if err := agent.ServiceCreate(r.Context(), req.Name, req.Image, req.Replicas, req.Ports, req.Env); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "service create: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": req.Name, "status": "created"})
}

func handleServiceScale(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, name := splitServicePath(r.URL.Path)
	var req struct {
		Replicas int `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := agent.ServiceScale(r.Context(), name, req.Replicas); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "service scale: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"name": name, "replicas": req.Replicas})
}

func handleServiceRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, name := splitServicePath(r.URL.Path)
	if err := agent.ServiceRemove(r.Context(), name); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "service rm: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"removed": name})
}

func handleFnDeploy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Image string `json:"image"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Name == "" || req.Image == "" {
		writeError(w, http.StatusBadRequest, "name and image required")
		return
	}
	if err := agent.FnDeploy(r.Context(), req.Name, req.Image); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "fn deploy: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": req.Name, "status": "deployed"})
}

func handleFnInvoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, name := splitFnPath(r.URL.Path)
	var req struct {
		Data string `json:"data,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	out, err := agent.FnInvoke(r.Context(), name, req.Data)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "fn call: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"name": name, "output": out})
}

func handleFnRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, name := splitFnPath(r.URL.Path)
	if err := agent.FnRemove(r.Context(), name); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "fn rm: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"removed": name})
}
