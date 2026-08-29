// Package server wires the HTTP daemon: routing, middleware chain, and the
// v1 API handlers. Container endpoints are live (Phase 1); image, blueprint,
// cluster and streaming handlers land in later phases.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/animesao/cardinal-wings/internal/agent"
	"github.com/animesao/cardinal-wings/internal/auth"
	"github.com/animesao/cardinal-wings/internal/config"
	"github.com/animesao/cardinal-wings/internal/runtime"
)

// version is a build-time variable overwritten by the release pipeline via
// `-ldflags "-X github.com/animesao/cardinal-wings/internal/server.version=…"`.
// Its default keeps local `go build` builds identifiable as dev builds.
var version = "dev"

// defaultClient points at the local cardinal node. Handlers read it, so the
// daemon must be started through Run (which assigns it).
var defaultClient *runtime.Client

// Run starts wings: it launches the local `cardinal serve` subprocess (if the
// binary is present), then serves the panel-facing API on the configured
// address. When cardinal is missing, wings still starts and reports the local
// node as down — the panel can show degraded status instead of a dead daemon.
func Run(cfg *config.Config) error {
	var local *agent.Local
	if l, err := agent.StartLocal(); err == nil {
		local = l
		defer local.Stop()
		defaultClient = local.Client()
	} else {
		logf("!! local cardinal unavailable: %v (nodes will show down)", err)
	}

	// Populate the node registry: local node first (nil client when cardinal
	// is missing), then every enabled remote node.
	entries := []nodeEntry{{name: "local", client: defaultClient, local: true}}
	for _, n := range agent.RemoteClients(cfg) {
		entries = append(entries, nodeEntry{name: n.Name, client: n.Client})
	}
	registerNodes(entries)

	// Background health checks for /v1/nodes.
	hcCtx, hcCancel := context.WithCancel(context.Background())
	defer hcCancel()
	checker.start(hcCtx)

	// Audit log of admin mutations.
	if dir := os.Getenv("WINGS_DATA_DIR"); dir != "" {
		initAudit(filepath.Join(dir, "wings-audit.jsonl"))
	} else {
		initAudit("wings-audit.jsonl")
	}

	// Webhook notifications (task completion etc).
	initWebhooks(cfg)

	// Per-container SFTP (SSH) server — the panel generates credentials and
	// users connect straight to the container data dir with Filezilla/WinSCP.
	if err := startSFTPServer(cfg); err != nil {
		logf("!! sftp server disabled: %v", err)
	}

	mw := auth.New(cfg)

	// Public (no auth): liveness and health checks for load balancers and
	// monitoring. Everything else lives behind the authenticated chain.
	public := http.NewServeMux()
	public.HandleFunc("/v1/ping", handlePing)
	public.HandleFunc("/healthz", handleHealthz)

	// Authenticated v1 API surface.
	api := http.NewServeMux()
	api.HandleFunc("/v1/version", handleVersion)
	api.HandleFunc("/v1/system/info", handleSystemInfo)
	api.HandleFunc("/v1/self", handleSelf)
	api.HandleFunc("/v1/metrics", handleMetrics)
	api.HandleFunc("/v1/events", handleEvents)
	api.HandleFunc("/v1/sftp/info", handleSftpInfo)
	tasksRoutes(api)
	containerRoutes(api, mw)
	imageRoutes(api, mw)
	blueprintRoutes(api, mw)
	servicesRoutes(api, mw)
	bootstrapRoutes(api, mw)
	clusterRoutes(api)

	// The authenticated chain: CORS -> per-key rate limit -> per-IP rate
	// limit -> bearer auth -> audit.
	var handler http.Handler = api
	handler = mw.Authenticate(handler)
	handler = auditMiddleware(handler)
	handler = mw.RateLimitKey(handler)
	handler = mw.RateLimit(handler)
	handler = mw.CORS(handler)

	// Everything else falls through to the authenticated API.
	public.Handle("/", handler)

	addr := net.JoinHostPort(cfg.Server.Host, fmt.Sprintf("%d", cfg.Server.Port))
	srv := &http.Server{
		Addr:              addr,
		Handler:           public,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       durSeconds(cfg.Server.ReadTimeout, 60),
		WriteTimeout:      durSeconds(cfg.Server.WriteTimeout, 60),
		IdleTimeout:       durSeconds(cfg.Server.IdleTimeout, 120),
		MaxHeaderBytes:    1 << 20,
	}

	logf("cardinal-wings listening on %s", addr)
	if len(cfg.Keys) == 0 {
		logf("!! no API keys configured — copy config.example.toml to %s and set [[keys]]", defaultConfigPath)
	}

	errCh := make(chan error, 1)
	go func() {
		if cfg.Server.TLSCert != "" && cfg.Server.TLSKey != "" {
			errCh <- srv.ListenAndServeTLS(cfg.Server.TLSCert, cfg.Server.TLSKey)
		} else {
			errCh <- srv.ListenAndServe()
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-stop:
		logf("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

// durSeconds converts a configured seconds value to a duration, falling back
// to the default when the config leaves it at zero.
func durSeconds(secs, def int) time.Duration {
	if secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return time.Duration(def) * time.Second
}

func logf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stdout, format+"\n", args...)
}

const defaultConfigPath = "/etc/cardinal-wings/config.toml"

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
