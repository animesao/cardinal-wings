package server

import (
	"net/http"
	"sync"

	"github.com/animesao/cardinal-wings/internal/runtime"
)

// nodeEntry holds one node's display name and its runtime client.
type nodeEntry struct {
	name   string
	client *runtime.Client
	local  bool
}

// nodeRegistry is the set of cardinal nodes wings can talk to: the local node
// (spawned at boot) plus any configured remote nodes. Handlers resolve which
// node to hit from the `?node=<name>` query parameter, defaulting to local.
type nodeRegistry struct {
	mu    sync.RWMutex
	nodes []nodeEntry
}

// registry is the process-wide node set, populated by Run before anything
// serves requests.
var registry = &nodeRegistry{}

// registerNodes replaces the registry contents with the live set.
func registerNodes(entries []nodeEntry) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.nodes = entries
}

// local returns the local node client, or nil if not yet started.
func (nr *nodeRegistry) local() *runtime.Client {
	nr.mu.RLock()
	defer nr.mu.RUnlock()
	for _, n := range nr.nodes {
		if n.local {
			return n.client
		}
	}
	return nil
}

// byName returns the client for a named node (exact match), or nil.
func (nr *nodeRegistry) byName(name string) *runtime.Client {
	nr.mu.RLock()
	defer nr.mu.RUnlock()
	for _, n := range nr.nodes {
		if n.name == name {
			return n.client
		}
	}
	return nil
}

// names returns every node name in registration order (local first).
func (nr *nodeRegistry) names() []string {
	nr.mu.RLock()
	defer nr.mu.RUnlock()
	out := make([]string, 0, len(nr.nodes))
	for _, n := range nr.nodes {
		out = append(out, n.name)
	}
	return out
}

// clientFor resolves the node a request targets. An explicit `?node=<name>`
// selects that node; an unknown name is treated as an error. An empty value
// selects the local node. Returns the resolved client, and false when the
// caller may proceed (all errors are written before returning).
// clientFor resolves the node a request targets. An explicit `?node=<name>`
// selects that node; an unknown name is treated as an error. An empty value
// selects the local node. The bool reports whether the caller may proceed
// (false means an error has already been written to w).
func clientFor(w http.ResponseWriter, r *http.Request) (*runtime.Client, bool) {
	name := r.URL.Query().Get("node")
	if name == "" {
		return registry.local(), true
	}
	c := registry.byName(name)
	if c == nil {
		writeErr(w, http.StatusNotFound, ErrNotFound, "unknown node: %s", name)
		return nil, false
	}
	return c, true
}
