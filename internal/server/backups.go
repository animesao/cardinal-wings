package server

// backups.go streams container data as a tar.gz archive. Unlike the file
// manager (per-file base64 over JSON) backups move whole directory trees as
// one raw binary stream: `tar czf - -C <data-root> .` runs inside the
// container through `cardinal exec` and wings pipes it straight to the HTTP
// response. Restore is the reverse: the uploaded archive is piped into
// `tar xzf - -C <data-root>`, optionally after wiping the data root
// (?clean=1) so the restore is exact, not a merge.

import (
	"bytes"
	"context"
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

	// Prefer the host-side data root: it resolves even when the container is
	// stopped (the overlay stays mounted) and never needs exec. The exec-based
	// fmRoot probe is only a fallback for the unmounted-overlay case (e.g.
	// after a host reboot before the container starts), where exec is the only
	// way to ask the container where its data lives.
	root := ""
	if p, _, err := containerDataRoot(ref); err == nil {
		root = p
	} else {
		root = fmRoot(r.Context(), ref)
	}
	if root == "/" || root == "" {
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

// hostDataRoot returns the host filesystem path of the container's data
// directory when the overlay is mounted, so archive create/restore can run
// on the host directly. Host tar is reliable for every base image — running
// tar inside the container (via exec) breaks on images whose entrypoint is
// not a plain shell (e.g. itzg/minecraft-server).
func hostDataRoot(ref string) (string, error) {
	p, mounted, err := containerDataRoot(ref)
	if err != nil {
		return "", err
	}
	if p == "" || !mounted {
		return "", fmt.Errorf("overlay not mounted")
	}
	return p, nil
}

// streamHostBackup pipes `tar czf - -C <root> .` from the host filesystem to
// the response body with zero buffering — the panel (or curl) writes the
// archive to disk while tar is still streamings.
func streamBackup(w http.ResponseWriter, r *http.Request, ref, root string) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Prefer archiving on the host (works for every image).
	if hostRoot, herr := hostDataRoot(ref); herr == nil {
		if hostRoot != "/" && root != "" { // root arg is the container jail; ignore for host path
			cmd := exec.Command("tar", "czf", "-", "-C", hostRoot, ".")
			cmd.Stderr = nil
			out, err := cmd.StdoutPipe()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "backup archive: "+err.Error())
				return
			}
			if err := cmd.Start(); err != nil {
				writeError(w, http.StatusInternalServerError, "backup archive: "+err.Error())
				return
			}

			w.Header().Set("Content-Type", "application/gzip")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"container-%s-backup.tar.gz\"", ref))
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(http.StatusOK)

			if _, err := copyAndClose(w, out); err != nil {
				logf("backup %s: client copy aborted: %v", ref, err)
				cmd.Process.Kill()
				_ = cmd.Wait()
				return
			}
			err = cmd.Wait()
			if err != nil {
				logf("backup %s: host tar exited with error: %v", ref, err)
			}
			return
		}
	}

	// Fallback: tar inside the container (older data layout / unmounted overlay).
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
		logf("backup %s: tar exited with error: %v", ref, err)
	}
}

// restoreBackup accepts a tar.gz body and extracts it into the data root.
// With ?clean=1 the root is emptied first (exact restore); without it the
// archive is extracted on top of existing files. Prefers extracting on the
// host (via the overlay data directory) so it works for every base image;
// falls back to tar inside the container when the overlay isn't mounted.
func restoreBackup(w http.ResponseWriter, r *http.Request, ref, root string) {
	clean := r.URL.Query().Get("clean") == "1"
	if r.ContentLength > 20<<30 {
		writeError(w, http.StatusRequestEntityTooLarge, "backup exceeds 20 GiB limit")
		return
	}
	if root == "/" || filepath.Clean(root) == "/" {
		writeError(w, http.StatusBadRequest, "invalid restore root")
		return
	}

	// Host path restore — robust for images whose entrypoint is not a shell.
	if hostRoot, herr := hostDataRoot(ref); herr == nil && hostRoot != "/" {
		if clean {
			entries, err := os.ReadDir(hostRoot)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "clean data root: "+err.Error())
				return
			}
			var firstErr error
			for _, e := range entries {
				if perr := os.RemoveAll(filepath.Join(hostRoot, e.Name())); perr != nil && firstErr == nil {
					firstErr = perr
				}
			}
			if firstErr != nil {
				writeError(w, http.StatusInternalServerError, "clean data root: "+firstErr.Error())
				return
			}
		}

		cmd := exec.Command("tar", "xzf", "-", "-C", hostRoot, "--no-absolute-names", "--no-same-owner")
		cmd.Stdin = io.LimitReader(r.Body, 20<<30)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("restore: host tar failed: %v %s", err, strings.TrimSpace(stderr.String())))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"restored": ref, "clean": fmt.Sprintf("%t", clean)})
		return
	}

	// Fallback: clean + extract inside the container (unmounted overlay).
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
	buf := make([]byte, 4096)
	go func() {
		for {
			if _, err := out.Read(buf); err != nil {
				return
			}
		}
	}()

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
