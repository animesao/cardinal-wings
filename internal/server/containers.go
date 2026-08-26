package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

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
	case "start", "stop", "restart", "kill", "remove", "exec", "exec/stream", "terminal", "terminal/input":
		return true
	}
	return action == "" && method == http.MethodDelete
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
	res, err := c.Create(r.Context(), &req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, ErrBadRequest, "create: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, res)
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
		var s interface{}
		if err := c.Stats(r.Context(), ref, &s); err != nil {
			writeErr(w, http.StatusNotFound, ErrNotFound, "stats %s: %s", ref, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, s)

	case action == "logs" && r.Method == http.MethodGet:
		handleContainerLogs(w, r, ref, c)

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
		writeJSON(w, http.StatusOK, map[string]string{"removed": ref})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
