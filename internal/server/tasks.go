package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// tasksRoutes mounts /v1/tasks (list) and /v1/tasks/{id} (poll).
func tasksRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		taskMgr.Prune()
		writeJSON(w, http.StatusOK, map[string]interface{}{"tasks": taskMgr.List()})
	})

	mux.HandleFunc("/v1/tasks/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/v1/tasks/")
		t, ok := taskMgr.Get(id)
		if !ok {
			writeErr(w, http.StatusNotFound, ErrNotFound, "task not found: %s", id)
			return
		}
		writeJSON(w, http.StatusOK, t)
	})
}
