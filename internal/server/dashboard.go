package server

import (
	"net/http"
	"sync"

	"github.com/animesao/cardinal-wings/internal/auth"
) // nodeSummary is the per-node block returned by /v1/system/info.
type nodeSummary struct {
	Name            string `json:"name"`
	Status          string `json:"status"`
	Running         int    `json:"running"`
	Stopped         int    `json:"stopped"`
	Total           int    `json:"total"`
	Images          int    `json:"images"`
	NCPU            int    `json:"ncpu,omitempty"`
	MemTotal        int64  `json:"mem_total_bytes,omitempty"`
	Architecture    string `json:"architecture,omitempty"`
	Hostname        string `json:"hostname,omitempty"`
	CardinalVersion string `json:"cardinal_version,omitempty"`
}

// handleSystemInfo returns a cluster-wide dashboard aggregate: one summary per
// node, computed concurrently. It answers "what am I looking at" in a single
// request so the panel's landing screen needs zero extra calls.
func handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	names := registry.names()
	summaries := make([]nodeSummary, len(names))

	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			summaries[i] = summarizeNode(r, name)
		}(i, name)
	}
	wg.Wait()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"nodes":   summaries,
		"running": totalRunning(summaries),
		"total":   totalAll(summaries),
	})
}

func summarizeNode(r *http.Request, name string) nodeSummary {
	c := registry.byName(name)
	if c == nil {
		return nodeSummary{Name: name, Status: "down"}
	}
	ns := nodeSummary{Name: name, Status: "up"}

	if status, err := c.Health(r.Context()); err != nil || status == "" {
		ns.Status = "down"
		return ns
	} else {
		ns.Status = status
	}

	// Containers
	if list, err := c.ListContainers(r.Context(), true); err == nil {
		ns.Total = len(list)
		for _, ct := range list {
			if isRunningStatus(ct.Status) {
				ns.Running++
			} else {
				ns.Stopped++
			}
		}
	}
	// Images
	if imgs, err := c.Images(r.Context()); err == nil {
		ns.Images = len(imgs)
	}
	// Host info (cpu/mem) for the node card.
	if info, err := c.Info(r.Context()); err == nil && info != nil {
		ns.NCPU = info.NCPU
		ns.MemTotal = info.MemTotal
		ns.Architecture = info.Architecture
		ns.Hostname = info.Name
	}
	// Cardinal version so the panel can warn on incompatibility.
	if v, err := c.CardinalVersion(r.Context()); err == nil {
		ns.CardinalVersion = v
	}

	return ns
}

func isRunningStatus(status string) bool {
	return status == "running" || status == "up" || status == "restarting"
}

func totalRunning(ss []nodeSummary) int {
	n := 0
	for _, s := range ss {
		n += s.Running
	}
	return n
}

func totalAll(ss []nodeSummary) int {
	n := 0
	for _, s := range ss {
		n += s.Total
	}
	return n
}

// handleSelf tells the panel which role the current API key has, so the UI can
// enable/disable admin actions without trying them and getting 403.
func handleSelf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	role, ok := auth.RoleFrom(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, ErrUnauthorized, "no role in context")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated": true,
		"role":          role,
		"admin":         role == "admin",
	})
}
