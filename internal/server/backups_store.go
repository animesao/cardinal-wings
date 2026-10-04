package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/animesao/cardinal-wings/internal/agent"
)

// backups_store.go implements daemon-local backup snapshots for the panel's
// default ("agent") backup driver, mirroring classic wings semantics:
//
//   - POST /v1/containers/{id}/backup {uuid} → 202, snapshot the data dir to
//     disk asynchronously, then report completion to the panel
//     (POST /api/remote/backups/{uuid}) when a panel link is configured.
//   - GET  /download/backup?token=<JWT scope=backup-download> → serve the
//     stored snapshot (browser/panel download).
//   - POST /v1/containers/{id}/backup {backup_uuid} → restore a stored
//     snapshot (?clean=1 wipes first). {download_url} restores a remote
//     archive (S3 presigned URL) without panel-side streaming.
//   - DELETE /v1/containers/{id}/backup?uuid= → drop the stored snapshot.
//
// Snapshots live under $WINGS_DATA_DIR/backups/<container>/<uuid>.tar.gz.

// backupStoreDir is the root of daemon-local snapshots.
func backupStoreDir() string {
	return filepath.Join(wingsDataDir(), "backups")
}

func backupSnapshotPath(containerID, uuid string) (string, error) {
	if !validContainerID(containerID) || uuid == "" || strings.ContainsAny(uuid, `/\`) || strings.Contains(uuid, "..") {
		return "", fmt.Errorf("invalid container or backup id")
	}
	return filepath.Join(backupStoreDir(), containerID, uuid+".tar.gz"), nil
}

// backupRequest is the JSON POST body for snapshot/restore operations.
type backupRequest struct {
	UUID        string `json:"uuid"`
	BackupUUID  string `json:"backup_uuid"`
	DownloadURL string `json:"download_url"`
	Format      string `json:"format"`
}

// handleBackupRoute dispatches backup calls: JSON snapshot/restore
// operations are handled here, everything else (GET live stream, binary
// upload restore) falls through to the existing handler.
func handleBackupRoute(w http.ResponseWriter, r *http.Request, ref string) {
	if r.Method == http.MethodDelete {
		handleBackupStoreDelete(w, r, ref)
		return
	}
	if r.Method == http.MethodPost && strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "read body: "+err.Error())
			return
		}
		var req backupRequest
		if jerr := json.Unmarshal(body, &req); jerr == nil && (req.UUID != "" || req.BackupUUID != "" || req.DownloadURL != "") {
			root := ""
			if p, _, perr := containerDataRoot(ref); perr == nil {
				root = p
			} else {
				root = fmRoot(r.Context(), ref)
			}
			if root == "/" || root == "" {
				writeError(w, http.StatusBadRequest, "container has no dedicated data mount; refusing backup operation")
				return
			}
			switch {
			case req.DownloadURL != "" || req.BackupUUID != "":
				handleBackupStoredRestore(w, r, ref, root, req)
			default:
				handleBackupStoreCreate(w, r, ref, req)
			}
			return
		}
		// Not a store-operation payload: replay the body into the classic
		// binary-restore handler.
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
	}
	handleContainerBackup(w, r, ref)
}

// handleBackupStoreCreate snapshots the data dir asynchronously.
func handleBackupStoreCreate(w http.ResponseWriter, r *http.Request, ref string, req backupRequest) {
	uuid := req.UUID
	if uuid == "" {
		uuid = req.BackupUUID
	}
	path, err := backupSnapshotPath(ref, uuid)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	go runBackupSnapshot(ref, uuid, path)
	writeJSON(w, http.StatusAccepted, map[string]interface{}{"ok": true, "accepted": true, "uuid": uuid})
}

// runBackupSnapshot tars the data root to path and reports to the panel.
func runBackupSnapshot(containerID, uuid, path string) {
	fail := func(cause string) {
		logf("backup %s/%s failed: %s", containerID, uuid, cause)
		reportBackupStatus(uuid, false, "", 0)
	}

	root := ""
	if p, _, err := containerDataRoot(containerID); err == nil {
		root = p
	} else {
		root = fmRoot(context.Background(), containerID)
	}
	if root == "/" || root == "" {
		fail("container has no dedicated data mount")
		return
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		fail(err.Error())
		return
	}
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		fail(err.Error())
		return
	}
	hasher := sha256.New()
	size, terr := tarDataRootTo(f, hasher, containerID, root)
	_ = f.Close()
	if terr != nil {
		_ = os.Remove(tmp)
		fail(terr.Error())
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		fail(err.Error())
		return
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	logf("backup %s/%s stored (%d bytes, sha256:%.12s)", containerID, uuid, size, sum)
	reportBackupStatus(uuid, true, sum, size)
}

// tarDataRootTo archives root (host tar preferred, container exec fallback)
// into w while hashing, returning the byte count.
func tarDataRootTo(w io.Writer, hasher io.Writer, containerID, root string) (int64, error) {
	mw := io.MultiWriter(w, hasher)
	counter := &countWriter{w: mw}
	if hostRoot, herr := hostDataRoot(containerID); herr == nil && hostRoot != "/" {
		cmd := exec.Command("tar", "czf", "-", "-C", hostRoot, ".")
		cmd.Stderr = nil
		out, err := cmd.StdoutPipe()
		if err != nil {
			return 0, err
		}
		if err := cmd.Start(); err != nil {
			return 0, err
		}
		n, copyErr := io.Copy(counter, out)
		waitErr := cmd.Wait()
		if copyErr != nil {
			return n, copyErr
		}
		return n, waitErr
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()
	out, wait, err := agent.RawExec(ctx, containerID, []string{"/bin/sh", "-c",
		fmt.Sprintf("tar czf - -C %s . 2>/dev/null", shq(root))}, nil)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	n, copyErr := io.Copy(counter, out)
	if werr := wait(); werr != nil && copyErr == nil {
		return n, werr
	}
	return n, copyErr
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// reportBackupStatus POSTs completion to the panel when linked. Without a
// panel link the snapshot stays on disk and the panel row is left pending
// (panel/link setup is part of node install).
func reportBackupStatus(uuid string, successful bool, checksum string, size int64) {
	link, ok := effectivePanelLink(wingCfg)
	if !ok {
		return
	}
	body, _ := json.Marshal(map[string]interface{}{
		"successful":    successful,
		"checksum":      checksum,
		"checksum_type": "sha256",
		"size":          size,
	})
	url := strings.TrimSuffix(link.URL, "/") + "/api/remote/backups/" + uuid
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		logf("backup report %s: %v", uuid, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if link.TokenID != "" {
		req.Header.Set("Authorization", "Bearer "+link.TokenID+"."+link.Token)
	} else {
		req.Header.Set("Authorization", "Bearer "+link.Token)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logf("backup report %s: %v", uuid, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		logf("backup report %s: panel status %d", uuid, resp.StatusCode)
	}
}

// handleBackupStoreDelete drops a stored snapshot. Missing files are 404 so
// the panel can treat "already gone" as success.
func handleBackupStoreDelete(w http.ResponseWriter, r *http.Request, ref string) {
	uuid := r.URL.Query().Get("uuid")
	if uuid == "" {
		uuid = r.URL.Query().Get("backup_uuid")
	}
	if uuid == "" {
		writeError(w, http.StatusBadRequest, "uuid required")
		return
	}
	path, err := backupSnapshotPath(ref, uuid)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, ErrNotFound, "backup %s not found", uuid)
			return
		}
		writeErr(w, http.StatusInternalServerError, ErrInternal, "delete backup: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": uuid})
}

// handleBackupStoredRestore restores from a stored snapshot or remote URL.
func handleBackupStoredRestore(w http.ResponseWriter, r *http.Request, ref, root string, req backupRequest) {
	clean := r.URL.Query().Get("clean") == "1"
	hostRoot, herr := hostDataRoot(ref)
	if herr != nil || hostRoot == "/" {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "restore: overlay not mounted (start the container first)")
		return
	}
	if req.DownloadURL != "" {
		if err := restoreFromURL(r, hostRoot, req.DownloadURL, clean); err != nil {
			writeErr(w, http.StatusBadGateway, ErrUpstream, "restore download: %s", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"restored": ref, "clean": fmt.Sprintf("%t", clean)})
		return
	}
	path, err := backupSnapshotPath(ref, req.BackupUUID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeErr(w, http.StatusNotFound, ErrNotFound, "backup %s not found", req.BackupUUID)
		return
	}
	defer f.Close()
	if err := extractReaderToHost(hostRoot, f, clean); err != nil {
		writeErr(w, http.StatusInternalServerError, ErrInternal, "restore: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"restored": ref, "clean": fmt.Sprintf("%t", clean)})
}

// restoreFromURL fetches a remote archive (e.g. S3 presigned URL) and
// extracts it into the data root without buffering the whole file.
func restoreFromURL(r *http.Request, hostRoot, url string, clean bool) error {
	dl, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(dl)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("download status %d", resp.StatusCode)
	}
	return extractReaderToHost(hostRoot, io.LimitReader(resp.Body, 20<<30), clean)
}

// handleBackupJWTDownload serves a stored snapshot on bearer JWT
// (scope backup-download, claims backup_uuid + server_uuid). Registered on
// the public mux by panelBrowserRoutes.
func handleBackupJWTDownload(w http.ResponseWriter, r *http.Request, claims *panelClaims) {
	path, err := backupSnapshotPath(claims.ServerUUID, claims.BackupUUID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		writeErr(w, http.StatusNotFound, ErrNotFound, "backup not found")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeErr(w, http.StatusNotFound, ErrNotFound, "backup not found")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", claims.BackupUUID+".tar.gz"))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}
