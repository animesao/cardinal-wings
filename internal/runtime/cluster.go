package runtime

import (
	"context"
	"time"
)

// ClusterHealth is the response cardinal serves at /cluster/health.
type ClusterHealth struct {
	Status string `json:"status"`
}

// NodeInfo is a panel-facing summary of one cardinal node.
type NodeInfo struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Status string `json:"status"`
}

// Health returns the cluster health status for this node.
func (c *Client) Health(ctx context.Context) (string, error) {
	var out ClusterHealth
	if err := c.do(ctx, "GET", "/cluster/health", nil, &out); err != nil {
		return "", err
	}
	if out.Status == "" {
		out.Status = "ok"
	}
	return out.Status, nil
}

// Replicas returns the cluster replicas listing from this node.
func (c *Client) Replicas(ctx context.Context) (interface{}, error) {
	var out interface{}
	if err := c.do(ctx, "GET", "/cluster/replicas", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ContainersOnNode lists containers running on this node's cluster view.
func (c *Client) ContainersOnNode(ctx context.Context) (interface{}, error) {
	var out interface{}
	if err := c.do(ctx, "GET", "/cluster/containers", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PingNode returns nil when the node is reachable and its cluster is healthy.
func (c *Client) PingNode(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.Ping(ctx); err != nil {
		return "down", err
	}
	return "up", nil
}
