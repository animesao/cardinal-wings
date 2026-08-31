package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/animesao/cardinal-wings/internal/agent"
)

// fmEntry is one directory entry returned by /fm/list.
type fmEntry struct {
	Type    string `json:"type"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
	Name    string `json:"name"`
}

type containerState struct {
	ImageName string        `json:"image_name"`
	ImageTag  string        `json:"image_tag"`
	Volumes   []volumeMount `json:"volumes,omitempty"`
}

type volumeMount struct {
	Type           string `json:"type,omitempty"`
	Source         string `json:"source"`
	Target         string `json:"target"`
	ReadOnly       bool   `json:"read_only,omitempty"`
	Propagation    string `json:"propagation,omitempty"`
	SELinuxRelabel string `json:"selinux_relabel,omitempty"`
}

// fmSession serializes mount setup and keeps every operation jailed to one
// persistent container data root. The lock also prevents two requests from
// racing while they temporarily mount an overlay for a stopped container.
type fmSession struct {
	root             string
	merged           string
	data             string
	temporaryData    bool
	temporaryFS      bool
	temporaryVolumes []string
	unlock           func()
}

var fmMountMu sync.Mutex

// Kept for compatibility with the existing script parser tests. The live
// file-manager path no longer executes this script; it uses host filesystem
// operations instead.
const fmListScript = `P=%s; cd -- "$P" 2>/dev/null || { echo "__ERR__no such dir"; exit 1; }; for f in "$P"/.[!.]* "$P"/*; do [ -e "$f" ] || continue; if [ -d "$f" ]; then type=d; size=0; else type=f; size=$(wc -c < "$f" 2>/dev/null || echo 0); fi; m=$(stat -c %%Y "$f" 2>/dev/null || echo 0); n=$(printf '%%s' "${f##*/}" | base64 | tr -d '\n'); printf '%%s\t%%s\t%%s\t%%s\n' "$type" "$size" "$m" "$n"; done`

func validContainerID(id string) bool {
	return id != "" && id != "." && id != ".." && filepath.Base(id) == id && !strings.ContainsAny(id, `/\\`)
}

func isMounted(target string) bool {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	target = filepath.Clean(target)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		// mountinfo field 5 is the mount point and may contain escaped spaces.
		if unescapeMountField(fields[4]) == target {
			return true
		}
	}
	return false
}

