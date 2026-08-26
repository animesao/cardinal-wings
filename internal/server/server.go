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

// Run starts wings: it launches the local `cardinal serve` subprocess, then
// serves the panel-facing API on the configured address.
func Run(cfg *config.Config) error {
	local, err := agent.StartLocal()
	if err != nil {
		return fmt.Errorf("local cardinal: %w (is cardinal installed?)", err)
	}
	defer local.Stop()
	defaultClient = local.Client()

	// Populate the node registry: local node first, then every enabled remote
	// node, so handlers can route with `?node=<name>`.
	entries := []nodeEntry{{name: "local", client: local.Client(), local: true}}
	for _, n := range agent.RemoteClients(cfg) {
		entries = append(entries, nodeEntry{name: n.Name, client: n.Client})
	}
	registerNodes(entries)

	// Background health checks for /v1/nodes.
	hcCtx, hcCancel := context.WithCancel(context.Background())
	defer hcCancel()
	checker.start(hcCtx)

	mw := auth.New(cfg)
	mux := http.NewServeMux()

	// --- v1 system endpoints (always available) ---
	mux.HandleFunc("/v1/ping", handlePing)
	mux.HandleFunc("/v1/version", handleVersion)
	mux.HandleFunc("/v1/system/info", handleSystemInfo)
	mux.HandleFunc("/v1/self", handleSelf)
	mux.HandleFunc("/v1/metrics", handleMetrics)
	tasksRoutes(mux)

	// --- resource endpoints (behind auth) ---
	containerRoutes(mux, mw)
	imageRoutes(mux, mw)
	blueprintRoutes(mux, mw)
	servicesRoutes(mux, mw)
	clusterRoutes(mux)

	// The authenticated chain: rate limit -> CORS -> bearer auth.
	var handler http.Handler = mux
	handler = mw.Authenticate(handler)
	handler = mw.RateLimit(handler)
	handler = mw.CORS(handler)

	addr := net.JoinHostPort(cfg.Server.Host, fmt.Sprintf("%d", cfg.Server.Port))
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
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

func logf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stdout, format+"\n", args...)
}

const defaultConfigPath = "/etc/cardinal-wings/config.toml"

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
