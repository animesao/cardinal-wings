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
	mu      sync.Mutex
	path    string
	f       *os.File
	size    int64
	maxSize int64
}

// auditMaxSize is the rotation threshold: at 10 MiB the log is rotated to
// <path>.1 and a fresh file is started.
const auditMaxSize = 10 << 20

var audit = &auditLog{maxSize: auditMaxSize}

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
	if fi, err := f.Stat(); err == nil {
		audit.size = fi.Size()
	}
}

// maybeRotateLocked rotates the log when it exceeds maxSize.
func (a *auditLog) maybeRotateLocked() {
	if a.f == nil || a.maxSize <= 0 || a.size < a.maxSize {
		return
	}
	_ = a.f.Close()
	_ = os.Rename(a.path, a.path+".1")
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	a.f = f
	a.size = 0
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
	n, _ := a.f.Write(append(data, '\n'))
	a.size += int64(n)
	a.maybeRotateLocked()
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
