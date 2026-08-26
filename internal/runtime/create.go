package runtime

import (
	"context"
	"fmt"
)

// PortBinding is a single host:container[/proto] mapping on create.
type PortBinding struct {
	Host      int    `json:"host"`
	Container int    `json:"container"`
	Protocol  string `json:"protocol,omitempty"`
}

// VolumeBinding is a mounted volume spec passed to cardinal.
type VolumeBinding struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Read   bool   `json:"read_only,omitempty"`
}

// CreateRequest is the panel-facing payload for creating a container.
type CreateRequest struct {
	Name    string            `json:"name"`
	Image   string            `json:"image"`
	Cmd     []string          `json:"cmd,omitempty"`
	Env     []string          `json:"env,omitempty"`
	Ports   []PortBinding     `json:"ports,omitempty"`
	Volumes []VolumeBinding   `json:"volumes,omitempty"`
	Restart string            `json:"restart,omitempty"`
	Memory  int64             `json:"memory_bytes,omitempty"`
	CPUs    float64           `json:"cpus,omitempty"`
	Disk    int64             `json:"disk_bytes,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
	DNS     []string          `json:"dns,omitempty"`
}

// CreateResult is returned on success.
type CreateResult struct {
	ID string `json:"id"`
}

// dockerCreateRequest maps to cardinal's CreateContainerRequest.
type dockerCreateRequest struct {
	Hostname   string                  `json:"Hostname"`
	Env        []string                `json:"Env,omitempty"`
	Cmd        []string                `json:"Cmd,omitempty"`
	Image      string                  `json:"Image"`
	Labels     map[string]string       `json:"Labels,omitempty"`
	HostConfig *dockerCreateHostConfig `json:"HostConfig,omitempty"`
}

type dockerCreateHostConfig struct {
	Binds         []string                       `json:"Binds,omitempty"`
	PortBindings  map[string][]dockerPortBinding `json:"PortBindings,omitempty"`
	RestartPolicy *dockerRestartPolicy           `json:"RestartPolicy,omitempty"`
	Memory        int64                          `json:"Memory,omitempty"`
	NanoCPUs      int64                          `json:"NanoCpus,omitempty"`
	DiskLimit     int64                          `json:"DiskLimit,omitempty"`
	DNS           []string                       `json:"Dns,omitempty"`
}

type dockerCreateResponse struct {
	ID string `json:"Id"`
}

// Create pulls the image and starts a container on the node.
func (c *Client) Create(ctx context.Context, req *CreateRequest) (*CreateResult, error) {
	if req == nil || req.Image == "" {
		return nil, fmt.Errorf("image is required")
	}
	restart := req.Restart
	if restart == "" {
		restart = "no"
	}

	binds := make([]string, 0, len(req.Volumes))
	for _, v := range req.Volumes {
		spec := v.Source + ":" + v.Target
		if v.Read {
			spec += ":ro"
		}
		binds = append(binds, spec)
	}

	portBindings := make(map[string][]dockerPortBinding, len(req.Ports))
	for _, p := range req.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		key := fmt.Sprintf("%d/%s", p.Container, proto)
		hp := ""
		if p.Host > 0 {
			hp = fmt.Sprintf("%d", p.Host)
		}
		portBindings[key] = []dockerPortBinding{{HostPort: hp}}
	}

	body := dockerCreateRequest{
		Hostname: req.Name,
		Env:      req.Env,
		Cmd:      req.Cmd,
		Image:    req.Image,
		Labels:   req.Labels,
		HostConfig: &dockerCreateHostConfig{
			Binds:         binds,
			PortBindings:  portBindings,
			RestartPolicy: &dockerRestartPolicy{Name: restart},
			Memory:        req.Memory,
			NanoCPUs:      int64(req.CPUs * 1_000_000_000),
			DiskLimit:     req.Disk,
			DNS:           req.DNS,
		},
	}

	var resp dockerCreateResponse
	if err := c.do(ctx, "POST", "/containers/create", &body, &resp); err != nil {
		return nil, err
	}
	// If no name was provided (DinD behavior), docker generates an Id; the
	// container is created stopped unless we start it. cardinal's create
	// auto-starts, so nothing further is needed.
	_ = resp
	return &CreateResult{ID: resp.ID}, nil
}
