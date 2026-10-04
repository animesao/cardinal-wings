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
	BackupUUID  string   `json:"backup_uuid"`
	Permissions []string `json:"permissions"`
	ExpiresAt   int64    `json:"exp"`
	NotBefore   int64    `json:"nbf"`
}

// verifyPanelJWT checks the signature against every configured API key and
// returns the claims. Expiry is enforced by the caller where "expiring soon"
// warnings are needed; here we only reject already-invalid tokens when
// strict is true.
func verifyPanelJWT(token string, cfg *config.Config, strictExpiry bool) (*panelClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("jwt: malformed token")
	}
	signingInput := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("jwt: bad signature encoding")
	}

	matched := false
	for _, k := range cfg.Keys {
		mac := hmac.New(sha256.New, []byte(k.Key))
		mac.Write([]byte(signingInput))
		if subtle.ConstantTimeCompare(sig, mac.Sum(nil)) == 1 {
			matched = true
			break
		}
	}
	if !matched {
		return nil, fmt.Errorf("jwt: signature mismatch (denylist)")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("jwt: bad claims encoding")
	}
	var claims panelClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("jwt: bad claims JSON")
	}

	now := time.Now().Unix()
	if claims.ExpiresAt > 0 && now > claims.ExpiresAt {
		return nil, fmt.Errorf("jwt: exp claim is invalid")
	}
	if claims.NotBefore > 0 && now+60 < claims.NotBefore {
		return nil, fmt.Errorf("jwt: created too far in past (denylist)")
	}
	if strictExpiry && claims.ExpiresAt > 0 && now > claims.ExpiresAt {
		return nil, fmt.Errorf("jwt: token expired")
	}
	return &claims, nil
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
