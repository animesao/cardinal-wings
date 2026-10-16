package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/animesao/cardinal-wings/internal/config"
)

// mintPanelJWT builds an HS256 JWT with the given key and claims (test only).
func mintPanelJWT(t *testing.T, key string, claims map[string]interface{}) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"typ": "JWT", "alg": "HS256"})
	payload, _ := json.Marshal(claims)
	h := base64.RawURLEncoding.EncodeToString(header)
	p := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(h + "." + p))
	return h + "." + p + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func testPanelConfig() *config.Config {
	cfg := config.Default()
	cfg.Keys = []config.APIKey{{Name: "panel", Key: "test-node-secret", Role: config.RoleAdmin}}
	return cfg
}

func TestVerifyPanelJWTValid(t *testing.T) {
	cfg := testPanelConfig()
	token := mintPanelJWT(t, "test-node-secret", map[string]interface{}{
		"server_uuid": "srv-1",
		"scope":       "websocket",
		"permissions": []string{"websocket.connect", "control.console"},
		"exp":         time.Now().Add(10 * time.Minute).Unix(),
		"nbf":         time.Now().Unix(),
	})
	claims, role, err := verifyPanelJWT(token, cfg, true)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if role != config.RoleAdmin {
		t.Fatalf("role = %q, want admin", role)
	}
	if claims.ServerUUID != "srv-1" || !claims.hasScope("websocket") {
		t.Fatalf("claims mismatch: %+v", claims)
	}
	if len(claims.Permissions) != 2 {
		t.Fatalf("permissions lost: %+v", claims.Permissions)
	}
}

func TestVerifyPanelJWTWrongKey(t *testing.T) {
	cfg := testPanelConfig()
	token := mintPanelJWT(t, "other-secret", map[string]interface{}{
		"server_uuid": "srv-1",
		"scope":       "websocket",
		"exp":         time.Now().Add(10 * time.Minute).Unix(),
	})
	if _, _, err := verifyPanelJWT(token, cfg, true); err == nil {
		t.Fatal("expected signature mismatch error")
	}
}

