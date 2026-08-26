package server

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/animesao/cardinal-wings/internal/auth"
)

// handleMetrics renders Prometheus text-format metrics aggregated from the
// local cardinal node plus the node registry. No external deps: wings renders
// the exposition format directly.
func handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// Metrics are sensitive (they reveal node layout); admin only.
	if role, ok := auth.RoleFrom(r.Context()); !ok || role != "admin" {
		writeErr(w, http.StatusForbidden, ErrForbidden, "admin role required")
		return
	}

	var b strings.Builder

	// Node liveness.
	snapshot := checker.snapshot()
	names := registry.names()
	for _, name := range names {
		st, ok := snapshot[name]
		up := 0
		if ok && st.status == "up" {
			up = 1
		}
		fmt.Fprintf(&b, "cardinal_wings_node_up{node=%q} %d\n", name, up)
	}
	fmt.Fprintf(&b, "# TYPE cardinal_wings_node_up gauge\n")
	fmt.Fprintf(&b, "# HELP cardinal_wings_node_up Whether a node is reachable (1) or down (0).\n\n")

	// Container counts by status on the local node.
	c := registry.local()
	if c != nil {
		list, err := c.ListContainers(r.Context(), true)
		if err == nil {
			counts := map[string]int{}
			for _, ct := range list {
				status := ct.Status
				if status == "" {
					status = "unknown"
				}
				counts[status]++
			}
			statuses := make([]string, 0, len(counts))
			for s := range counts {
				statuses = append(statuses, s)
			}
			sort.Strings(statuses)
			for _, s := range statuses {
				fmt.Fprintf(&b, "cardinal_wings_containers{state=%q} %d\n", s, counts[s])
			}
			fmt.Fprintf(&b, "cardinal_wings_containers_total %d\n", len(list))
			fmt.Fprintf(&b, "# TYPE cardinal_wings_containers gauge\n")
			fmt.Fprintf(&b, "# TYPE cardinal_wings_containers_total gauge\n\n")
		}
	}

	// Image count.
	if c != nil {
		if imgs, err := c.Images(r.Context()); err == nil {
			fmt.Fprintf(&b, "cardinal_wings_images_total %d\n", len(imgs))
			fmt.Fprintf(&b, "# TYPE cardinal_wings_images_total gauge\n\n")
		}
	}

	// Task stats.
	tasks := taskMgr.List()
	failed := 0
	running := 0
	succeeded := 0
	for _, t := range tasks {
		switch t.Status {
		case "failed":
			failed++
		case "running", "queued":
			running++
		case "succeeded":
			succeeded++
		}
	}
	fmt.Fprintf(&b, "cardinal_wings_tasks_failed_total %d\n", failed)
	fmt.Fprintf(&b, "cardinal_wings_tasks_running %d\n", running)
	fmt.Fprintf(&b, "cardinal_wings_tasks_succeeded_total %d\n", succeeded)
	fmt.Fprintf(&b, "# TYPE cardinal_wings_tasks_failed_total counter\n")
	fmt.Fprintf(&b, "# TYPE cardinal_wings_tasks_running gauge\n")
	fmt.Fprintf(&b, "# TYPE cardinal_wings_tasks_succeeded_total counter\n")

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}
