package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/animesao/cardinal-wings/internal/agent"
	"github.com/animesao/cardinal-wings/internal/auth"
	"github.com/animesao/cardinal-wings/internal/runtime"
)

// blueprintRoutes mounts the Phase 3 blueprint endpoints. List/inspect read
// the catalog from the official registry; install/uninstall are delegated to
// the `cardinal blueprint` CLI so semantics stay identical to the CLI.
func blueprintRoutes(mux *http.ServeMux, mw *auth.Middleware) {
	mux.HandleFunc("/v1/blueprints", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			c, ok := clientFor(w, r)
			if !ok {
				return
			}
			cat, err := c.BlueprintRegistry(r.Context(), "")
			if err != nil {
				writeErr(w, http.StatusBadGateway, ErrUpstream, "blueprint registry: %s", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, struct {
				Description string                          `json:"description"`
				Blueprints  []runtime.BlueprintCatalogEntry `json:"blueprints"`
			}{cat.Description, cat.List()})
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	mux.HandleFunc("/v1/blueprints/", func(w http.ResponseWriter, r *http.Request) {
		action, _ := splitBlueprintPath(r.URL.Path)
		if action == "install" || action == "uninstall" {
			mw.AdminOnly(http.HandlerFunc(handleBlueprintAction)).ServeHTTP(w, r)
			return
		}
		handleBlueprintRef(w, r)
	})
}

func splitBlueprintPath(path string) (action, name string) {
	trimmed := strings.TrimPrefix(path, "/v1/blueprints/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 0 {
		return "", ""
	}
	name = parts[0]
	if len(parts) == 2 {
		action = parts[1]
	}
	return action, name
}

func handleBlueprintRef(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, name := splitBlueprintPath(r.URL.Path)
	if name == "" {
		writeError(w, http.StatusNotFound, "missing blueprint name")
		return
	}

	c, ok := clientFor(w, r)
	if !ok {
		return
	}
	cat, err := c.BlueprintRegistry(r.Context(), "")
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "blueprint registry: %s", err.Error())
		return
	}
	entry, ok := cat.Blueprints[name]
	if !ok {
		writeErr(w, http.StatusNotFound, ErrNotFound, "blueprint not in registry: %s", name)
		return
	}
	tplURL := strings.TrimSuffix(cat.BaseURL, "/") + "/" + entry.File
	tpl, err := c.BlueprintTemplate(r.Context(), tplURL)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "blueprint template: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		runtime.BlueprintCatalogEntry
		Template *runtime.BlueprintTemplate `json:"template"`
	}{entry, tpl})
}

func handleBlueprintAction(w http.ResponseWriter, r *http.Request) {
	action, name := splitBlueprintPath(r.URL.Path)
	if name == "" {
		writeError(w, http.StatusNotFound, "missing blueprint name")
		return
	}

	if action == "uninstall" && r.Method == http.MethodPost {
		id := taskMgr.Submit("blueprint_uninstall", func(ctx context.Context) (string, error) {
			return "uninstalled " + name, agent.BlueprintUninstall(ctx, name)
		})
		writeJSON(w, http.StatusAccepted, map[string]string{"task_id": id, "action": "uninstall", "name": name})
		return
	}

	if action == "install" && r.Method == http.MethodPost {
		var req struct {
			Name   string   `json:"name"`
			Memory string   `json:"memory,omitempty"`
			CPUs   string   `json:"cpus,omitempty"`
			Env    []string `json:"env,omitempty"`
			Yes    bool     `json:"yes,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		id := taskMgr.Submit("blueprint_install", func(ctx context.Context) (string, error) {
			return agent.BlueprintInstallOut(ctx, name, req.Memory, req.CPUs, req.Env, req.Yes)
		})
		writeJSON(w, http.StatusAccepted, map[string]string{"task_id": id, "action": "install", "name": name})
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}
