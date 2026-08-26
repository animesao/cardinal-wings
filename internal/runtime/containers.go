package runtime

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Summary is an API-facing, panel-friendly container summary.
type Summary struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Image     string            `json:"image"`
	Status    string            `json:"status"`
	IP        string            `json:"ip,omitempty"`
	Ports     []string          `json:"ports,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	CreatedAt string            `json:"created_at"`
}

// PortRef describes a single port mapping.
type PortRef struct {
	Host      int    `json:"host"`
	Container int    `json:"container"`
	Protocol  string `json:"protocol"`
}

// VolumeRef describes a mounted volume.
type VolumeRef struct {
	Type     string `json:"type"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
}

// Detail is the full inspect view.
type Detail struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Image    string            `json:"image"`
	Status   string            `json:"status"`
	IP       string            `json:"ip,omitempty"`
	Ports    []PortRef         `json:"ports,omitempty"`
	Volumes  []VolumeRef       `json:"volumes,omitempty"`
	Env      []string          `json:"env,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
	Restart  string            `json:"restart"`
	Memory   int64             `json:"memory_limit_bytes,omitempty"`
	CPUs     float64           `json:"cpus,omitempty"`
	Platform string            `json:"platform"`
}

// ListContainers returns summaries for all containers on the node.
func (c *Client) ListContainers(ctx context.Context, all bool) ([]Summary, error) {
	path := "/containers/json"
	if all {
		path += "?all=1"
	}
	var raw []dockerContainerSummary
	if err := c.do(ctx, "GET", path, nil, &raw); err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(raw))
	for _, r := range raw {
		out = append(out, summarizeDocker(r))
	}
	return out, nil
}

// Inspect returns full details for a container on the node.
func (c *Client) Inspect(ctx context.Context, id string) (*Detail, error) {
	var raw dockerContainerInspect
	if err := c.do(ctx, "GET", "/containers/"+id+"/json", nil, &raw); err != nil {
		return nil, err
	}
	return dockerDetail(&raw), nil
}

// Action applies a lifecycle operation (start/stop/restart/kill).
func (c *Client) Action(ctx context.Context, id, op string) error {
	switch op {
	case "start", "stop", "restart", "kill":
	default:
		return fmt.Errorf("unknown action %q", op)
	}
	return c.do(ctx, "POST", "/containers/"+id+"/"+op, nil, nil)
}

// Remove deletes a container.
func (c *Client) Remove(ctx context.Context, id string, force bool) error {
	path := "/containers/" + id
	if force {
		path += "?force=1"
	}
	return c.do(ctx, "DELETE", path, nil, nil)
}

// Rename renames a container.
func (c *Client) Rename(ctx context.Context, id, newName string) error {
	return c.do(ctx, "POST", "/containers/"+id+"/rename?name="+newName, nil, nil)
}

// Logs returns up to tail lines of a container log. cardinal ignores follow,
// so wings implements follow via polling (see FollowLogs).
func (c *Client) Logs(ctx context.Context, id string, tail string) ([]byte, error) {
	path := "/containers/" + id + "/logs"
	if tail != "" {
		path += "?tail=" + url.QueryEscape(tail)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("logs %s: %w", id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("logs %s: status %d", id, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// FollowLogs polls the container log file (cardinal's logs endpoint does not
// stream) and streams new lines over the returned channel until ctx is done.
func (c *Client) FollowLogs(ctx context.Context, id string) (<-chan string, <-chan error) {
	ch := make(chan string)
	errCh := make(chan error, 1)
	go func() {
		defer close(ch)
		var last int
		for {
			select {
			case <-ctx.Done():
				errCh <- nil
				return
			default:
			}
			data, err := c.Logs(ctx, id, "") // all lines
			if err != nil {
				errCh <- err
				return
			}
			lines := strings.Split(string(data), "\n")
			if len(lines) > last {
				for _, ln := range lines[last:] {
					if ln != "" {
						ch <- ln
					}
				}
				last = len(lines)
			}
			select {
			case <-ctx.Done():
				errCh <- nil
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	}()
	return ch, errCh
}

// ExecRequest describes an exec invocation.
type ExecRequest struct {
	Cmd          []string `json:"Cmd"`
	AttachStdin  bool     `json:"AttachStdin"`
	AttachStdout bool     `json:"AttachStdout"`
	AttachStderr bool     `json:"AttachStderr"`
	Tty          bool     `json:"Tty"`
}

// Exec runs a command in a container via cardinal's exec endpoint.
func (c *Client) Exec(ctx context.Context, id string, req *ExecRequest) (string, error) {
	var out struct {
		ID string `json:"Id"`
	}
	if err := c.do(ctx, "POST", "/containers/"+id+"/exec", req, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}
