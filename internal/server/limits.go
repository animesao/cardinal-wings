package server

import (
	"net/http"
	"strings"
)

// limits.go caps request body sizes against memory/CPU exhaustion. JSON API
// bodies are kilobytes; anything bigger is either a bug or an attack —
// except the three documented streaming uploads (backup restore tarballs,
// fm zip upload, browser file upload), which enforce their own limits.

// maxJSONBodyBytes caps ordinary API request bodies.
const maxJSONBodyBytes = 8 << 20 // 8 MiB

// bodyLimitMiddleware rejects oversized bodies before handlers buffer them.
// Streaming upload routes carry their own caps and are exempt.
func bodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.ContentLength != 0 && !largeBodyRoute(r) {
			r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// largeBodyRoute reports whether the request is a known streaming upload
// with its own size enforcement (backup restore, zip upload).
func largeBodyRoute(r *http.Request) bool {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		return false
	}
	p := r.URL.Path
	return strings.HasSuffix(p, "/backup") || strings.HasSuffix(p, "/fm/upload-zip")
}
