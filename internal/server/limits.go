package server

import (
	"net/http"
	"strings"
)

// limits.go caps request body sizes against memory/CPU exhaustion. JSON API
// bodies are kilobytes; anything bigger is either a bug or an attack —
// except whole-file base64 writes (fm/write and its fm/upload alias, same
// cap as the panel: 256 MiB files → ~342 MiB base64) and the documented
// streaming uploads (backup restore tarballs, fm zip upload, browser file
// upload), which enforce their own limits.

// maxJSONBodyBytes caps ordinary API request bodies.
const maxJSONBodyBytes = 8 << 20 // 8 MiB

// maxFmWriteBytes caps fm/write bodies: whole-file base64 uploads from the
// panel (384 MiB covers the base64 overhead of 256 MiB files).
const maxFmWriteBytes = 384 << 20 // 384 MiB

// maxUploadZipBytes caps fm/upload-zip bodies.
//
// Раньше маршрут был ИСКЛЮЧЁН из общего лимита тела запроса («свой лимит»
// внутри unzip), но фактического лимита не было: decodeBody читал тело
// целиком в память, а потом base64 декодировал его ещё раз. Запрос на 20 ГБ
// давал ~35 ГБ RSS и убивал wings вместе со всеми контейнерами.
const maxUploadZipBytes = 384 << 20 // 384 MiB

// bodyLimitMiddleware rejects oversized bodies before handlers buffer them.
// Streaming upload routes carry their own caps and are exempt.
func bodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.ContentLength == 0 {
			next.ServeHTTP(w, r)
			return
		}
		if largeBodyRoute(r) {
			next.ServeHTTP(w, r)
			return
		}
		cap := bodyCapFor(r)
		r.Body = http.MaxBytesReader(w, r.Body, cap)
		next.ServeHTTP(w, r)
	})
}

// bodyCapFor — лимит тела запроса для конкретного маршрута.
func bodyCapFor(r *http.Request) int64 {
	switch {
	case isFmUploadZipRoute(r):
		return maxUploadZipBytes
	case isFmWriteRoute(r):
		return maxFmWriteBytes
	default:
		return maxJSONBodyBytes
	}
}

// largeBodyRoute reports whether the request is a known streaming upload
// with its own size enforcement (backup restore).
func largeBodyRoute(r *http.Request) bool {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		return false
	}
	p := r.URL.Path
	return strings.HasSuffix(p, "/backup")
}

// isFmUploadZipRoute matches the zip upload route: it carries a base64
// archive in a JSON body, so it needs its own (generous) cap — но не
// бесконечный.
func isFmUploadZipRoute(r *http.Request) bool {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		return false
	}
	return strings.HasSuffix(r.URL.Path, "/fm/upload-zip")
}

// isFmWriteRoute matches whole-file base64 uploads (fm/write and its
// fm/upload alias): anything under /v1/containers/{id}/fm/write|upload.
func isFmWriteRoute(r *http.Request) bool {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		return false
	}
	p := r.URL.Path
	return strings.HasSuffix(p, "/fm/write") || strings.HasSuffix(p, "/fm/upload")
}
