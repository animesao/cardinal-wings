package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/animesao/cardinal-wings/internal/config"
	"github.com/animesao/cardinal-wings/internal/ws"
)

// panelbrowser.go speaks the legacy Pterodactyl/Reviactyl browser protocols
// so the panel's React client works unchanged against cardinal-wings:
//
//   - GET  /download/file?token=<JWT scope=file-download>
//     streams one file's raw bytes (browser download).
//   - POST /upload/file?token=<JWT scope=file-upload>[&directory=]
//     accepts multipart files into the container data dir.
//   - GET  /api/servers/{uuid}/ws
//     the old JSON event websocket (auth/status/console output/stats,
//     send command, set state). Backed by a console attach session, so the
//     browser talks TO the game process exactly like before.
//
// JWTs are verified by verifyPanelJWT (paneljwt.go): the panel signs them
// with the node daemon token, which is a configured wings API key.

// panelBrowserRoutes mounts the no-Bearer public routes. They carry their
// own JWT auth, so they live on the public mux next to /v1/ping.
//
// Маршруты оборачиваются в auditMiddleware: POST /upload/file пишет файлы
// в контейнер, и без аудита такая запись не попадала в wings-audit.jsonl.
func panelBrowserRoutes(mux *http.ServeMux, cfg *config.Config) {
	mux.Handle("/upload/file", auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handleBrowserUpload(w, r, cfg)
	})))
	mux.HandleFunc("/download/file", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handleBrowserDownload(w, r, cfg)
	})
	mux.HandleFunc("/download/backup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		claims, _, err := verifyPanelJWT(r.URL.Query().Get("token"), cfg, true)
		if err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		if !claims.hasScope("backup-download") {
			writeError(w, http.StatusForbidden, "jwt scope mismatch")
			return
		}
		if claims.ServerUUID == "" || claims.BackupUUID == "" {
			writeError(w, http.StatusBadRequest, "token is missing server/backup claims")
			return
		}
		handleBackupJWTDownload(w, r, claims)
	})
	mux.HandleFunc("/api/servers/", func(w http.ResponseWriter, r *http.Request) {
		// Only the websocket path is served here; everything else 404s so a
		// stray old-daemon call can never hit the authenticated chain.
		trimmed := strings.TrimPrefix(r.URL.Path, "/api/servers/")
		if !strings.HasSuffix(trimmed, "/ws") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		handleLegacyWS(w, r, cfg)
	})
}

// ─── File download ─────────────────────────────────────────

// handleBrowserDownload verifies the file-download JWT and streams the file.
func handleBrowserDownload(w http.ResponseWriter, r *http.Request, cfg *config.Config) {
	claims, _, err := verifyPanelJWT(r.URL.Query().Get("token"), cfg, true)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if !claims.hasScope("file-download") {
		writeError(w, http.StatusForbidden, "jwt scope mismatch")
		return
	}
	if claims.ServerUUID == "" || claims.FilePath == "" {
		writeError(w, http.StatusBadRequest, "token is missing server/file claims")
		return
	}

	session, err := beginFMSession(claims.ServerUUID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "download: %s", err.Error())
		return
	}
	defer session.Close()

	hostPath, err := session.resolve(claims.FilePath, false)
	if err != nil {
		writeErr(w, http.StatusNotFound, ErrNotFound, "file not found: %s", claims.FilePath)
		return
	}
	info, err := os.Stat(hostPath)
	if err != nil || !info.Mode().IsRegular() {
		writeErr(w, http.StatusNotFound, ErrNotFound, "file not found: %s", claims.FilePath)
		return
	}
	f, err := os.Open(hostPath)
	if err != nil {
		writeErr(w, http.StatusNotFound, ErrNotFound, "read %s: %s", claims.FilePath, err.Error())
		return
	}
	defer f.Close()

	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	ctype := http.DetectContentType(head[:n])
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		writeErr(w, http.StatusInternalServerError, ErrInternal, "seek: %s", err.Error())
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(claims.FilePath)))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

// ─── File upload ───────────────────────────────────────────

