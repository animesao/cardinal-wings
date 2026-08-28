package server

// backups.go streams container data as a tar.gz archive. Unlike the file
// manager (per-file base64 over JSON) backups move whole directory trees as
// one raw binary stream: `tar czf - -C <data-root> .` runs inside the
// container through `cardinal exec` and wings pipes it straight to the HTTP
// response. Restore is the reverse: the uploaded archive is piped into
// `tar xzf - -C <data-root>`, optionally after wiping the data root
// (?clean=1) so the restore is exact, not a merge.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/animesao/cardinal-wings/internal/agent"
)

// backupDeadline covers slow transfers: a multi-GB world over a slow link
// needs far more than the server's default 60s write timeout. The handler
// clears the per-connection deadline instead of tuning the whole server.
const backupDeadline = 4 * time.Hour

// handleContainerBackup serves GET (create archive) and POST (restore) on
// /v1/containers/{id}/backup.
func handleContainerBackup(w http.ResponseWriter, r *http.Request, ref string) {
	// Long transfers: clear the read/write deadlines for this connection only.
	if rc := http.NewResponseController(w); rc != nil {
		_ = rc.SetWriteDeadline(time.Now().Add(backupDeadline))
		_ = rc.SetReadDeadline(time.Now().Add(backupDeadline))
	}

	root := fmRoot(r.Context(), ref)
	if root == "/" {
		writeError(w, http.StatusBadRequest, "container has no dedicated data mount; refusing to back up the whole filesystem")
		return
	}

	switch r.Method {
	case http.MethodGet:
		streamBackup(w, r, ref, root)
	case http.MethodPost:
		restoreBackup(w, r, ref, root)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// streamBackup pipes `tar czf - -C <root> .` from the container to the
// response body with zero buffering — the panel (or curl) writes the archive
// to disk while tar is still running inside the container.
func streamBackup(w http.ResponseWriter, r *http.Request, ref, root string) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	out, wait, err := agent.RawExec(ctx, ref, []string{"/bin/sh", "-c",
		fmt.Sprintf("tar czf - -C %s . 2>/dev/null", shq(root))}, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backup exec: "+err.Error())
		return
	}
	defer out.Close()

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"container-%s-backup.tar.gz\"", ref))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	if _, err := copyAndClose(w, out); err != nil {
		logf("backup %s: client copy aborted: %v", ref, err)
		return
	}
	if err := wait(); err != nil {
		// tar already streamed; a trailing failure (e.g. file changed while
		// reading) is logged but headers cannot be rewritten.
		logf("backup %s: tar exited with error: %v", ref, err)
	}
}

// restoreBackup accepts a tar.gz body and extracts it into the data root.
// With ?clean=1 the root is emptied first (exact restore); without it the
// archive is extracted on top of existing files.
func restoreBackup(w http.ResponseWriter, r *http.Request, ref, root string) {
	clean := r.URL.Query().Get("clean") == "1"
	if clean {
		// Empty the data root, but never the root itself.
		script := fmt.Sprintf("find %s -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +", shq(root))
		if _, err := agent.ExecCapture(r.Context(), ref, script, nil); err != nil {
			writeError(w, http.StatusInternalServerError, "clean data root: "+err.Error())
			return
		}
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Feed the request body straight into tar inside the container. -i makes
	// cardinal pipe our stdin into the container process.
	out, wait, err := agent.RawExec(ctx, ref, []string{"/bin/sh", "-c",
		fmt.Sprintf("tar xzf - -C %s 2>&1", shq(root))}, r.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "restore exec: "+err.Error())
		return
	}
	defer out.Close()

	// Drain stdout so tar never blocks on a full pipe; report it in the
	// response if the restore failed.
	var drainErr error
	buf := make([]byte, 4096)
	go func() {
		for {
			if _, err := out.Read(buf); err != nil {
				return
			}
		}
	}()
	_ = drainErr

	if err := wait(); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "exit status") {
			msg = "tar failed inside the container (is the image based on busybox/debian with tar?)"
		}
		writeError(w, http.StatusInternalServerError, "restore: "+msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"restored": ref, "clean": fmt.Sprintf("%t", clean)})
}

// copyAndClose copies src into w and closes src, tolerating client aborts.
func copyAndClose(w http.ResponseWriter, src interface{ Read([]byte) (int, error) }) (int64, error) {
	buf := make([]byte, 256*1024)
	var total int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return total, werr
			}
			total += int64(n)
		}
		if err != nil {
			return total, nil // EOF expected; anything else ends the stream too
		}
	}
}
