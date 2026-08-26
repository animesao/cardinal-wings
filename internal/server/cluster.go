package server

import (
	"net/http"
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
		nodes := make([]map[string]interface{}, 0, len(names))
		for _, name := range names {
			c := registry.byName(name)
			if c == nil {
				continue
			}
			status := "configured"
			if h, err := c.Health(r.Context()); err == nil && h != "" {
				status = h
			}
			nodes = append(nodes, map[string]interface{}{
				"name":   name,
				"url":    c.Base(),
				"status": status,
				"local":  name == "local",
			})
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
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if r.Method == http.MethodPost {
			writeErr(w, http.StatusNotImplemented, ErrNotImplemented, "replica create via panel not yet wired — use service commands")
			return
		}
		c, ok := clientFor(w, r)
		if !ok {
			return
		}
		replicas, err := c.Replicas(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, ErrUpstream, "replicas: %s", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"replicas": replicas})
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
