// filemanager.go implements a full file manager API on top of container shell
// commands. cardinal's own `fs` CLI is read-only, so wings runs small `sh -c`
// scripts inside the container (through `cardinal exec`) to list, read, write,
// mkdir, rm and mv files. Binary-safe: file names and contents cross the wire
// base64-encoded.
package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/animesao/cardinal-wings/internal/agent"
)

// fmEntry is one directory entry returned by /fm/list.
type fmEntry struct {
	Type    string `json:"type"` // "dir" | "file"
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"` // unix seconds
	Name    string `json:"name"`
}

// shq single-quotes a string for safe embedding in a `sh -c` script. Any
// single quote inside is escaped as the standard '\” sequence.
func shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// fmListScript lists a container dir, one line per entry:
//
//	<type> \t <size> \t <mtime> \t <base64(name)>
//
// type is "d" or "f". The single `%s` is the (shell-quoted) directory; every
// other `%%…` survives fmt.Sprintf as a literal `%` in the generated script.
// The file name is base64-encoded so spaces/unicode/tabs are safe on the wire.
const fmListScript = `P=%s; cd -- "$P" 2>/dev/null || { echo "__ERR__no such dir"; exit 1; }; for f in "$P"/.[!.]* "$P"/*; do [ -e "$f" ] || continue; if [ -d "$f" ]; then echo -n d; printf '\t0'; else s=$(wc -c < "$f" 2>/dev/null || echo 0); echo -n f; printf '\t%%s' "$s"; fi; m=$(stat -c %%Y "$f" 2>/dev/null || echo 0); n=$(printf '%%s' "${f##*/}" | base64 | tr -d '\n'); printf '\t%%s\t%%s\n' "$m" "$n"; done`

// handleFm serves file-manager operations: list | read | download | write |
// upload | mkdir | rm | move. Reads are GET, mutations are POST. Paths are
// passed base64-encoded in query/body so arbitrary characters are safe.
func handleFm(w http.ResponseWriter, r *http.Request, id string) {
	_, action := splitRef(r.URL.Path)
	switch {
	case strings.HasPrefix(action, "fm/list"):
		handleFmList(w, r, id)
	case strings.HasPrefix(action, "fm/read"), strings.HasPrefix(action, "fm/download"):
		handleFmRead(w, r, id)
	case strings.HasPrefix(action, "fm/write"), strings.HasPrefix(action, "fm/upload"):
		handleFmWrite(w, r, id)
	case strings.HasPrefix(action, "fm/mkdir"):
		handleFmMkdir(w, r, id)
	case strings.HasPrefix(action, "fm/rm"):
		handleFmRm(w, r, id)
	case strings.HasPrefix(action, "fm/move"):
		handleFmMove(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "unknown fm action: "+action)
	}
}

// pathArg decodes a base64 path from a query param or JSON body field. A
// non-base64 (plain) value is returned as-is.
func pathArg(r *http.Request, body map[string]interface{}, name string) (string, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		if s, ok := body[name].(string); ok {
			raw = s
		}
	}
	if raw == "" {
		return "", false
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil {
		return string(b), true
	}
	return raw, true
}

func decodeBody(r *http.Request) map[string]interface{} {
	m := map[string]interface{}{}
	_ = json.NewDecoder(r.Body).Decode(&m)
	return m
}

func handleFmList(w http.ResponseWriter, r *http.Request, id string) {
	path, ok := pathArg(r, nil, "path")
	if !ok {
		path = "/"
	}
	script := fmt.Sprintf(fmListScript, shq(path))
	out, err := agent.ExecCapture(r.Context(), id, script, nil)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "fm/list: %s", err.Error())
		return
	}
	if strings.Contains(out, "__ERR__no such dir") {
		writeErr(w, http.StatusNotFound, ErrNotFound, "path not found: %s", path)
		return
	}
	entries := []fmEntry{}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		typ := "file"
		if parts[0] == "d" {
			typ = "dir"
		}
		size, _ := strconv.ParseInt(parts[1], 10, 64)
		mt, _ := strconv.ParseInt(parts[2], 10, 64)
		name := ""
		if len(parts) >= 4 {
			if n, err := base64.StdEncoding.DecodeString(parts[3]); err == nil {
				name = string(n)
			} else {
				name = parts[3]
			}
		}
		entries = append(entries, fmEntry{Type: typ, Size: size, ModTime: mt, Name: name})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"path": path, "entries": entries})
}

func handleFmRead(w http.ResponseWriter, r *http.Request, id string) {
	path, ok := pathArg(r, nil, "path")
	if !ok || path == "" {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	script := "base64 < " + shq(path) + " 2>/dev/null || exit 1"
	out, err := agent.ExecCapture(r.Context(), id, script, nil)
	if err != nil {
		writeErr(w, http.StatusNotFound, ErrNotFound, "fm/read %s: %s", path, err.Error())
		return
	}
	content, err := base64.StdEncoding.DecodeString(strings.TrimSpace(out))
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "fm/read: decode: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":    path,
		"content": string(content),
		"size":    len(content),
	})
}

func handleFmWrite(w http.ResponseWriter, r *http.Request, id string) {
	body := decodeBody(r)
	path, ok := pathArg(r, body, "path")
	if !ok || path == "" {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	contentB64, _ := body["content"].(string)
	content, err := base64.StdEncoding.DecodeString(contentB64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "content must be base64")
		return
	}
	script := "mkdir -p -- $(dirname -- " + shq(path) + ") && base64 -d > " + shq(path)
	if _, err := agent.ExecCapture(r.Context(), id, script, []byte(contentB64+"\n")); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "fm/write: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "path": path, "size": len(content)})
}

func handleFmMkdir(w http.ResponseWriter, r *http.Request, id string) {
	body := decodeBody(r)
	path, ok := pathArg(r, body, "path")
	if !ok || path == "" {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	recursive := false
	if v, _ := body["recursive"].(bool); v {
		recursive = true
	}
	script := "mkdir"
	if recursive {
		script += " -p"
	}
	script += " -- " + shq(path)
	if _, err := agent.ExecCapture(r.Context(), id, script, nil); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "fm/mkdir: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "path": path})
}

func handleFmRm(w http.ResponseWriter, r *http.Request, id string) {
	body := decodeBody(r)
	path, ok := pathArg(r, body, "path")
	if !ok || path == "" {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	script := "rm -rf -- " + shq(path)
	if _, err := agent.ExecCapture(r.Context(), id, script, nil); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "fm/rm: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "path": path})
}

func handleFmMove(w http.ResponseWriter, r *http.Request, id string) {
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
	script := "mkdir -p -- $(dirname -- " + shq(dst) + ") && mv -f -- " + shq(src) + " " + shq(dst)
	if _, err := agent.ExecCapture(r.Context(), id, script, nil); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "fm/move: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "src": src, "dst": dst})
}
