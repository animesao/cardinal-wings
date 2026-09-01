package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/animesao/cardinal-wings/internal/auth"
	"github.com/animesao/cardinal-wings/internal/runtime"
)

// containerRoutes mounts the container endpoints. Every handler routes to the
// node named by `?node=` (default: the local node).
func containerRoutes(mux *http.ServeMux, mw *auth.Middleware) {
	mux.HandleFunc("/v1/containers", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			c, ok := clientFor(w, r)
			if !ok {
				return
			}
			list, err := c.ListContainers(r.Context(), r.URL.Query().Get("all") == "1")
			if err != nil {
				writeErr(w, http.StatusBadGateway, ErrUpstream, "list containers: %s", err.Error())
				return
			}
			filtered := filterContainers(list, r.URL.Query())
			sortContainers(filtered, r.URL.Query())
			page := paginate(filtered, r.URL.Query())
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"containers": page.items,
				"total":      len(filtered),
				"limit":      page.limit,
				"offset":     page.offset,
			})
		case http.MethodPost:
			mw.AdminOnly(http.HandlerFunc(handleContainerCreate)).ServeHTTP(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
	mux.HandleFunc("/v1/containers/", func(w http.ResponseWriter, r *http.Request) {
		action, _ := splitRef(r.URL.Path)
		if isMutating(action, r.Method) {
			mw.AdminOnly(http.HandlerFunc(handleContainerRef)).ServeHTTP(w, r)
			return
		}
		handleContainerRef(w, r)
	})
}

