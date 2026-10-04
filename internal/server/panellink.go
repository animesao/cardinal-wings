package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/animesao/cardinal-wings/internal/config"
)

// panellink.go ties wings back to the web panel for SFTP authentication.
// The panel pushes its URL + this node's token pair on every node save
// (POST /v1/config, see panel.go); admins can also set it statically via the
// [panel] section in config.toml. The file copy wins when the config section
// is empty, so installer-less panel-managed nodes work out of the box.

// panelLink carries the credentials wings uses to call panel remote APIs.
type panelLink struct {
	URL     string `json:"url"`
	TokenID string `json:"token_id"`
	Token   string `json:"token"`
}

func panelLinkPath() string {
	return filepath.Join(wingsDataDir(), "panel-link.json")
}

// panelLinkFromPush extracts the link from a panel-pushed config payload
// (Node::getWingsConfiguration()). The panel sends token_id implicitly: the
// daemon bearer it already uses is "tokenID.secret"... but wings only sees
// the secret part as its API key name lookup. getWingsConfiguration sends
// panel_url + node_uuid; token_id arrives as "daemon_token_id" when the
// panel includes it. Accept several spellings to be tolerant.
func panelLinkFromPush(cfg map[string]interface{}) *panelLink {
	str := func(m map[string]interface{}, keys ...string) string {
		for _, k := range keys {
			if v, ok := m[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	url := str(cfg, "panel_url", "panelURL", "remote")
	if url == "" {
		return nil
	}
	link := &panelLink{
		URL:     url,
		TokenID: str(cfg, "daemon_token_id", "token_id", "tokenID"),
		Token:   str(cfg, "daemon_token", "token", "secret"),
	}
	if link.TokenID == "" && link.Token == "" {
		return nil
	}
	return link
}

func savePanelLink(link *panelLink) error {
	data, err := json.MarshalIndent(link, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(wingsDataDir(), 0700); err != nil {
		return err
	}
	tmp := panelLinkPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, panelLinkPath())
}

func loadPanelLinkFile() *panelLink {
	data, err := os.ReadFile(panelLinkPath())
	if err != nil {
		return nil
	}
	var link panelLink
	if err := json.Unmarshal(data, &link); err != nil {
		return nil
	}
	if link.URL == "" {
		return nil
	}
	return &link
}

// effectivePanelLink returns the panel link: static config wins, then the
// panel-pushed file. The second return is false when no link is configured.
func effectivePanelLink(cfg *config.Config) (*panelLink, bool) {
	if cfg != nil && cfg.Panel.URL != "" && cfg.Panel.Token != "" {
		return &panelLink{URL: cfg.Panel.URL, TokenID: cfg.Panel.TokenID, Token: cfg.Panel.Token}, true
	}
	if link := loadPanelLinkFile(); link != nil && (link.Token != "" || link.TokenID != "") {
		return link, true
	}
	return nil, false
}

// panelSFTPAuth verifies SFTP credentials against the panel's
// /api/remote/sftp/auth endpoint. On success it returns the server (==
// container) uuid the session must be jailed to.
func panelSFTPAuth(link *panelLink, username, password, authType string) (string, error) {
	if link == nil || link.URL == "" {
		return "", fmt.Errorf("no panel link configured")
	}
	body, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
		"type":     authType,
	})
	url := link.URL + "/api/remote/sftp/auth"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if link.TokenID != "" {
		req.Header.Set("Authorization", "Bearer "+link.TokenID+"."+link.Token)
	} else {
		req.Header.Set("Authorization", "Bearer "+link.Token)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("panel sftp auth: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("panel sftp auth: status %d", resp.StatusCode)
	}
	var out struct {
		Server string `json:"server"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("panel sftp auth decode: %w", err)
	}
	if out.Server == "" {
		return "", fmt.Errorf("panel sftp auth: empty server")
	}
	return out.Server, nil
}
