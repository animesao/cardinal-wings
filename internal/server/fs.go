package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/animesao/cardinal-wings/internal/agent"
)

// decodeJSON decodes a JSON body with a size cap.
func decodeJSON(r *http.Request, v interface{}) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

// handleFs serves /v1/containers/{id}/fs/{ls|cat|tree}. The requested path is
// passed via ?path= (paths inside the container may contain slashes, so a
// query param is safer than another path segment).
func handleFs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ref, action := splitRef(r.URL.Path)
	if ref == "" {
		writeError(w, http.StatusNotFound, "missing container id")
		return
	}
	path := r.URL.Query().Get("path")
	var (
		out string
		err error
	)
	switch {
	case strings.HasPrefix(action, "fs/ls"):
		out, err = agent.FsList(r.Context(), ref, path)
	case strings.HasPrefix(action, "fs/cat"):
		out, err = agent.FsCat(r.Context(), ref, path)
	case strings.HasPrefix(action, "fs/tree"):
		out, err = agent.FsTree(r.Context(), ref, path)
	default:
		writeError(w, http.StatusNotFound, "unknown fs action: "+action)
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "%s: %s", action, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(out))
}

// handleCp serves POST /v1/containers/{id}/cp with {src, dst} (host paths or
// container paths like id:/path). Delegates to `cardinal cp`.
func handleCp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Src string `json:"src"`
		Dst string `json:"dst"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Src == "" || req.Dst == "" {
		writeError(w, http.StatusBadRequest, "src and dst required")
		return
	}
	if err := agent.Cp(r.Context(), req.Src, req.Dst); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "cp: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}