// filterContainers applies panel-friendly filters: ?state=running|stopped,
// ?image=<substring>, ?search=<name-or-image substring>.
func filterContainers(list []runtime.Summary, q url.Values) []runtime.Summary {
	state := q.Get("state")
	image := q.Get("image")
	search := q.Get("search")
	if state == "" && image == "" && search == "" {
		return list
	}
	out := make([]runtime.Summary, 0, len(list))
	for _, s := range list {
		if state != "" {
			running := isRunningStatus(s.Status)
			if state == "running" && !running {
				continue
			}
			if state == "stopped" && running {
				continue
			}
		}
		if image != "" && !strings.Contains(strings.ToLower(s.Image), strings.ToLower(image)) {
			continue
		}
		if search != "" {
			hay := strings.ToLower(s.Name + " " + s.Image)
			if !strings.Contains(hay, strings.ToLower(search)) {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

// sortContainers orders a list by ?sort=name|status|created_at (default name)
// and ?order=asc|desc (default asc).
func sortContainers(list []runtime.Summary, q url.Values) {
	sortBy := q.Get("sort")
	desc := q.Get("order") == "desc"
	less := func(i, j int) bool {
		a, b := list[i], list[j]
		switch sortBy {
		case "status":
			return a.Status < b.Status
		case "created_at":
			return a.CreatedAt < b.CreatedAt
		default:
			return a.Name < b.Name
		}
	}
	if desc {
		sort.SliceStable(list, func(i, j int) bool { return less(j, i) })
	} else {
		sort.SliceStable(list, less)
	}
}

// pageResult carries a sliced page plus the requested window.
type pageResult struct {
	items  []runtime.Summary
	limit  int
	offset int
}

// paginate applies ?limit= and ?offset= to a list. Defaults: limit 100,
// offset 0; limit is capped at 1000 so a panel cannot fetch unbounded pages.
func paginate(list []runtime.Summary, q url.Values) pageResult {
	limit := 100
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
		limit = v
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := 0
	if v, err := strconv.Atoi(q.Get("offset")); err == nil && v > 0 {
		offset = v
	}
	if offset >= len(list) {
		return pageResult{items: []runtime.Summary{}, limit: limit, offset: offset}
	}
	end := offset + limit
	if end > len(list) {
		end = len(list)
	}
	return pageResult{items: list[offset:end], limit: limit, offset: offset}
}

// splitRef returns the container id and trailing action from a ref path.
func splitRef(path string) (ref, action string) {
	trimmed := strings.TrimPrefix(path, "/v1/containers/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 0 {
		return "", ""
	}
	ref = parts[0]
	if len(parts) == 2 {
		action = parts[1]
	}
	return ref, action
}

func isMutating(action, method string) bool {
	switch action {
	case "start", "stop", "restart", "kill", "remove", "exec", "exec/stream", "terminal", "terminal/input", "terminal/ws", "cp", "limits", "update":
		return true
	case "sftp":
		return method != http.MethodGet
	}
	// Backup restore (POST) uploads an archive into the container — mutating.
	// Backup download (GET) is a read, same as fm/download.
	if action == "backup" {
		return method == http.MethodPost
	}
	// File manager: reads (list/read/download) are non-mutating; the write ops
	// and every other fm action require admin.
	if strings.HasPrefix(action, "fm/") {
		return rwFmMutating(action)
	}
	return action == "" && method == http.MethodDelete
}

// rwFmMutating reports whether an fm action mutates the container filesystem.
func rwFmMutating(action string) bool {
	switch {
	case strings.HasPrefix(action, "fm/list"), strings.HasPrefix(action, "fm/read"), strings.HasPrefix(action, "fm/download"):
		return false
	default:
		return true
	}
}

func handleContainerCreate(w http.ResponseWriter, r *http.Request) {
	c, ok := clientFor(w, r)
	if !ok {
		return
	}
	var req runtime.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Panel containers get a persistent data volume: a named cardinal volume
	// keyed by container name. cardinal auto-creates named volumes inside its
	// own state directory and mounts them — unlike host binds they are not
	// subject to the protected-host-path check, they survive container
	// recreates (image change/reinstall keeps files), and backups/file-manager
	// only touch the data dir instead of the whole overlay. The panel sends
	// the volume without a source; Wings assigns the name.
	for i := range req.Volumes {
		if req.Volumes[i].Source == "" {
			req.Volumes[i].Source = "srv-" + req.Name
		}
	}

	res, err := c.Create(r.Context(), &req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, ErrBadRequest, "create: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

// handleContainerUpdate forwards a generic update payload (startup script, env,
// memory/CPU/disk) to cardinal's /containers/<id>/update. Startup and env are
// persisted by cardinal and applied on the next container start, so the panel's
// "Запуск" tab takes effect on restart without recreating the container.
func handleContainerUpdate(w http.ResponseWriter, r *http.Request, ref string, c *runtime.Client) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid update JSON: "+err.Error())
		return
	}
	if len(req) == 0 {
		writeError(w, http.StatusBadRequest, "no changes supplied")
		return
	}

	res, err := c.UpdateContainer(r.Context(), ref, req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, ErrInternal, "update %s: %s", ref, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleContainerLimits changes a container's memory/CPU/disk limits in place
// (no recreate, no data snapshot). Memory and CPU are applied to the container's
// cgroup live by cardinal; disk is persisted and applied on the next restart.
func handleContainerLimits(w http.ResponseWriter, r *http.Request, ref string, c *runtime.Client) {
	var req struct {
		MemoryBytes *int64   `json:"memory_bytes"`
		CPUs        *float64 `json:"cpus"`
		DiskBytes   *int64   `json:"disk_bytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid limits JSON: "+err.Error())
		return
	}

	payload := map[string]interface{}{}
	if req.MemoryBytes != nil {
		payload["memory_bytes"] = *req.MemoryBytes
	}
	if req.CPUs != nil {
		payload["cpus"] = *req.CPUs
	}
	if req.DiskBytes != nil {
		payload["disk_bytes"] = *req.DiskBytes
	}
	if len(payload) == 0 {
		writeError(w, http.StatusBadRequest, "no limits supplied")
		return
	}

	res, err := c.UpdateContainer(r.Context(), ref, payload)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, ErrInternal, "update limits %s: %s", ref, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type cpuSample struct {
	usage     float64
	timestamp float64
}

var cpuSamples = struct {
	sync.Mutex
	values map[string]cpuSample
}{values: make(map[string]cpuSample)}

// addCPUPercent calculates the native cardinal CPU counter delta once per
// container. cardinal reports usage in microseconds and timestamp in ns.
func addCPUPercent(ref string, stats map[string]interface{}) {
	usage, usageOK := stats["cpu_usage_usec"].(float64)
	timestamp, timestampOK := stats["timestamp"].(float64)
	if !usageOK || !timestampOK || usage < 0 || timestamp <= 0 {
		return
	}

	cpuSamples.Lock()
	defer cpuSamples.Unlock()
	previous, ok := cpuSamples.values[ref]
	cpuSamples.values[ref] = cpuSample{usage: usage, timestamp: timestamp}
	if !ok || timestamp <= previous.timestamp || usage < previous.usage {
		stats["cpu_percent"] = float64(0)
		return
	}

	percent := (usage - previous.usage) * 100000 / (timestamp - previous.timestamp)
	if percent < 0 {
		percent = 0
	}
	if percent > 9999 {
		percent = 9999
	}
	stats["cpu_percent"] = percent
}

func handleContainerRef(w http.ResponseWriter, r *http.Request) {
	c, ok := clientFor(w, r)
	if !ok {
		return
	}
	ref, action := splitRef(r.URL.Path)
	if ref == "" {
		writeError(w, http.StatusNotFound, "missing container id")
		return
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		d, err := c.Inspect(r.Context(), ref)
		if err != nil {
			writeErr(w, http.StatusNotFound, ErrNotFound, "inspect %s: %s", ref, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, d)

	case action == "stats" && r.Method == http.MethodGet:
		if r.URL.Query().Get("stream") == "1" {
			handleContainerStatsStream(w, r, ref, c)
			return
		}
		var s map[string]interface{}
		if err := c.Stats(r.Context(), ref, &s); err != nil {
			writeErr(w, http.StatusNotFound, ErrNotFound, "stats %s: %s", ref, err.Error())
			return
		}
		addCPUPercent(ref, s)
		writeJSON(w, http.StatusOK, s)

	case action == "logs" && r.Method == http.MethodGet:
		handleContainerLogs(w, r, ref, c)

	case action == "backup" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		handleContainerBackup(w, r, ref)

	case action == "limits" && r.Method == http.MethodPost:
		handleContainerLimits(w, r, ref, c)

	case action == "update" && r.Method == http.MethodPost:
		handleContainerUpdate(w, r, ref, c)

	case action == "exec" && r.Method == http.MethodPost:
		handleContainerExec(w, r, ref, c)

	case action == "exec/stream" && r.Method == http.MethodPost:
		handleContainerExecStream(w, r, ref)

	case action == "terminal" && r.Method == http.MethodPost:
		handleTerminalOpen(w, r)

	case action == "terminal/input" && r.Method == http.MethodPost:
		handleTerminalInput(w, r)

	case action == "terminal/stream" && r.Method == http.MethodGet:
		handleTerminalStream(w, r)

	case action == "terminal/ws" && r.Method == http.MethodGet:
		handleTerminalWS(w, r)

	case strings.HasPrefix(action, "fs/") && r.Method == http.MethodGet:
		handleFs(w, r)

	case action == "sftp" && r.Method == http.MethodGet:
		handleSftpStatus(w, r, ref)

	case action == "sftp" && r.Method == http.MethodPut:
		handleSftpSet(w, r, ref)

	case action == "sftp" && r.Method == http.MethodDelete:
		handleSftpDelete(w, r, ref)

	case strings.HasPrefix(action, "fm/"):
		handleFm(w, r, ref)

	case action == "cp" && r.Method == http.MethodPost:
		handleCp(w, r)

	case (action == "start" || action == "stop" || action == "restart" || action == "kill") && r.Method == http.MethodPost:
		if err := c.Action(r.Context(), ref, action); err != nil {
			writeErr(w, http.StatusInternalServerError, ErrInternal, "%s %s: %s", action, ref, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": ref, "action": action, "ok": "true"})

	case action == "" && r.Method == http.MethodDelete:
		force := r.URL.Query().Get("force") == "1"
		if err := c.Remove(r.Context(), ref, force); err != nil {
			writeErr(w, http.StatusInternalServerError, ErrInternal, "remove %s: %s", ref, err.Error())
			return
		}
		// Убираем SFTP-креду удалённого контейнера, чтобы не копились
		// протухшие записи (container id пересоздаётся заново).
		if sftpStoreInst != nil {
			_ = sftpStoreInst.remove(ref)
		}
		writeJSON(w, http.StatusOK, map[string]string{"removed": ref})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
