package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/animesao/cardinal-wings/internal/auth"
)

// auditLog appends one JSONL line per mutating request (POST/DELETE). It is
// how an operator answers "who changed what, when".
type auditLog struct {
	mu   sync.Mutex
	path string
	f    *os.File
}

var audit = &auditLog{}

// initAudit opens (or creates) the audit file. Disabled when path is empty.
func initAudit(path string) {
	audit.mu.Lock()
	defer audit.mu.Unlock()
	if path == "" {
		return
	}
	if dir := filepath.Dir(path); dir != "." {
		_ = os.MkdirAll(dir, 0700)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	audit.path = path
	audit.f = f
}

// record writes one audit entry.
func (a *auditLog) record(keyName, method, path, remote, role string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.f == nil {
		return
	}
	entry := map[string]interface{}{
		"ts":     time.Now().UTC().Format(time.RFC3339),
		"key":    keyName,
		"role":   role,
		"method": method,
		"path":   path,
		"remote": remote,
	}
	data, _ := json.Marshal(entry)
	_, _ = a.f.Write(append(data, '\n'))
}

// auditMiddleware wraps the authenticated mux and records mutating requests.
func auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodDelete || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			name, _ := auth.KeyNameFrom(r.Context())
			role, _ := auth.RoleFrom(r.Context())
			audit.record(name, r.Method, r.URL.Path, r.RemoteAddr, string(role))
		}
		next.ServeHTTP(w, r)
	})
}
