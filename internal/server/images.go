package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/animesao/cardinal-wings/internal/agent"
	"github.com/animesao/cardinal-wings/internal/auth"
)

// imageRoutes mounts the Phase 2 image endpoints against the runtime client.
func imageRoutes(mux *http.ServeMux, mw *auth.Middleware) {
	mux.HandleFunc("/v1/images", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			c, ok := clientFor(w, r)
			if !ok {
				return
			}
			list, err := c.Images(r.Context())
			if err != nil {
				writeErr(w, http.StatusBadGateway, ErrUpstream, "list images: %s", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"images": list})
		case http.MethodDelete:
			// Delete-all is disallowed by design.
			writeError(w, http.StatusBadRequest, "ref required")
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	mux.HandleFunc("/v1/images/", func(w http.ResponseWriter, r *http.Request) {
		action, _ := splitImagePath(r.URL.Path)
		if isImageMutating(action, r.Method) {
			mw.AdminOnly(http.HandlerFunc(handleImageRef)).ServeHTTP(w, r)
			return
		}
		handleImageRef(w, r)
	})
}

// splitImagePath returns the trailing action and the image ref.
func splitImagePath(path string) (action, ref string) {
	trimmed := strings.TrimPrefix(path, "/v1/images/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 0 {
		return "", ""
	}
	ref = parts[0]
	if len(parts) == 2 {
		action = parts[1]
	}
	return action, ref
}

func isImageMutating(action, method string) bool {
	switch action {
	case "pull", "tag", "push":
		return true
	}
	return action == "" && method == http.MethodDelete
}

func handleImageRef(w http.ResponseWriter, r *http.Request) {
	action, ref := splitImagePath(r.URL.Path)
	if ref == "" {
		writeError(w, http.StatusNotFound, "missing image ref")
		return
	}

	c, ok := clientFor(w, r)
	if !ok {
		return
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		var out interface{}
		if err := c.InspectImage(r.Context(), ref, &out); err != nil {
			writeErr(w, http.StatusNotFound, ErrNotFound, "inspect image %s: %s", ref, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, out)

	case action == "" && r.Method == http.MethodDelete:
		if err := c.RemoveImage(r.Context(), ref); err != nil {
			writeErr(w, http.StatusInternalServerError, ErrInternal, "remove image %s: %s", ref, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"removed": ref})

	case action == "pull" && r.Method == http.MethodPost:
		var req struct {
			Image    string `json:"image"`
			Platform string `json:"platform,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if req.Image == "" {
			req.Image = ref
		}
		id := taskMgr.Submit("image_pull", func(ctx context.Context) (string, error) {
			return agent.PullImageOut(ctx, req.Image, req.Platform)
		})
		writeJSON(w, http.StatusAccepted, map[string]string{"task_id": id, "action": "pull", "image": req.Image})

	case action == "tag" && r.Method == http.MethodPost:
		repo := r.URL.Query().Get("repo")
		if repo == "" {
			writeError(w, http.StatusBadRequest, "repo query param required")
			return
		}
		if err := c.TagImage(r.Context(), ref, repo, r.URL.Query().Get("tag")); err != nil {
			writeErr(w, http.StatusInternalServerError, ErrInternal, "tag %s: %s", ref, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"tagged": repo, "from": ref})

	case action == "push" && r.Method == http.MethodPost:
		user := r.URL.Query().Get("username")
		pass := r.URL.Query().Get("password")
		if err := c.PushImage(r.Context(), ref, user, pass); err != nil {
			writeErr(w, http.StatusInternalServerError, ErrInternal, "push %s: %s", ref, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"pushed": ref})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
