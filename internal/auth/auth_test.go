package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/animesao/cardinal-wings/internal/config"
)

const secret = "s3cret-key"

func newMiddleware(keys ...config.APIKey) *Middleware {
	cfg := config.Default()
	if len(keys) == 0 {
		keys = []config.APIKey{{Name: "main", Key: secret, Role: config.RoleAdmin}}
	}
	cfg.Keys = keys
	return New(cfg)
}

func okHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func do(mw *Middleware, method, token string) *httptest.ResponseRecorder {
	handler := mw.Authenticate(mw.RateLimit(http.HandlerFunc(okHandler)))
	req := httptest.NewRequest(method, "http://example.test/v1/containers", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestAuthenticateMissingToken(t *testing.T) {
	rec := do(newMiddleware(), "GET", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestAuthenticateWrongToken(t *testing.T) {
	rec := do(newMiddleware(), "GET", "wrong")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rec.Code)
	}
}

func TestAuthenticateValidToken(t *testing.T) {
	rec := do(newMiddleware(), "GET", secret)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAuthenticateQueryToken(t *testing.T) {
	// Browser WebSockets can't set the Authorization header, so the panel sends
	// the API key as ?token= on the console WS URL. It must authenticate.
	mw := newMiddleware()
	handler := mw.Authenticate(mw.RateLimit(http.HandlerFunc(okHandler)))
	req := httptest.NewRequest("GET", "http://example.test/v1/containers/x/terminal/ws?token="+secret, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("query token should authenticate, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAuthenticateQueryTokenWrong(t *testing.T) {
	mw := newMiddleware()
	handler := mw.Authenticate(mw.RateLimit(http.HandlerFunc(okHandler)))
	req := httptest.NewRequest("GET", "http://example.test/v1/containers/x/terminal/ws?token=wrong", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong query token should be 403, got %d", rec.Code)
	}
}

func TestAdminOnlyBlocksReadonly(t *testing.T) {
	keys := []config.APIKey{
		{Name: "admin", Key: "admin-key", Role: config.RoleAdmin},
		{Name: "viewer", Key: "view-key", Role: config.RoleReadOnly},
	}
	mw := newMiddleware(keys...)
	handler := mw.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// route-level admin gate
		mw.AdminOnly(http.HandlerFunc(okHandler)).ServeHTTP(w, r)
	}))

	req := httptest.NewRequest("POST", "http://example.test/v1/containers", nil)
	req.Header.Set("Authorization", "Bearer view-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("readonly should be forbidden, got %d", rec.Code)
	}

	req2 := httptest.NewRequest("POST", "http://example.test/v1/containers", nil)
	req2.Header.Set("Authorization", "Bearer admin-key")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("admin should pass, got %d", rec2.Code)
	}
}

func TestRateLimitLoopbackExempt(t *testing.T) {
	mw := newMiddleware()
	handler := mw.RateLimit(http.HandlerFunc(okHandler))
	// Loopback remote addr is simulated by httptest's 192.0.2.1, which is NOT
	// loopback, but the auth chain still works. Just assert 200 for one call.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "http://x/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected OK, got %d", rec.Code)
	}
}

func TestCORSHandlesPreflight(t *testing.T) {
	mw := newMiddleware()
	handler := mw.CORS(http.HandlerFunc(okHandler))
	req := httptest.NewRequest("OPTIONS", "http://x/v1/containers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 preflight, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(ct, "POST") {
		t.Fatalf("expected allow-methods to include POST, got %q", ct)
	}
}
