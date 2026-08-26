package server

import (
	"net/http"
	"sync"

	"github.com/animesao/cardinal-wings/internal/runtime"
)

// clusterRoutes mounts the Phase 5 cluster node endpoints. These aggregate
// the local node and any configured remote cluster nodes.
func clusterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/nodes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var nodes []runtime.NodeInfo
		if defaultClient != nil {
			nodes = append(nodes, runtime.NodeInfo{Name: "local", URL: defaultClient.Base(), Status: "up"})
		}
		for _, n := range remoteNodesSnapshot() {
			nodes = append(nodes, n)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"nodes": nodes})
	})

	mux.HandleFunc("/v1/cluster/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		status, err := defaultClient.Health(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
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
			writeError(w, http.StatusNotImplemented, "replica create via panel not yet wired — use service commands")
			return
		}
		replicas, err := defaultClient.Replicas(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"replicas": replicas})
	})

	mux.HandleFunc("/v1/cluster/containers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		containers, err := defaultClient.ContainersOnNode(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"containers": containers})
	})
}

// remoteNodesMu guards remoteNodesSnapshot.
var remoteNodesMu sync.RWMutex
var remoteNodes = []runtime.NodeInfo{}

func setRemoteNodes(nodes []runtime.NodeInfo) {
	remoteNodesMu.Lock()
	defer remoteNodesMu.Unlock()
	remoteNodes = nodes
}

func remoteNodesSnapshot() []runtime.NodeInfo {
	remoteNodesMu.RLock()
	defer remoteNodesMu.RUnlock()
	out := make([]runtime.NodeInfo, len(remoteNodes))
	copy(out, remoteNodes)
	return out
}
