package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/animesao/cardinal-wings/internal/auth"
	"github.com/animesao/cardinal-wings/internal/runtime"
)

// panelRoutes mounts panel-facing parity endpoints that have no direct
// cardinal primitive: reinstall, file pull, transfer and config push.
// All mutating routes are admin-only, matching the rest of the v1 surface.
func panelRoutes(mux *http.ServeMux, mw *auth.Middleware) {
	mux.HandleFunc("/v1/transfers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		mw.AdminOnly(http.HandlerFunc(handleTransferInbound)).ServeHTTP(w, r)
	})
	mux.HandleFunc("/v1/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		mw.AdminOnly(http.HandlerFunc(handleConfigPush)).ServeHTTP(w, r)
	})
	mux.HandleFunc("/v1/deauthorize", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		mw.AdminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		})).ServeHTTP(w, r)
	})
}

// handleContainerReinstall recreates a container from a panel CreateRequest,
// keeping the named data volume (srv-<name>) so files survive reinstall.
// POST /v1/containers/{id}/reinstall
func handleContainerReinstall(w http.ResponseWriter, r *http.Request, ref string, c *runtime.Client) {
	var req runtime.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Image == "" {
		writeError(w, http.StatusBadRequest, "image required")
		return
	}
	if req.Name == "" {
		req.Name = ref
	}
	for i := range req.Volumes {
		if req.Volumes[i].Source == "" {
			req.Volumes[i].Source = "srv-" + req.Name
		}
	}

	if err := c.Remove(r.Context(), ref, true); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "not found") &&
			!strings.Contains(strings.ToLower(err.Error()), "no such") {
			writeErr(w, http.StatusBadGateway, ErrUpstream, "remove %s: %s", ref, err.Error())
			return
		}
	}
	res, err := c.Create(r.Context(), &req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "reinstall %s: %s", ref, err.Error())
		return
	}
	if sftpStoreInst != nil {
		_ = sftpStoreInst.remove(ref)
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": res.ID, "reinstalled": ref})
}

// handleContainerPull downloads a remote URL into the container data dir.
// POST /v1/containers/{id}/pull {url, root?, file_name?}
// GET /v1/containers/{id}/pull -> [] (no background queue in v1; panel polls).
func handleContainerPull(w http.ResponseWriter, r *http.Request, ref string) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	case http.MethodPost:
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		URL        string `json:"url"`
		Root       string `json:"root"`
		FileName   string `json:"file_name"`
		UseHeader  string `json:"use_header"`
		Foreground bool   `json:"foreground"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url required")
		return
	}
	root := req.Root
	if root == "" {
		root = "/"
	}

	session, err := beginFMSession(ref)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "pull %s: %s", ref, err.Error())
		return
	}
	defer session.Close()

	name := req.FileName
	if name == "" {
		name = filepath.Base(strings.Split(req.URL, "?")[0])
		if name == "" || name == "." || name == "/" {
			name = "download"
		}
	}
	dest, err := session.resolve(filepath.Join(root, name), true)
	if err != nil {
		writeErr(w, http.StatusBadRequest, ErrBadRequest, "invalid path: %s", root)
		return
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "create parent: %s", err.Error())
		return
	}

	dl, err := http.NewRequestWithContext(r.Context(), http.MethodGet, req.URL, nil)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "pull request: %s", err.Error())
		return
	}
	resp, err := http.DefaultClient.Do(dl)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "pull download: %s", err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "pull download: status %d", resp.StatusCode)
		return
	}
	f, err := os.Create(dest)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "pull write: %s", err.Error())
		return
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, 20<<30))
	_ = f.Close()
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "pull write: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "path": filepath.Join(root, name), "size": n})
}

// handleContainerTransfer accepts a panel transfer notification and reports
// it as queued. The actual data movement is a backup export/import driven
// by the panel (GET /backup on source, POST /backup on target).
func handleContainerTransfer(w http.ResponseWriter, r *http.Request, ref string) {
	var req map[string]interface{}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"ok":        true,
		"queued":    true,
		"id":        ref,
		"server_id": req["server_id"],
	})
}

// handleTransferInbound accepts an inbound transfer registration from a
// source node (panel-driven migration target).
func handleTransferInbound(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
	writeJSON(w, http.StatusAccepted, map[string]interface{}{"ok": true, "accepted": true})
}

// handleConfigPush accepts a panel-pushed wings configuration (TOML-as-JSON
// from Node::getWingsConfiguration()). Keys/binds that require a restart are
// reported; everything else applies on the next container operation. The
// panel link (panel_url + node token pair) is persisted to disk so SFTP
// logins can be verified against the panel even after a restart. The
// endpoint never fails the panel sync flow.
func handleConfigPush(w http.ResponseWriter, r *http.Request) {
	var cfg map[string]interface{}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	logf("panel config push: %s", summarizeKeys(cfg))
	persisted := false
	if link := panelLinkFromPush(cfg); link != nil {
		if err := savePanelLink(link); err != nil {
			logf("panel link persist: %v", err)
		} else {
			persisted = true
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true, "applied": false, "panel_link": persisted,
		"note": fmt.Sprintf("received %d top-level keys; restart wings to apply bind/key changes", len(cfg)),
	})
}

func summarizeKeys(m map[string]interface{}) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return strings.Join(keys, ",")
}