// handleBrowserUpload verifies the file-upload JWT and stores multipart
// files into ?directory= (default /).
func handleBrowserUpload(w http.ResponseWriter, r *http.Request, cfg *config.Config) {
	claims, keyRole, err := verifyPanelJWT(r.URL.Query().Get("token"), cfg, true)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	// Uploads mutate the container: a readonly-signed token must never
	// drive writes (same watch-vs-drive rule as the legacy websocket,
	// which downgrades non-admin keys to watch-only).
	if keyRole != config.RoleAdmin {
		writeError(w, http.StatusForbidden, "jwt role mismatch")
		return
	}
	if !claims.hasScope("file-upload") {
		writeError(w, http.StatusForbidden, "jwt scope mismatch")
		return
	}
	if claims.ServerUUID == "" {
		writeError(w, http.StatusBadRequest, "token is missing server claim")
		return
	}

	// Лимиты загрузки. Раньше тело запроса читалось целиком
	// (ParseMultipartForm), а ограничение было ПОФАЙЛОВЫМ (100 МиБ) и без
	// ограничения числа файлов: один запрос с тысячей частей по 100 миБ
	// забивал диск ноды (~100 ГБ) и убивал все контейнеры на ней.
	// Маршрут публичный (собственная JWT-аутентификация), поэтому лимиты
	// обязаны быть тут.
	const (
		maxUploadFiles     = 32
		maxUploadTotal     = 512 << 20
		maxUploadPerFile   = 100 << 20
		maxUploadOverhead  = 1 << 20
	)
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadTotal+maxUploadOverhead)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart body: "+err.Error())
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, "no files uploaded")
		return
	}
	if len(files) > maxUploadFiles {
		writeError(w, http.StatusRequestEntityTooLarge, "too many files")
		return
	}
	dir := r.URL.Query().Get("directory")
	if dir == "" {
		dir = "/"
	}
	// Каталог назначения должен быть ПОДПИСАН в JWT: иначе держатель
	// токена на загрузку мог писать в любой каталог своего контейнера
	// (включая /cardinal, где лежит бинарник-хелпер exec'а), хотя панель
	// выдавала токен на конкретную папку.
	if claims.Directory != "" && claims.Directory != dir {
		writeError(w, http.StatusForbidden, "token directory mismatch")
		return
	}
	if claims.Directory == "" && dir != "/" {
		writeError(w, http.StatusForbidden, "token has no directory claim")
		return
	}

	session, err := beginFMSession(claims.ServerUUID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, ErrUpstream, "upload: %s", err.Error())
		return
	}
	defer session.Close()

	var total int64
	for _, fh := range files {
		name := filepath.Base(fh.Filename)
		if name == "" || name == "." || name == "/" {
			continue
		}
		dest, err := session.resolve(filepath.Join(dir, name), true)
		if err != nil {
			writeErr(w, http.StatusBadRequest, ErrBadRequest, "invalid path: %s", dir)
			return
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			writeErr(w, http.StatusBadGateway, ErrUpstream, "create parent: %s", err.Error())
			return
		}
		src, err := fh.Open()
		if err != nil {
			writeErr(w, http.StatusBadGateway, ErrUpstream, "open upload: %s", err.Error())
			return
		}
		dst, err := os.Create(dest)
		if err != nil {
			_ = src.Close()
			writeErr(w, http.StatusBadGateway, ErrUpstream, "write %s: %s", name, err.Error())
			return
		}
		// Копируем с бюджетом: не более maxUploadTotal всего и не более
		// maxUploadPerFile на файл.
		remain := maxUploadTotal - total
		if remain < 0 {
			remain = 0
		}
		limit := int64(maxUploadPerFile)
		if remain < limit {
			limit = remain
		}
		n, err := io.Copy(dst, io.LimitReader(src, limit+1))
		_ = src.Close()
		_ = dst.Close()
		if err != nil {
			writeErr(w, http.StatusBadGateway, ErrUpstream, "write %s: %s", name, err.Error())
			return
		}
		if n > limit {
			writeErr(w, http.StatusRequestEntityTooLarge, ErrBadRequest, "upload too large")
			return
		}
		total += n
		if total > maxUploadTotal {
			writeErr(w, http.StatusRequestEntityTooLarge, ErrBadRequest, "upload too large")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ─── Legacy console websocket ─────────────────────────────

// wsMsg is one legacy protocol frame.
type wsMsg struct {
	Event string   `json:"event"`
	Args  []string `json:"args,omitempty"`
}

func wsSend(conn *ws.Conn, event string, args ...string) {
	msg, _ := json.Marshal(wsMsg{Event: event, Args: args})
	_ = conn.WriteText(msg)
}

// handleLegacyWS implements the old daemon socket protocol on
// /api/servers/{uuid}/ws so the panel React console works unchanged.
func handleLegacyWS(w http.ResponseWriter, r *http.Request, cfg *config.Config) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/servers/")
	uuid := strings.TrimSuffix(trimmed, "/ws")
	if uuid == "" || strings.Contains(uuid, "/") {
		writeError(w, http.StatusNotFound, "missing server id")
		return
	}
	if defaultClient == nil {
		writeErr(w, http.StatusServiceUnavailable, ErrUpstream, "cardinal node unavailable")
		return
	}

	conn, err := ws.Upgrade(w, r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, ErrBadRequest, "websocket upgrade: %s", err.Error())
		return
	}
	defer conn.Close()

	// First frame must be auth.
	raw, err := conn.ReadText()
	if err != nil {
		return
	}
	var first wsMsg
	if err := json.Unmarshal(raw, &first); err != nil || first.Event != "auth" || len(first.Args) == 0 {
		wsSend(conn, "jwt error", "authentication required")
		_ = conn.Close()
		return
	}
	claims, keyRole, err := verifyPanelJWT(first.Args[0], cfg, false)
	if err != nil {
		wsSend(conn, "jwt error", err.Error())
		_ = conn.Close()
		return
	}
	// A token signed by a non-admin API key can only watch: strip every
	// write permission so a stolen readonly key can't mint itself
	// control.* powers over the console.
	if keyRole != "" && keyRole != config.RoleAdmin {
		kept := claims.Permissions[:0]
		for _, p := range claims.Permissions {
			if p == "websocket.connect" {
				kept = append(kept, p)
			}
		}
		claims.Permissions = kept
	}
	if !claims.hasScope("websocket") {
		wsSend(conn, "jwt error", "jwt scope mismatch")
		_ = conn.Close()
		return
	}
	// Привязка к серверу обязательна: пустой server_uuid больше НЕ считается
	// «подойдёт любой сервер». Раньше проверка выглядела
	// `if claims.ServerUUID != "" && claims.ServerUUID != uuid`, то есть
	// токен без server_uuid открывал консоль ЛЮБОГО контейнера, названного
	// в URL. Теперь требуем точное совпадение.
	if claims.ServerUUID == "" || claims.ServerUUID != uuid {
		wsSend(conn, "jwt error", "token server mismatch")
		_ = conn.Close()
		return
	}

	wsSend(conn, "auth success")

	// Token refresh timers mirroring the old daemon.
	if left := claims.secondsLeft(); left > 0 {
		go func() {
			if left > 180 {
				select {
				case <-time.After(time.Duration(left-180) * time.Second):
					wsSend(conn, "token expiring")
				case <-r.Context().Done():
					return
				}
			}
			select {
			case <-time.After(time.Duration(min64(left, 180)) * time.Second):
				wsSend(conn, "token expired")
				_ = conn.Close()
			case <-r.Context().Done():
			}
		}()
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	sess, err := terminals.open(ctx, uuid)
	if err != nil {
		wsSend(conn, "daemon error", "console unavailable: "+err.Error())
		_ = conn.Close()
		return
	}
	defer sess.close()
	defer terminals.removeIf(uuid, sess)

	// Empty permission lists deny writes: only explicitly granted
	// control.* (or the admin wildcard) may act. Legacy tokens without a
	// permissions claim can still watch the console, never drive it.
	//
	// Сравнение ТОЧНОЕ. Раньше здесь стояло
	// `p == perm || p == "admin.websocket.install" || p == "admin.websocket.transfer"`,
	// то есть право «установить блюпринт» или «перенести сервер» давало
	// также control.console / control.start / control.stop / control.restart
	// и право вводить команды в консоль. Панель выдаёт
	// admin.websocket.install пользователю для фичи «Установить» — и он
	// получал полное управление сервером.
	allow := func(perm string) bool {
		for _, p := range claims.Permissions {
			if p == perm || p == "admin" {
				return true
			}
		}
		return false
	}

	// Session output + status/stats pumps -> websocket.
	go func() {
		ch, unsubscribe := sess.subscribe()
		defer unsubscribe()
		statsT := time.NewTicker(5 * time.Second)
		defer statsT.Stop()
		pushStatusStats := func() {
			if st, err := machineryState(ctx, uuid); err == nil {
				wsSend(conn, "status", st)
			}
			if payload, err := containerStatsPayload(ctx, uuid); err == nil {
				wsSend(conn, "stats", payload)
			}
		}
		pushStatusStats()
		for {
			select {
			case <-ctx.Done():
				return
			case line, ok := <-ch:
				if !ok {
					return
				}
				switch {
				case line == "__session_ended__":
					wsSend(conn, "status", "offline")
					return
				case strings.HasPrefix(line, "__session_error__:"):
					wsSend(conn, "daemon error", strings.TrimPrefix(line, "__session_error__:"))
				default:
					wsSend(conn, "console output", line)
				}
			case <-statsT.C:
				pushStatusStats()
			}
		}
	}()

	// Browser input -> session / power.
	for {
		msg, err := conn.ReadText()
		if err != nil {
			return
		}
		var in wsMsg
		if err := json.Unmarshal(msg, &in); err != nil {
			continue
		}
		switch in.Event {
		case "auth":
			wsSend(conn, "auth success")
		case "send command":
			if !allow("control.console") {
				wsSend(conn, "daemon error", "missing control.console permission")
				continue
			}
			if len(in.Args) > 0 {
				_ = sess.writeInput(in.Args[0] + "\n")
			}
		case "set state":
			if len(in.Args) == 0 {
				continue
			}
			action := in.Args[0]
			perm := map[string]string{
				"start": "control.start", "stop": "control.stop",
				"restart": "control.restart", "kill": "control.stop",
			}[action]
			if perm == "" || !allow(perm) {
				wsSend(conn, "daemon error", "missing "+perm+" permission")
				continue
			}
			if err := defaultClient.Action(ctx, uuid, action); err != nil {
				wsSend(conn, "daemon error", action+" failed: "+err.Error())
			}
		case "send logs":
			if data, err := defaultClient.Logs(ctx, uuid, "100"); err == nil {
				for _, ln := range strings.Split(string(data), "\n") {
					if ln != "" {
						wsSend(conn, "console output", ln)
					}
				}
			}
		case "send stats":
			if payload, err := containerStatsPayload(ctx, uuid); err == nil {
				wsSend(conn, "stats", payload)
			}
		}
	}
}

// machineryState maps cardinal status to the legacy console states.
func machineryState(ctx context.Context, id string) (string, error) {
	d, err := defaultClient.Inspect(ctx, id)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(d.Status) {
	case "running", "up", "restarting":
		return "running", nil
	default:
		return "offline", nil
	}
}

// containerStatsPayload builds the legacy stats JSON string from wings stats.
func containerStatsPayload(ctx context.Context, id string) (string, error) {
	var s map[string]interface{}
	if err := defaultClient.Stats(ctx, id, &s); err != nil {
		return "", err
	}
	num := func(keys ...string) float64 {
		for _, k := range keys {
			if v, ok := s[k].(float64); ok {
				return v
			}
		}
		return 0
	}
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := s[k].(string); ok {
				return v
			}
		}
		return ""
	}
	out := map[string]interface{}{
		"memory_bytes":       num("memory_bytes", "memory_usage"),
		"memory_limit_bytes": num("memory_limit_bytes", "memory_limit"),
		"cpu_absolute":       num("cpu_percent", "cpu_absolute"),
		"disk_bytes":         num("disk_bytes"),
		"network": map[string]interface{}{
			"rx_bytes": num("rx_bytes", "network_rx_bytes"),
			"tx_bytes": num("tx_bytes", "network_tx_bytes"),
		},
		"uptime": num("uptime"),
		"state":  str("status", "state"),
	}
	if out["state"] == "" {
		if st, err := machineryState(ctx, id); err == nil {
			out["state"] = st
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
