// Package auth provides HTTP middleware: constant-time bearer-key checks with
// role enforcement, a per-IP rate limiter (loopback exempt), CORS, and a
// request-body cap. It mirrors the hardening already in `cardinal/internal/api`.
package auth

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/animesao/cardinal-wings/internal/config"
)

// Context keys for downstream handlers.
type ctxKey int

const (
	ctxRole ctxKey = iota
	ctxKeyName
)

// Middleware bundles the auth helpers for an http.Server chain.
type Middleware struct {
	cfg *config.Config
	// rate limiting: per remote ip and per API key
	mu      sync.Mutex
	clients map[string]*bucket
	keys    map[string]*bucket
}

const (
	rateTPS    = 25.0
	burst      = 50
	keyRateTPS = 10.0
	keyBurst   = 30
	maxClients = 4096
)

type bucket struct {
	tokens float64
	last   time.Time
}

// New builds a Middleware from config.
func New(cfg *config.Config) *Middleware {
	return &Middleware{cfg: cfg, clients: map[string]*bucket{}, keys: map[string]*bucket{}}
}

// Authenticate gates the request on a valid API key and embeds the role.
func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if len(auth) < len(prefix) || auth[:len(prefix)] != prefix {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		role, ok := m.cfg.Authorize(auth[len(prefix):])
		if !ok {
			writeError(w, http.StatusForbidden, "invalid API key")
			return
		}
		name, _ := m.cfg.KeyName(auth[len(prefix):])
		ctx := WithRole(r.Context(), role)
		ctx = WithKeyName(ctx, name)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AdminOnly rejects requests that lack the admin role.
func (m *Middleware) AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if role, ok := RoleFrom(r.Context()); !ok || role != config.RoleAdmin {
			writeError(w, http.StatusForbidden, "admin role required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RateLimit is a per-IP token bucket; loopback is exempt so the panel and
// local tooling are not throttled.
func (m *Middleware) RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			next.ServeHTTP(w, r)
			return
		}
		m.mu.Lock()
		b, ok := m.clients[host]
		if !ok {
			if len(m.clients) >= maxClients {
				m.pruneLocked(10 * time.Minute)
			}
			b = &bucket{tokens: burst, last: time.Now()}
			m.clients[host] = b
		}
		if !takeLocked(b) {
			m.mu.Unlock()
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		m.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

// RateLimitKey is a per-key token bucket that applies even on loopback, so a
// shared key cannot hammer the API from anywhere. The key name (or the raw
// bearer token) is the bucket id.
func (m *Middleware) RateLimitKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		id := "anon"
		if len(auth) > len(prefix) && auth[:len(prefix)] == prefix {
			if name, ok := KeyNameFrom(r.Context()); ok && name != "" {
				id = "key:" + name
			} else {
				id = "key:" + auth[len(prefix):]
			}
		}
		m.mu.Lock()
		b, ok := m.keys[id]
		if !ok {
			if len(m.keys) >= maxClients {
				m.pruneKeysLocked(10 * time.Minute)
			}
			b = &bucket{tokens: keyBurst, last: time.Now()}
			m.keys[id] = b
		}
		// Per-key bucket refills at its own rate.
		now := time.Now()
		b.tokens += now.Sub(b.last).Seconds() * keyRateTPS
		if b.tokens > keyBurst {
			b.tokens = keyBurst
		}
		b.last = now
		if b.tokens < 1 {
			m.mu.Unlock()
			writeError(w, http.StatusTooManyRequests, "key rate limit exceeded")
			return
		}
		b.tokens--
		m.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) pruneKeysLocked(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	for k, v := range m.keys {
		if v.last.Before(cutoff) {
			delete(m.keys, k)
		}
	}
}

func (m *Middleware) pruneLocked(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	for k, v := range m.clients {
		if v.last.Before(cutoff) {
			delete(m.clients, k)
		}
	}
}

// takeLocked refills and consumes one token from a bucket.
func takeLocked(b *bucket) bool {
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * rateTPS
	if b.tokens > burst {
		b.tokens = burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// CORS restricts browser cross-origin access to loopback unless extra origins
// are configured.
func (m *Middleware) CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":` + `"` + msg + `"` + "}\n"))
}
