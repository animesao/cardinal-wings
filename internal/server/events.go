package server

import (
	"net/http"

	"github.com/animesao/cardinal-wings/internal/agent"
)

// handleEvents streams cardinal container events as SSE. The panel subscribes
// once and gets pushed updates (container started/stopped/removed) instead of
// polling. Each connection spawns a `cardinal events` subprocess on the local
// node; that is fine for a handful of panel clients.
func handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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

	// Stream events; exit when the client disconnects.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = agent.StreamEvents(r.Context(), emit)
	}()
	select {
	case <-r.Context().Done():
	case <-done:
	}
}
