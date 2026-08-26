package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/animesao/cardinal-wings/internal/tasks"
)

// taskMgr is the process-wide async job manager. Finished tasks are pruned
// after an hour so the panel can poll without unbounded growth.
var taskMgr = tasks.NewManager(1 * time.Hour)

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
