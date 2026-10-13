package server

import (
	"encoding/base64"
	"net/http/httptest"
	"net/url"
	"testing"
)

// TestPathArgRawPaths verifies that plain query paths reach the handler
// untouched — even when they happen to be valid base64. "/plugins" is valid
// base64 that round-trips but decodes to binary garbage; it previously
// produced "path not found: <mojibake>" on Minecraft servers.
func TestPathArgRawPaths(t *testing.T) {
	for _, raw := range []string{"/plugins", "/mods", "/config", "/world", "/logs", "/server.properties", "/"} {
		r := httptest.NewRequest("GET", "/v1/containers/x/fm/list?path="+raw, nil)
		got, ok := pathArg(r, nil, "path")
		if !ok || got != raw {
			t.Fatalf("pathArg(%q) = %q, %v; want raw passthrough", raw, got, ok)
		}
	}
}

// TestPathArgBase64 verifies genuine base64 payloads still decode.
func TestPathArgBase64(t *testing.T) {
	want := "/hello world/Привет"
	enc := base64.StdEncoding.EncodeToString([]byte(want))
	r := httptest.NewRequest("GET", "/v1/containers/x/fm/list?path="+url.QueryEscape(enc), nil)
	got, ok := pathArg(r, nil, "path")
	if !ok || got != want {
		t.Fatalf("pathArg(%q) = %q, %v; want %q", enc, got, ok, want)
	}
}

// TestPathArgBodyPlain verifies JSON body paths are never base64-decoded.
func TestPathArgBodyPlain(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/containers/x/fm/write", nil)
	got, ok := pathArg(r, map[string]interface{}{"path": "test"}, "path")
	if !ok || got != "test" {
		t.Fatalf("body pathArg = %q, %v; want test", got, ok)
	}
}