func TestVerifyPanelJWTExpired(t *testing.T) {
	cfg := testPanelConfig()
	token := mintPanelJWT(t, "test-node-secret", map[string]interface{}{
		"server_uuid": "srv-1",
		"scope":       "file-download",
		"file_path":   "/server.properties",
		"exp":         time.Now().Add(-time.Minute).Unix(),
	})
	if _, _, err := verifyPanelJWT(token, cfg, true); err == nil {
		t.Fatal("expected expiry error")
	} else if !strings.Contains(err.Error(), "exp") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyPanelJWTMalformed(t *testing.T) {
	cfg := testPanelConfig()
	for _, tok := range []string{"", "a.b", "a.b.c.d", "not-a-jwt"} {
		if _, _, err := verifyPanelJWT(tok, cfg, true); err == nil {
			t.Fatalf("expected error for %q", tok)
		}
	}
}

func TestVerifyPanelJWTReadonlyRole(t *testing.T) {
	cfg := testPanelConfig()
	cfg.Keys = append(cfg.Keys, config.APIKey{Name: "ro", Key: "readonly-secret", Role: config.RoleReadOnly})
	token := mintPanelJWT(t, "readonly-secret", map[string]interface{}{
		"server_uuid": "srv-1",
		"scope":       "websocket file-download",
		"permissions": []string{"websocket.connect", "control.console"},
		"exp":         time.Now().Add(10 * time.Minute).Unix(),
	})
	claims, role, err := verifyPanelJWT(token, cfg, true)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if role != config.RoleReadOnly {
		t.Fatalf("role = %q, want readonly", role)
	}
	if !claims.hasScope("websocket") {
		t.Fatal("scope lost")
	}
}

func TestVerifyPanelJWTRequiresExp(t *testing.T) {
	cfg := testPanelConfig()
	token := mintPanelJWT(t, "test-node-secret", map[string]interface{}{
		"server_uuid": "srv-1",
		"scope":       "websocket",
	})
	if _, _, err := verifyPanelJWT(token, cfg, true); err == nil {
		t.Fatal("expected error for missing exp")
	} else if !strings.Contains(err.Error(), "exp") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyPanelJWTRejectsLongTTL(t *testing.T) {
	cfg := testPanelConfig()
	token := mintPanelJWT(t, "test-node-secret", map[string]interface{}{
		"server_uuid": "srv-1",
		"scope":       "file-download",
		"exp":         time.Now().Add(48 * time.Hour).Unix(),
	})
	if _, _, err := verifyPanelJWT(token, cfg, true); err == nil {
		t.Fatal("expected error for excessive TTL")
	}
}

func TestVerifyPanelJWTRejectsBadAlg(t *testing.T) {
	cfg := testPanelConfig()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT","alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"server_uuid":"srv-1","scope":"websocket","exp":9999999999}`))
	if _, _, err := verifyPanelJWT(header+"."+payload+".", cfg, true); err == nil {
		t.Fatal("expected error for alg=none")
	}
}

func TestPanelLinkFromPush(t *testing.T) {
	link := panelLinkFromPush(map[string]interface{}{
		"panel_url":       "https://panel.example.com",
		"daemon_token_id": "abc123",
		"daemon_token":    "secret",
	})
	if link == nil || link.URL != "https://panel.example.com" || link.TokenID != "abc123" || link.Token != "secret" {
		t.Fatalf("link mismatch: %+v", link)
	}
	if panelLinkFromPush(map[string]interface{}{"server": map[string]interface{}{}}) != nil {
		t.Fatal("expected nil without panel_url")
	}
}

func TestBackupSnapshotPathValidation(t *testing.T) {
	if _, err := backupSnapshotPath("srv-1", "b3f0-uuid"); err != nil {
		t.Fatalf("valid path rejected: %v", err)
	}
	for _, tc := range [][2]string{
		{"../evil", "uuid"}, {"srv", "../../x"}, {"srv", ""}, {"a/b", "uuid"},
	} {
		if _, err := backupSnapshotPath(tc[0], tc[1]); err == nil {
			t.Fatalf("expected error for %q/%q", tc[0], tc[1])
		}
	}
}

func TestIsMutatingBackupDelete(t *testing.T) {
	if !isMutating("backup", "DELETE") {
		t.Fatal("DELETE backup must be admin-only")
	}
	if isMutating("backup", "GET") {
		t.Fatal("GET backup must stay readable")
	}
}

func TestBrowserUploadRejectsReadonlyJWT(t *testing.T) {
	cfg := testPanelConfig()
	cfg.Keys = append(cfg.Keys, config.APIKey{Name: "ro", Key: "readonly-secret", Role: config.RoleReadOnly})
	token := mintPanelJWT(t, "readonly-secret", map[string]interface{}{
		"server_uuid": "srv-1",
		"scope":       "file-upload",
		"exp":         time.Now().Add(10 * time.Minute).Unix(),
	})
	var body strings.Builder
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("files", "evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte("evil"))
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/upload/file?token="+token, strings.NewReader(body.String()))
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	handleBrowserUpload(rec, req, cfg)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("readonly-signed upload JWT: status = %d, want 403", rec.Code)
	}
}

func TestBrowserUploadAllowsAdminJWT(t *testing.T) {
	cfg := testPanelConfig()
	token := mintPanelJWT(t, "test-node-secret", map[string]interface{}{
		"server_uuid": "srv-1",
		"scope":       "file-upload",
		"exp":         time.Now().Add(10 * time.Minute).Unix(),
	})
	var body strings.Builder
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("files", "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte("a"))
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/upload/file?token="+token, strings.NewReader(body.String()))
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	handleBrowserUpload(rec, req, cfg)
	// No container exists in tests: must fail upstream (502), never 403.
	// This pins that admin-signed uploads are not blocked by the role gate.
	if rec.Code == http.StatusForbidden {
		t.Fatal("admin-signed upload JWT must not be rejected by the role gate")
	}
}
