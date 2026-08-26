package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/animesao/cardinal-wings/internal/auth"
	"github.com/animesao/cardinal-wings/internal/runtime"
)

// containerRoutes mounts the Phase 1 container endpoints against the runtime
// client for the local node.
func containerRoutes(mux *http.ServeMux, mw *auth.Middleware) {
	mux.HandleFunc("/v1/containers", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			list, err := defaultClient.ListContainers(r.Context(), r.URL.Query().Get("all") == "1")
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, list)
		case http.MethodPost:
			mw.AdminOnly(http.HandlerFunc(handleContainerCreate)).ServeHTTP(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
	mux.HandleFunc("/v1/containers/", func(w http.ResponseWriter, r *http.Request) {
		action, _ := splitRef(r.URL.Path)
		if isMutating(action, r.Method) {
			mw.AdminOnly(http.HandlerFunc(handleContainerRef)).ServeHTTP(w, r)
			return
		}
		handleContainerRef(w, r)
	})
}

// splitRef returns the container id and trailing action from a ref path.
func splitRef(path string) (ref, action string) {
	trimmed := strings.TrimPrefix(path, "/v1/containers/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 0 {
		return "", ""
	}
	ref = parts[0]
	if len(parts) == 2 {
		action = parts[1]
	}
	return ref, action
}

func isMutating(action, method string) bool {
	switch action {
	case "start", "stop", "restart", "kill", "remove", "exec":
		return true
	}
	return action == "" && method == http.MethodDelete
}

func handleContainerCreate(w http.ResponseWriter, r *http.Request) {
	var req runtime.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	res, err := defaultClient.Create(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func handleContainerRef(w http.ResponseWriter, r *http.Request) {
	ref, action := splitRef(r.URL.Path)
	if ref == "" {
		writeError(w, http.StatusNotFound, "missing container id")
		return
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		d, err := defaultClient.Inspect(r.Context(), ref)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, d)

	case action == "stats" && r.Method == http.MethodGet:
		var s interface{}
		if err := defaultClient.Stats(r.Context(), ref, &s); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, s)

	case action == "logs" && r.Method == http.MethodGet:
		handleContainerLogs(w, r, ref)

	case action == "exec" && r.Method == http.MethodPost:
		handleContainerExec(w, r, ref)

	case (action == "start" || action == "stop" || action == "restart" || action == "kill") && r.Method == http.MethodPost:
		if err := defaultClient.Action(r.Context(), ref, action); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": ref, "action": action, "ok": "true"})

	case action == "" && r.Method == http.MethodDelete:
		force := r.URL.Query().Get("force") == "1"
		if err := defaultClient.Remove(r.Context(), ref, force); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"removed": ref})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
