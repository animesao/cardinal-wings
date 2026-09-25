package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/animesao/cardinal-wings/internal/agent"
	"github.com/animesao/cardinal-wings/internal/runtime"
)

// handleContainerRename proxies POST /v1/containers/{id}/rename?name=
// to cardinal's rename endpoint.
func handleContainerRename(w http.ResponseWriter, r *http.Request, ref string, c *runtime.Client) {
	name := r.URL.Query().Get("name")
	if name == "" {
		var req struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		name = req.Name
	}
	if name == "" {
		writeError(w, http.StatusBadRequest, "name required (?name= or {\"name\": ...})")
		return
	}
	if err := c.Rename(r.Context(), ref, name); err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "rename %s: %s", ref, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": ref, "name": name, "ok": "true"})
}

// handleContainerTop proxies GET /v1/containers/{id}/top?ps_args=aux.
func handleContainerTop(w http.ResponseWriter, r *http.Request, ref string, c *runtime.Client) {
	out, err := c.Top(r.Context(), ref, r.URL.Query().Get("ps_args"))
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "top %s: %s", ref, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleContainerWait proxies POST /v1/containers/{id}/wait.
func handleContainerWait(w http.ResponseWriter, r *http.Request, ref string, c *runtime.Client) {
	out, err := c.Wait(r.Context(), ref)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "wait %s: %s", ref, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleContainerChanges proxies GET /v1/containers/{id}/changes.
func handleContainerChanges(w http.ResponseWriter, r *http.Request, ref string, c *runtime.Client) {
	out, err := c.Changes(r.Context(), ref)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "changes %s: %s", ref, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"changes": out})
}

// handleContainerExport streams the container filesystem tar
// (cardinal GET /containers/{id}/export) straight to the caller.
func handleContainerExport(w http.ResponseWriter, r *http.Request, ref string, c *runtime.Client) {
	resp, err := c.Proxy(r.Context(), "GET", "/containers/"+ref+"/export")
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "export %s: %s", ref, err.Error())
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", "attachment; filename=\"container-"+ref+"-export.tar\"")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, resp.Body)
}

// handleContainerCommit runs `cardinal commit <id> <repo>[:tag]`
// (cardinal has no commit HTTP endpoint, so wings delegates to the CLI).
func handleContainerCommit(w http.ResponseWriter, r *http.Request, ref string) {
	var req struct {
		Repo string `json:"repo"`
		Tag  string `json:"tag"`
		Ref  string `json:"ref"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	target := req.Ref
	if target == "" {
		target = req.Repo
		if target == "" {
			target = r.URL.Query().Get("repo")
		}
		if target != "" {
			if tag := req.Tag; tag != "" {
				target += ":" + tag
			} else if qtag := r.URL.Query().Get("tag"); qtag != "" {
				target += ":" + qtag
			}
		}
	}
	if target == "" {
		writeError(w, http.StatusBadRequest, "repo (or ref) required")
		return
	}
	out, err := agent.Commit(r.Context(), ref, target)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "commit %s: %s", ref, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": ref, "image": target, "output": out})
}

// handleContainerPortsShow runs `cardinal port <id>`.
func handleContainerPortsShow(w http.ResponseWriter, r *http.Request, ref string) {
	out, err := agent.PortShow(r.Context(), ref)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "ports %s: %s", ref, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": ref, "ports": out})
}

// handleContainerPortsMut handles ports/add and ports/remove.
func handleContainerPortsMut(w http.ResponseWriter, r *http.Request, ref, action string) {
	var req struct {
		Mapping string `json:"mapping"`
		Port    string `json:"port"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	mapping := req.Mapping
	if mapping == "" {
		mapping = req.Port
	}
	if mapping == "" {
		mapping = r.URL.Query().Get("mapping")
	}
	if mapping == "" {
		mapping = r.URL.Query().Get("port")
	}
	if mapping == "" {
		writeError(w, http.StatusBadRequest, "mapping required (host:container[/proto])")
		return
	}
	var err error
	if action == "ports/add" {
		err = agent.PortAdd(r.Context(), ref, mapping)
	} else {
		err = agent.PortRemove(r.Context(), ref, mapping)
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "%s %s: %s", action, ref, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": ref, "action": action, "mapping": mapping, "ok": "true"})
}
