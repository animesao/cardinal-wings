package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/animesao/cardinal-wings/internal/agent"
)

// clusterRoutes mounts the cluster node endpoints. These aggregate the local
// node and any configured remote cluster nodes; an optional `?node=<name>`
// selects a specific node for the underlying cardinal calls.
func clusterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/nodes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		names := registry.names()
		snapshot := checker.snapshot()
		nodes := make([]map[string]interface{}, 0, len(names))
		for _, name := range names {
			c := registry.byName(name)
			if c == nil {
				continue
			}
			node := map[string]interface{}{
				"name":  name,
				"url":   c.Base(),
				"local": name == "local",
			}
			if st, ok := snapshot[name]; ok {
				node["status"] = st.status
				node["checked_at"] = st.checkedAt
				if st.err != "" {
					node["error"] = st.err
				}
			} else {
				node["status"] = "unknown"
			}
			nodes = append(nodes, node)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"nodes": nodes})
	})

	mux.HandleFunc("/v1/cluster/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		c, ok := clientFor(w, r)
		if !ok {
			return
		}
		status, err := c.Health(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, ErrUpstream, "node health: %s", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": status})
	})

	mux.HandleFunc("/v1/cluster/replicas", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			c, ok := clientFor(w, r)
			if !ok {
				return
			}
			// cardinal serve has no GET /cluster/replicas list; the local
			// orchestrator state is exposed via `cardinal service ls`.
			// Try the daemon first, fall back to the CLI so the panel
			// always gets a usable list instead of a 405.
			if replicas, err := c.Replicas(r.Context()); err == nil {
				writeJSON(w, http.StatusOK, map[string]interface{}{"replicas": replicas})
				return
			}
			out, err := agent.ServiceList(r.Context())
			if err != nil {
				writeErr(w, http.StatusBadGateway, ErrUpstream, "replicas: %s", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"replicas": out})
		case http.MethodPost:
			c, ok := clientFor(w, r)
			if !ok {
				return
			}
			var req map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
				return
			}
			if _, hasImage := req["image"]; !hasImage {
				writeError(w, http.StatusBadRequest, "image required")
				return
			}
			out, err := c.ReplicaCreate(r.Context(), req)
			if err != nil {
				writeErr(w, http.StatusBadGateway, ErrUpstream, "replica create: %s", err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, out)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	mux.HandleFunc("/v1/cluster/replicas/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		c, ok := clientFor(w, r)
		if !ok {
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/v1/cluster/replicas/")
		if id == "" {
			writeError(w, http.StatusBadRequest, "replica id required")
			return
		}
		out, err := c.ReplicaRemove(r.Context(), id)
		if err != nil {
			writeErr(w, http.StatusBadGateway, ErrUpstream, "replica remove: %s", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("/v1/cluster/info", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		out, err := agent.ClusterInfo(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, ErrUpstream, "cluster info: %s", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"output": out})
	})

	mux.HandleFunc("/v1/cluster/containers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		c, ok := clientFor(w, r)
		if !ok {
			return
		}
		containers, err := c.ContainersOnNode(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, ErrUpstream, "cluster containers: %s", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"containers": containers})
	})
}
