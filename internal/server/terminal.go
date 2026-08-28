package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/animesao/cardinal-wings/internal/agent"
	"github.com/animesao/cardinal-wings/internal/ws"
)

// terminalSession is one console session attached to a container's main
// process. Output chunks are kept in a small ring buffer (so late SSE
// subscribers see recent output) and broadcast to live subscribers.
type terminalSession struct {
	mu        sync.Mutex
	id        string
	closed    bool
	stdinCh   chan string
	cancel    context.CancelFunc
	ring      []string
	subs      map[chan string]struct{}
	generated uint64
}

// terminalManager tracks live terminal sessions per container id.
type terminalManager struct {
	mu       sync.Mutex
	sessions map[string]*terminalSession
}

var terminals = &terminalManager{sessions: map[string]*terminalSession{}}

// open starts a console session attached to the container's main process.
// If one is already open for the container it is replaced.
func (tm *terminalManager) open(ctx context.Context, containerID string) (*terminalSession, error) {
	sessCtx, cancel := context.WithCancel(ctx)
	stdinCh := make(chan string, 64)

	s := &terminalSession{
		id:      containerID,
		stdinCh: stdinCh,
		cancel:  cancel,
		subs:    map[chan string]struct{}{},
	}

	// cardinal attach connects to the container's console socket — the
	// stdin/stdout of the main process (PID 1). For game servers this is the
	// game console itself: typed lines go to the game (e.g. Minecraft
	// `list`, `say`, `stop`) and game log output streams back live. This
	// mirrors how Pterodactyl's console works; a /bin/sh exec would open a
	// shell NEXT to the game instead of talking TO it.
	go func() {
		err := agent.Attach(sessCtx, containerID, stdinCh, func(data string) {
			s.broadcast(data)
		})
		_ = err
		s.broadcast("\r\n__session_ended__")
	}()

	tm.mu.Lock()
	if old, ok := tm.sessions[containerID]; ok {
		old.close()
	}
	tm.sessions[containerID] = s
	tm.mu.Unlock()

	return s, nil
}

// broadcast appends to the ring and forwards to subscribers.
func (s *terminalSession) broadcast(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.generated++
	if len(s.ring) >= 200 {
		s.ring = append(s.ring[1:], line)
	} else {
		s.ring = append(s.ring, line)
	}
	for ch := range s.subs {
		select {
		case ch <- line:
		default:
		}
	}
}

// subscribe returns a channel that replays the ring then live lines.
func (s *terminalSession) subscribe() (<-chan string, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan string, 512)
	for _, l := range s.ring {
		select {
		case ch <- l:
		default:
		}
	}
	s.subs[ch] = struct{}{}
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
		close(ch)
	}
}

func (s *terminalSession) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	if s.stdinCh != nil {
		close(s.stdinCh)
	}
}

func (tm *terminalManager) get(id string) *terminalSession {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.sessions[id]
}

func (tm *terminalManager) remove(id string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	delete(tm.sessions, id)
}

// writeInput feeds stdin of the session for a container.
func (s *terminalSession) writeInput(data string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return io.ErrClosedPipe
	}
	s.mu.Unlock()
	// Feed into the attach stdin channel
	if s.stdinCh != nil {
		select {
		case s.stdinCh <- data:
			return nil
		default:
			return io.ErrShortWrite
		}
	}
	return io.ErrClosedPipe
}

func handleTerminalOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := terminalID(r)
	if id == "" {
		writeError(w, http.StatusNotFound, "missing container id")
		return
	}
	if _, err := terminals.open(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, ErrInternal, "terminal: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"session": id, "shell": "attach"})
}

func handleTerminalInput(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := terminalID(r)
	s := terminals.get(id)
	if s == nil {
		writeErr(w, http.StatusNotFound, ErrNotFound, "no terminal session for container: %s", id)
		return
	}
	var req struct {
		Data string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.writeInput(req.Data); err != nil {
		writeErr(w, http.StatusInternalServerError, ErrInternal, "terminal input: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func handleTerminalStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := terminalID(r)
	s := terminals.get(id)
	if s == nil {
		writeErr(w, http.StatusNotFound, ErrNotFound, "no terminal session for container: %s", id)
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

	ch, unsubscribe := s.subscribe()
	defer unsubscribe()
	defer terminals.remove(id)
	defer s.close()

	heartbeat := time.NewTicker(5 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case line := <-ch:
			if line == "__session_ended__" {
				_, _ = w.Write([]byte("event: end\ndata: {}\n\n"))
				fl.Flush()
				return
			}
			_, _ = w.Write([]byte("data: " + line + "\n\n"))
			fl.Flush()
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			fl.Flush()
		}
	}
}

// handleTerminalWS bridges a websocket to an interactive session: text
// messages from the browser become stdin, session output is sent back as
// text frames. This is the terminal path a real panel UI would use.
func handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := terminalID(r)
	if id == "" {
		writeError(w, http.StatusNotFound, "missing container id")
		return
	}
	conn, err := ws.Upgrade(w, r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, ErrBadRequest, "websocket upgrade: %s", err.Error())
		return
	}
	defer conn.Close()

	sess, err := terminals.open(r.Context(), id)
	if err != nil {
		_ = conn.WriteText([]byte("\r\nterminal error: " + err.Error() + "\r\n"))
		return
	}
	defer sess.close()
	defer terminals.remove(id)

	// Session output -> websocket.
	done := make(chan struct{})
	go func() {
		defer close(done)
		ch, unsubscribe := sess.subscribe()
		defer unsubscribe()
		for line := range ch {
			if line == "__session_ended__" {
				return
			}
			if err := conn.WriteText([]byte(line + "\r\n")); err != nil {
				return
			}
		}
	}()

	// Websocket input -> session stdin.
	for {
		msg, err := conn.ReadText()
		if err != nil {
			return
		}
		if err := sess.writeInput(string(msg)); err != nil {
			return
		}
	}
}

// terminalID extracts the container id from /v1/containers/{id}/terminal/...
func terminalID(r *http.Request) string {
	ref, _ := splitRef(r.URL.Path)
	return ref
}