func unescapeMountField(value string) string {
	value = strings.ReplaceAll(value, `\040`, " ")
	value = strings.ReplaceAll(value, `\011`, "\t")
	return strings.ReplaceAll(value, `\134`, `\`)
}

// beginFMSession makes sure disk-backed data and the overlay are available.
// Cardinal normally leaves these mounts in place after stop. If the host was
// rebooted while the container was stopped, Wings recreates them on demand.
func beginFMSession(id string) (*fmSession, error) {
	if !validContainerID(id) {
		return nil, fmt.Errorf("invalid container id")
	}

	base := filepath.Join(cardinalDataDir(), "overlay", id)
	merged := filepath.Join(base, "merged")
	data := filepath.Join(base, "data")
	diskImage := filepath.Join(base, "disk.img")
	upper := filepath.Join(base, "upper")
	work := filepath.Join(base, "work")
	statePath := filepath.Join(cardinalDataDir(), "containers", id+".json")

	fmMountMu.Lock()
	session := &fmSession{
		merged: merged,
		data:   data,
		unlock: fmMountMu.Unlock,
	}
	closeWithError := func(err error) (*fmSession, error) {
		session.cleanupMounts()
		session.unlock()
		session.unlock = nil
		return nil, err
	}

	var state containerState
	stateErr := readJSONIfExists(statePath, &state)
	if stateErr != nil && !isMounted(merged) {
		return closeWithError(fmt.Errorf("read container state: %w", stateErr))
	}

	// A disk-limited container stores its overlay upper/work directories inside
	// the ext4 data image. Mount it before checking the overlay paths.
	if _, err := os.Stat(diskImage); err == nil && !isMounted(data) {
		if err := os.MkdirAll(data, 0755); err != nil {
			return closeWithError(fmt.Errorf("create data mount point: %w", err))
		}
		out, err := exec.Command("mount", "-o", "loop", diskImage, data).CombinedOutput()
		if err != nil {
			return closeWithError(fmt.Errorf("mount persistent data: %s: %w", strings.TrimSpace(string(out)), err))
		}
		session.temporaryData = true
	}

	if !isMounted(merged) {
		if state.ImageName == "" || state.ImageTag == "" {
			return closeWithError(fmt.Errorf("container image metadata is missing"))
		}
		lower := filepath.Join(cardinalDataDir(), "images", filepath.FromSlash(state.ImageName), state.ImageTag, "rootfs")
		if _, err := os.Stat(lower); err != nil {
			return closeWithError(fmt.Errorf("container image rootfs unavailable: %w", err))
		}
		if session.temporaryData {
			upper = filepath.Join(data, "upper")
			work = filepath.Join(data, "work")
		}
		if isMounted(data) {
			upper = filepath.Join(data, "upper")
			work = filepath.Join(data, "work")
		}
		if err := os.MkdirAll(upper, 0755); err != nil {
			return closeWithError(fmt.Errorf("create overlay upper: %w", err))
		}
		if err := os.MkdirAll(work, 0755); err != nil {
			return closeWithError(fmt.Errorf("create persistent work: %w", err))
		}
		if err := os.MkdirAll(merged, 0755); err != nil {
			return closeWithError(fmt.Errorf("create overlay mount point: %w", err))
		}
		options := "lowerdir=" + lower + ",upperdir=" + upper + ",workdir=" + work
		out, err := exec.Command("mount", "-t", "overlay", "overlay", "-o", options, merged).CombinedOutput()
		if err != nil {
			return closeWithError(fmt.Errorf("mount container overlay: %s: %w", strings.TrimSpace(string(out)), err))
		}
		session.temporaryFS = true
	}

	if stateErr == nil {
		if err := mountConfiguredVolumes(session, state.Volumes); err != nil {
			return closeWithError(err)
		}
	}

	for _, candidate := range []string{
		filepath.Join(merged, "data"),
		filepath.Join(merged, "home", "container"),
		merged,
	} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			session.root = candidate
			return session, nil
		}
	}
	return closeWithError(fmt.Errorf("container data directory is unavailable"))
}

func (s *fmSession) cleanupMounts() {
	if s == nil {
		return
	}
	// Volume mounts live below the overlay mount and must be removed first.
	for i := len(s.temporaryVolumes) - 1; i >= 0; i-- {
		_ = exec.Command("umount", s.temporaryVolumes[i]).Run()
	}
	s.temporaryVolumes = nil
	if s.temporaryFS {
		_ = exec.Command("umount", s.merged).Run()
		s.temporaryFS = false
	}
	if s.temporaryData {
		_ = exec.Command("umount", s.data).Run()
		s.temporaryData = false
	}
}

func (s *fmSession) Close() {
	if s == nil {
		return
	}
	s.cleanupMounts()
	if s.unlock != nil {
		s.unlock()
		s.unlock = nil
	}
}

// mountConfiguredVolumes restores the persistent mounts recorded by Cardinal
// after a host reboot. Runtime volume mounts are normally already present while
// a container is stopped; this path only creates missing mounts and records
// them for cleanup when the file-manager request ends.
func mountConfiguredVolumes(session *fmSession, volumes []volumeMount) error {
	for _, volume := range volumes {
		target := filepath.Join(session.merged, filepath.FromSlash(strings.TrimPrefix(filepath.Clean(volume.Target), string(filepath.Separator))))
		if target == session.merged || !isInside(session.merged, target) {
			return fmt.Errorf("unsafe volume target %q", volume.Target)
		}
		if isMounted(target) {
			continue
		}
		if volume.Target == "" || !strings.HasPrefix(volume.Target, "/") || strings.Contains(volume.Target, "\\") {
			return fmt.Errorf("invalid volume target %q", volume.Target)
		}
		if err := os.MkdirAll(target, 0755); err != nil {
			return fmt.Errorf("create volume target %q: %w", volume.Target, err)
		}

		mountType := volume.Type
		if mountType == "" {
			if volume.Source != "" && !strings.Contains(volume.Source, "/") && !strings.Contains(volume.Source, "\\") {
				mountType = "volume"
			} else {
				mountType = "bind"
			}
		}
		var source string
		switch mountType {
		case "volume":
			if !validContainerID(volume.Source) {
				return fmt.Errorf("invalid named volume %q", volume.Source)
			}
			source = filepath.Join(cardinalDataDir(), "volumes", volume.Source)
		case "bind":
			if volume.Source == "" || !filepath.IsAbs(filepath.FromSlash(volume.Source)) {
				return fmt.Errorf("bind source must be absolute: %q", volume.Source)
			}
			source = filepath.Clean(volume.Source)
		case "nfs":
			if volume.Source == "" {
				return fmt.Errorf("NFS source is empty")
			}
			out, err := exec.Command("mount", "-t", "nfs", volume.Source, target).CombinedOutput()
			if err != nil {
				return fmt.Errorf("mount NFS %q: %s: %w", volume.Source, strings.TrimSpace(string(out)), err)
			}
			session.temporaryVolumes = append(session.temporaryVolumes, target)
			continue
		case "tmpfs":
			// tmpfs is intentionally not recreated while stopped: its contents
			// are ephemeral and recreating it would hide the persistent overlay.
			continue
		default:
			return fmt.Errorf("unsupported volume type %q", mountType)
		}

		if _, err := os.Stat(source); err != nil {
			return fmt.Errorf("volume source %q unavailable: %w", source, err)
		}
		if err := exec.Command("mount", "--bind", source, target).Run(); err != nil {
			return fmt.Errorf("mount volume %q: %w", volume.Source, err)
		}
		if volume.ReadOnly {
			if err := exec.Command("mount", "-o", "remount,bind,ro", target).Run(); err != nil {
				_ = exec.Command("umount", target).Run()
				return fmt.Errorf("make volume %q read-only: %w", volume.Source, err)
			}
		}
		session.temporaryVolumes = append(session.temporaryVolumes, target)
	}
	return nil
}

func isInside(root, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// shq is retained for the backup fallback, which still executes tar inside
// a running container when no host overlay is available.
func shq(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// fmRoot returns the in-container data path used by the backup fallback. File
// manager requests themselves use the host-side session above.
func fmRoot(ctx context.Context, id string) string {
	out, err := agent.ExecCapture(ctx, id, `for d in /data /home/container; do [ -d "$d" ] && { echo "$d"; exit 0; }; done; echo /`, nil)
	if err != nil {
		return "/"
	}
	root := strings.TrimSpace(out)
	if strings.HasPrefix(root, "/") {
		return root
	}
	return "/"
}

func readJSONIfExists(path string, value interface{}) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewDecoder(file).Decode(value)
}

// resolve keeps a path inside the session root and rejects symlinks that point
// outside it. The parent is checked separately so new files cannot escape via
// a symlinked directory.
func (s *fmSession) resolve(requestPath string, allowMissing bool) (string, error) {
	if requestPath == "" {
		requestPath = "/"
	}
	clean := filepath.Clean(filepath.Join(string(filepath.Separator), filepath.FromSlash(requestPath)))
	full := filepath.Join(s.root, clean)
	rel, err := filepath.Rel(s.root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", os.ErrPermission
	}

	check := full
	if allowMissing {
		// New files and nested directories do not exist yet. Resolve the
		// nearest existing parent, then verify that parent remains inside the
		// data root before allowing the caller to create the missing suffix.
		check = full
		for {
			if _, statErr := os.Lstat(check); statErr == nil {
				break
			}
			next := filepath.Dir(check)
			if next == check {
				return "", os.ErrNotExist
			}
			check = next
		}
	}
	real, err := filepath.EvalSymlinks(check)
	if err != nil {
		return "", err
	}
	rootReal, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return "", err
	}
	rel, err = filepath.Rel(rootReal, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", os.ErrPermission
	}
	return full, nil
}

func pathArg(r *http.Request, body map[string]interface{}, name string) (string, bool) {
	// Query paths from the panel are base64 encoded. JSON body paths are already
	// plain strings; decoding them as base64 corrupts valid names such as
	// "test" or "logs".
	if raw := r.URL.Query().Get(name); raw != "" {
		if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && base64.StdEncoding.EncodeToString(decoded) == raw {
			return string(decoded), true
		}
		return raw, true
	}
	if body != nil {
		if value, ok := body[name].(string); ok && value != "" {
			return value, true
		}
	}
	return "", false
}

func decodeBody(r *http.Request) map[string]interface{} {
	body := map[string]interface{}{}
	_ = json.NewDecoder(r.Body).Decode(&body)
	return body
}

func handleFm(w http.ResponseWriter, r *http.Request, id string) {
	_, action := splitRef(r.URL.Path)
	switch {
	case strings.HasPrefix(action, "fm/list"):
		handleFmList(w, r, id)
	case strings.HasPrefix(action, "fm/read"):
		handleFmRead(w, r, id, false)
	case strings.HasPrefix(action, "fm/download"):
		handleFmRead(w, r, id, true)
	case strings.HasPrefix(action, "fm/write"), strings.HasPrefix(action, "fm/upload"):
		handleFmWrite(w, r, id)
	case strings.HasPrefix(action, "fm/mkdir"):
		handleFmMkdir(w, r, id)
	case strings.HasPrefix(action, "fm/rm"):
		handleFmRm(w, r, id)
	case strings.HasPrefix(action, "fm/move"):
		handleFmMove(w, r, id)
	case strings.HasPrefix(action, "fm/chmod"):
		handleFmChmod(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "unknown fm action: "+action)
	}
}

func handleFmList(w http.ResponseWriter, r *http.Request, id string) {
	session, err := beginFMSession(id)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "fm/list: %s", err.Error())
		return
	}
	defer session.Close()

	requestPath, ok := pathArg(r, nil, "path")
	if !ok {
		requestPath = "/"
	}
	dir, err := session.resolve(requestPath, false)
	if err != nil {
		writeErr(w, http.StatusNotFound, ErrNotFound, "path not found: %s", requestPath)
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeErr(w, http.StatusNotFound, ErrNotFound, "path not found: %s", requestPath)
		return
	}

	result := make([]fmEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		typ := "file"
		if info.IsDir() {
			typ = "dir"
		}
		result = append(result, fmEntry{Type: typ, Size: info.Size(), ModTime: info.ModTime().Unix(), Name: entry.Name()})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"path": requestPath, "entries": result})
}

func handleFmRead(w http.ResponseWriter, r *http.Request, id string, download bool) {
	session, err := beginFMSession(id)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "fm/read: %s", err.Error())
		return
	}
	defer session.Close()

	requestPath, ok := pathArg(r, nil, "path")
	if !ok || requestPath == "" {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	filePath, err := session.resolve(requestPath, false)
	if err != nil {
		writeErr(w, http.StatusNotFound, ErrNotFound, "file not found: %s", requestPath)
		return
	}
	info, err := os.Stat(filePath)
	if err != nil || !info.Mode().IsRegular() {
		writeErr(w, http.StatusNotFound, ErrNotFound, "file not found: %s", requestPath)
		return
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		writeErr(w, http.StatusNotFound, ErrNotFound, "read %s: %s", requestPath, err.Error())
		return
	}
	if download {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"path":    requestPath,
			"content": base64.StdEncoding.EncodeToString(data),
			"size":    len(data),
			"binary":  true,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":    requestPath,
		"content": string(data),
		"size":    len(data),
	})
}

func handleFmWrite(w http.ResponseWriter, r *http.Request, id string) {
	session, err := beginFMSession(id)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "fm/write: %s", err.Error())
		return
	}
	defer session.Close()

	body := decodeBody(r)
	requestPath, ok := pathArg(r, body, "path")
	if !ok || requestPath == "" {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	contentB64, _ := body["content"].(string)
	content, err := base64.StdEncoding.DecodeString(contentB64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "content must be base64")
		return
	}
	filePath, err := session.resolve(requestPath, true)
	if err != nil {
		writeErr(w, http.StatusBadRequest, ErrBadRequest, "invalid path: %s", requestPath)
		return
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "create parent: %s", err.Error())
		return
	}
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "write %s: %s", requestPath, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "path": requestPath, "size": len(content)})
}

func handleFmMkdir(w http.ResponseWriter, r *http.Request, id string) {
	session, err := beginFMSession(id)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "fm/mkdir: %s", err.Error())
		return
	}
	defer session.Close()
	body := decodeBody(r)
	requestPath, ok := pathArg(r, body, "path")
	if !ok || requestPath == "" {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	dir, err := session.resolve(requestPath, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "mkdir %s: %s", requestPath, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "path": requestPath})
}

func handleFmRm(w http.ResponseWriter, r *http.Request, id string) {
	session, err := beginFMSession(id)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "fm/rm: %s", err.Error())
		return
	}
	defer session.Close()
	body := decodeBody(r)
	requestPath, ok := pathArg(r, body, "path")
	if !ok || requestPath == "" {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	target, err := session.resolve(requestPath, false)
	if err != nil || filepath.Clean(target) == filepath.Clean(session.root) {
		writeError(w, http.StatusBadRequest, "refusing to remove the data root")
		return
	}
	if err := os.RemoveAll(target); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "remove %s: %s", requestPath, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "path": requestPath})
}

func handleFmMove(w http.ResponseWriter, r *http.Request, id string) {
	session, err := beginFMSession(id)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "fm/move: %s", err.Error())
		return
	}
	defer session.Close()
	body := decodeBody(r)
	src, ok := pathArg(r, body, "src")
	if !ok || src == "" {
		writeError(w, http.StatusBadRequest, "src required")
		return
	}
	dst, ok := pathArg(r, body, "dst")
	if !ok || dst == "" {
		writeError(w, http.StatusBadRequest, "dst required")
		return
	}
	source, err := session.resolve(src, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid source path")
		return
	}
	target, err := session.resolve(dst, true)
	if err != nil || filepath.Clean(source) == filepath.Clean(session.root) || filepath.Clean(target) == filepath.Clean(session.root) {
		writeError(w, http.StatusBadRequest, "invalid destination path")
		return
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "create destination: %s", err.Error())
		return
	}
	if err := os.Rename(source, target); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "move %s: %s", src, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "src": src, "dst": dst})
}

func handleFmChmod(w http.ResponseWriter, r *http.Request, id string) {
	session, err := beginFMSession(id)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "fm/chmod: %s", err.Error())
		return
	}
	defer session.Close()
	body := decodeBody(r)
	requestPath, ok := pathArg(r, body, "path")
	if !ok {
		requestPath, ok = pathArg(r, body, "file")
	}
	mode, _ := body["mode"].(string)
	if !ok || !regexpOctal(mode) {
		writeError(w, http.StatusBadRequest, "file and mode required")
		return
	}
	target, err := session.resolve(requestPath, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	parsed, _ := strconv.ParseUint(mode, 8, 32)
	if err := os.Chmod(target, fs.FileMode(parsed)); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "chmod %s: %s", requestPath, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "path": requestPath, "mode": mode})
}

func regexpOctal(value string) bool {
	return len(value) == 3 && value[0] >= '0' && value[0] <= '7' && value[1] >= '0' && value[1] <= '7' && value[2] >= '0' && value[2] <= '7'
}
