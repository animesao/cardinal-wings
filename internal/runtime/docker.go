package runtime

import (
	"fmt"
	"strings"
)

// --- Docker API subset (see cardinal/internal/api/types.go) ---

type portMapping struct {
	PrivatePort int    `json:"PrivatePort"`
	PublicPort  int    `json:"PublicPort"`
	Type        string `json:"Type"`
}

type mountInfo struct {
	Type        string `json:"Type"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	RW          bool   `json:"RW"`
}

type networkSettings struct {
	IPAddress string `json:"IPAddress"`
}

type dockerContainerSummary struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Created int64             `json:"Created"`
	Ports   []portMapping     `json:"Ports"`
	Labels  map[string]string `json:"Labels"`
	Mounts  []mountInfo       `json:"Mounts"`
	Network *networkSettings  `json:"NetworkSettings"`
}

type dockerRestartPolicy struct {
	Name string `json:"Name"`
}

type dockerPortBinding struct {
	HostIp   string `json:"HostIp,omitempty"`
	HostPort string `json:"HostPort,omitempty"`
}

type dockerHostConfig struct {
	Binds         []string                       `json:"Binds,omitempty"`
	PortBindings  map[string][]dockerPortBinding `json:"PortBindings,omitempty"`
	RestartPolicy *dockerRestartPolicy           `json:"RestartPolicy,omitempty"`
	Memory        int64                          `json:"Memory,omitempty"`
	NanoCPUs      int64                          `json:"NanoCpus,omitempty"`
}

type dockerContainerConfig struct {
	Env []string `json:"Env,omitempty"`
}

type dockerContainerState struct {
	Status string `json:"Status"`
}

type dockerContainerInspect struct {
	ID              string                 `json:"Id"`
	Name            string                 `json:"Name"`
	State           *dockerContainerState  `json:"State"`
	Image           string                 `json:"Image"`
	Config          *dockerContainerConfig `json:"Config"`
	HostConfig      *dockerHostConfig      `json:"HostConfig"`
	NetworkSettings *networkSettings       `json:"NetworkSettings"`
	Mounts          []mountInfo            `json:"Mounts,omitempty"`
}

func summarizeDocker(r dockerContainerSummary) Summary {
	name := ""
	if len(r.Names) > 0 {
		name = strings.TrimPrefix(r.Names[0], "/")
	}
	ports := make([]string, 0, len(r.Ports))
	for _, p := range r.Ports {
		proto := p.Type
		if proto == "" {
			proto = "tcp"
		}
		ports = append(ports, fmt.Sprintf("%d:%d/%s", p.PublicPort, p.PrivatePort, proto))
	}
	ip := ""
	if r.Network != nil {
		ip = r.Network.IPAddress
	}
	return Summary{
		ID:        r.ID,
		Name:      name,
		Image:     r.Image,
		Status:    r.State,
		IP:        ip,
		Ports:     ports,
		Labels:    r.Labels,
		CreatedAt: fmt.Sprint(r.Created),
	}
}

func dockerDetail(in *dockerContainerInspect) *Detail {
	name := strings.TrimPrefix(in.Name, "/")
	status := ""
	if in.State != nil {
		status = in.State.Status
	}
	ip := ""
	if in.NetworkSettings != nil {
		ip = in.NetworkSettings.IPAddress
	}

	var env []string
	var restart string
	var memory int64
	var cpus float64
	if in.Config != nil {
		env = in.Config.Env
	}
	hostPorts := map[string][]dockerPortBinding{}
	if in.HostConfig != nil {
		if in.HostConfig.RestartPolicy != nil {
			restart = in.HostConfig.RestartPolicy.Name
		}
		memory = in.HostConfig.Memory
		if in.HostConfig.NanoCPUs > 0 {
			cpus = float64(in.HostConfig.NanoCPUs) / 1_000_000_000
		}
		hostPorts = in.HostConfig.PortBindings
	}

	// Ports, parsed from HostConfig.PortBindings (key "containerPort/proto").
	ports := make([]PortRef, 0)
	for key, bindings := range hostPorts {
		proto := "tcp"
		containerKey := key
		if i := strings.Index(key, "/"); i >= 0 {
			proto = key[i+1:]
			containerKey = key[:i]
		}
		contPort := 0
		_, _ = fmt.Sscanf(containerKey, "%d", &contPort)
		hostPort := 0
		if len(bindings) > 0 && bindings[0].HostPort != "" {
			_, _ = fmt.Sscanf(bindings[0].HostPort, "%d", &hostPort)
		}
		ports = append(ports, PortRef{Host: hostPort, Container: contPort, Protocol: proto})
	}

	volumes := make([]VolumeRef, 0, len(in.Mounts))
	for _, m := range in.Mounts {
		volumes = append(volumes, VolumeRef{
			Type:     m.Type,
			Source:   m.Source,
			Target:   m.Destination,
			ReadOnly: !m.RW,
		})
	}

	return &Detail{
		ID:       in.ID,
		Name:     name,
		Image:    in.Image,
		Status:   status,
		IP:       ip,
		Ports:    ports,
		Volumes:  volumes,
		Env:      env,
		Restart:  restart,
		Memory:   memory,
		CPUs:     cpus,
		Platform: "linux",
	}
}
