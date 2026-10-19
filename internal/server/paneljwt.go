package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/animesao/cardinal-wings/internal/config"
)

// paneljwt.go verifies the short-lived JWTs the panel issues for browser
// flows (console websocket, file download/upload). The panel signs them with
// HMAC-SHA256 using the node's daemon token — which is exactly the string
// stored as an API key in wings — so verification needs no new secrets and
// no new dependencies (stdlib only).
//
// Panel token layout (lcobucci/jwt, NodeJWTService):
// header:  {"typ":"JWT","alg":"HS256"}
// claims:  iss, aud, jti (+header jti), iat, nbf, exp,
//          server_uuid, scope ("websocket" | "file-download" | "file-upload" | ...),
//          file_path (download), permissions (websocket), user_uuid, unique_id

// panelClaims is the subset of claims wings enforces.
type panelClaims struct {
	ServerUUID  string   `json:"server_uuid"`
	Scope       string   `json:"scope"`
	FilePath    string   `json:"file_path"`
	Directory   string   `json:"directory"`
	BackupUUID  string   `json:"backup_uuid"`
	Permissions []string `json:"permissions"`
	ExpiresAt   int64    `json:"exp"`
	NotBefore   int64    `json:"nbf"`
}

// maxPanelJWTTTL caps token lifetime: even a leaked signing key only buys
// the attacker a bounded window (the panel mints 5-10 minute tokens).
const maxPanelJWTTTL = 24 * time.Hour

// verifyPanelJWT checks the HS256 signature against every configured API key
// and returns the claims plus the role of the key that signed them.
// Callers must downgrade write permissions for non-admin roles: a stolen
// readonly key must never mint itself control.* powers.
func verifyPanelJWT(token string, cfg *config.Config, strictExpiry bool) (*panelClaims, config.Role, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, "", fmt.Errorf("jwt: malformed token")
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if raw, err := base64.RawURLEncoding.DecodeString(parts[0]); err != nil {
		return nil, "", fmt.Errorf("jwt: bad header encoding")
	} else if err := json.Unmarshal(raw, &header); err != nil {
		return nil, "", fmt.Errorf("jwt: bad header JSON")
	} else if header.Alg != "HS256" {
		return nil, "", fmt.Errorf("jwt: unexpected alg")
	}
	signingInput := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, "", fmt.Errorf("jwt: bad signature encoding")
	}

	matched := false
	var role config.Role
	for _, k := range cfg.Keys {
		mac := hmac.New(sha256.New, []byte(k.Key))
		mac.Write([]byte(signingInput))
		// Сравниваем ВСЕ ключи, не прерываясь на первом совпадении: ранний
		// break позволял по времени ответа определить, каким по счёту
		// ключом подписан токен, и сколько ключей настроено.
		if subtle.ConstantTimeCompare(sig, mac.Sum(nil)) == 1 {
			matched = true
			role = k.Role
		}
	}
	if !matched {
		return nil, "", fmt.Errorf("jwt: signature mismatch (denylist)")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, "", fmt.Errorf("jwt: bad claims encoding")
	}
	var claims panelClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, "", fmt.Errorf("jwt: bad claims JSON")
	}

	now := time.Now().Unix()
	// Eternal tokens (no exp) are rejected: every legitimate panel flow
	// sets exp, and an exp-less token would survive key rotation forever.
	if claims.ExpiresAt <= 0 {
		return nil, "", fmt.Errorf("jwt: exp claim is required")
	}
	if now > claims.ExpiresAt {
		return nil, "", fmt.Errorf("jwt: exp claim is invalid")
	}
	if claims.ExpiresAt-now > int64(maxPanelJWTTTL/time.Second) {
		return nil, "", fmt.Errorf("jwt: TTL exceeds maximum")
	}
	if claims.NotBefore > 0 && now+60 < claims.NotBefore {
		return nil, "", fmt.Errorf("jwt: created too far in past (denylist)")
	}
	if strictExpiry && now > claims.ExpiresAt {
		return nil, "", fmt.Errorf("jwt: token expired")
	}
	return &claims, role, nil
}

// hasScope reports whether the space-joined scope claim contains want.
func (c *panelClaims) hasScope(want string) bool {
	for _, s := range strings.Fields(c.Scope) {
		if s == want {
			return true
		}
	}
	return false
}

// secondsLeft returns seconds until expiry, or -1 when the token has no exp.
func (c *panelClaims) secondsLeft() int64 {
	if c.ExpiresAt == 0 {
		return -1
	}
	return c.ExpiresAt - time.Now().Unix()
}
