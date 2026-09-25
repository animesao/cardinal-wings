package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/animesao/cardinal-wings/internal/agent"
	"github.com/animesao/cardinal-wings/internal/runtime"
)

// handleContainerLogs serves container logs on the given node client. When
// follow=1 it streams as server-sent events by polling cardinal's
// (non-streaming) logs endpoint for new lines; otherwise it returns the
// requested tail as plain text.
func handleContainerLogs(w http.ResponseWriter, r *http.Request, id string, c *runtime.Client) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tail := r.URL.Query().Get("tail")
	follow := r.URL.Query().Get("follow") == "1"

	// Non-streaming path: return the log tail directly.
	if !follow {
		data, err := c.Logs(r.Context(), id, tail)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, ErrInternal, "logs %s: %s", id, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}

	// Streaming path: SSE follow. cardinal's logs endpoint does not stream, so
	// wings polls it and emits new lines as SSE events.
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

	ch, errCh := c.FollowLogs(r.Context(), id)
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

// handleContainerStatsStream polls container stats every ~2s and emits each
// snapshot as an SSE event, so the panel can draw live CPU/mem charts.
func handleContainerStatsStream(w http.ResponseWriter, r *http.Request, id string, c *runtime.Client) {
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

	interval := 2 * time.Second
	if v := r.URL.Query().Get("interval"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 500*time.Millisecond {
			interval = d
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	send := func() bool {
		var s interface{}
		if err := c.Stats(r.Context(), id, &s); err != nil {
			return false
		}
		b, err := json.Marshal(s)
		if err != nil {
			return false
		}
		if _, err := w.Write([]byte("data: " + string(b) + "\n\n")); err != nil {
			return false
		}
		fl.Flush()
		return true
	}

	send()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !send() {
				return
			}
		}
	}
}

// handleContainerExecStream runs a command and streams its output as SSE so a
// panel can show a live log of the command. Runs via the local cardinal CLI
// (see agent.StreamExec). Interactive stdin (a true TTY) is not supported yet.
func handleContainerExecStream(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Cmd []string `json:"Cmd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(req.Cmd) == 0 {
		writeError(w, http.StatusBadRequest, "Cmd required")
		return
	}

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

	emit := func(line string) {
		_, _ = w.Write([]byte("data: " + line + "\n\n"))
		fl.Flush()
	}
	err := agent.StreamExec(r.Context(), id, req.Cmd, emit)
	if err != nil {
		_, _ = w.Write([]byte("event: error\ndata: " + err.Error() + "\n\n"))
		fl.Flush()
	}
	_, _ = w.Write([]byte("event: done\ndata: {}\n\n"))
	fl.Flush()
}

// handleContainerExec runs a command in a container via cardinal's exec
// endpoint, returning the exec id.
func handleContainerExec(w http.ResponseWriter, r *http.Request, id string, c *runtime.Client) {
	var req runtime.ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(req.Cmd) == 0 {
		writeError(w, http.StatusBadRequest, "Cmd required")
		return
	}
	execID, err := c.Exec(r.Context(), id, &req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, ErrInternal, "exec %s: %s", id, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": execID})
}
