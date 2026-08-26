package server

import (
	"encoding/json"
	"net/http"

	"github.com/animesao/cardinal-wings/internal/runtime"
)

// handleContainerLogs serves container logs. When follow=1 it streams as
// server-sent events by polling cardinal's (non-streaming) logs endpoint for
// new lines; otherwise it returns the requested tail as plain text.
func handleContainerLogs(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tail := r.URL.Query().Get("tail")
	follow := r.URL.Query().Get("follow") == "1"

	// Non-streaming path: return the log tail directly.
	if !follow {
		data, err := defaultClient.Logs(r.Context(), id, tail)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}

	// Streaming path: SSE follow. cardinal's logs endpoint does not stream,
	// so wings polls it and emits new lines as SSE events.
	fl, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	fl.Flush()

	ch, errCh := defaultClient.FollowLogs(r.Context(), id)
	for {
		select {
		case <-r.Context().Done():
			return
		case err := <-errCh:
			if err != nil {
				_, _ = w.Write([]byte("event: error\ndata: " + err.Error() + "\n\n"))
				fl.Flush()
			}
			return
		case line, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write([]byte("data: " + line + "\n\n")); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

// handleContainerExec runs a command in a container via cardinal's exec
// endpoint, returning the exec id.
func handleContainerExec(w http.ResponseWriter, r *http.Request, id string) {
	var req runtime.ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(req.Cmd) == 0 {
		writeError(w, http.StatusBadRequest, "Cmd required")
		return
	}
	execID, err := defaultClient.Exec(r.Context(), id, &req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": execID})
}
