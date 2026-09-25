package agent

import (
	"strings"

	"github.com/animesao/cardinal-wings/internal/config"
	"github.com/animesao/cardinal-wings/internal/runtime"
)

// RemoteNode bundles a node's display name and its runtime client.
type RemoteNode struct {
	Name   string
	Client *runtime.Client
}

// RemoteClients builds runtime clients for all configured, enabled nodes.
// Address normalization fills in an http:// scheme when omitted.
func RemoteClients(cfg *config.Config) []RemoteNode {
	out := make([]RemoteNode, 0)
	for _, n := range cfg.Nodes {
		if !n.Enabled {
			continue
		}
		addr := n.Address
		if !strings.Contains(addr, "://") {
			addr = "http://" + addr
		}
		out = append(out, RemoteNode{Name: n.Name, Client: runtime.NewClient(addr, n.Token)})
	}
	return out
}
