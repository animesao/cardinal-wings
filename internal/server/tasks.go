package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/animesao/cardinal-wings/internal/auth"
	"github.com/animesao/cardinal-wings/internal/tasks"
)

// taskMgr is the process-wide async job manager. Finished tasks are pruned
// after an hour and persisted to a JSON file (surviving restarts).
var taskMgr = tasks.NewManager(1 * time.Hour).WithPersistence(taskDataPath())

func taskDataPath() string {
	if dir := os.Getenv("WINGS_DATA_DIR"); dir != "" {
		return filepath.Join(dir, "wings-tasks.json")
	}
	return "wings-tasks.json"
}

// tasksRoutes mounts /v1/tasks (list), /v1/tasks/{id} (poll) and
// POST /v1/tasks/{id}/cancel + /retry (admin).
func tasksRoutes(mux *http.ServeMux, mw *auth.Middleware) {
	mux.HandleFunc("/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		taskMgr.Prune()
		writeJSON(w, http.StatusOK, map[string]interface{}{"tasks": taskMgr.List()})
	})

	mux.HandleFunc("/v1/tasks/", func(w http.ResponseWriter, r *http.Request) {
		id, action := splitTaskPath(r.URL.Path)
		switch action {
		case "", "cancel", "retry":
		default:
			writeErr(w, http.StatusNotFound, ErrNotFound, "unknown task action: %s", action)
			return
		}
		if action == "" {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			t, ok := taskMgr.Get(id)
			if !ok {
				writeErr(w, http.StatusNotFound, ErrNotFound, "task not found: %s", id)
				return
			}
			writeJSON(w, http.StatusOK, t)
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		mw.AdminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch action {
			case "cancel":
				if !taskMgr.Cancel(id) {
					writeErr(w, http.StatusNotFound, ErrNotFound, "task not found or already finished: %s", id)
					return
				}
				t, _ := taskMgr.Get(id)
				writeJSON(w, http.StatusOK, t)
			case "retry":
				newID, ok := taskMgr.Retry(id)
				if !ok {
					writeErr(w, http.StatusNotFound, ErrNotFound, "task not found, still running, or not retryable: %s", id)
					return
				}
				t, _ := taskMgr.Get(newID)
				writeJSON(w, http.StatusAccepted, t)
			}
		})).ServeHTTP(w, r)
	})
}

// splitTaskPath splits /v1/tasks/{id}[/{action}].
func splitTaskPath(path string) (id, action string) {
	trimmed := strings.TrimPrefix(path, "/v1/tasks/")
	parts := strings.SplitN(trimmed, "/", 2)
	id = parts[0]
	if len(parts) == 2 {
		action = parts[1]
	}
	return id, action
}
