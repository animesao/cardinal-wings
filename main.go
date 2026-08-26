// cardinal-wings — a REST API daemon for managing cardinal over HTTP.
//
// Design intent (see SPEC.md): wings is the panel-facing management API. It
// talks to cardinal over its Docker-compatible HTTP API (`cardinal serve`) —
// spawning a local instance on loopback for this node and reaching other
// cluster nodes over HTTP — and adds multiple API keys with roles, live
// streaming, and a unified facade over single-host and cluster management.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/animesao/cardinal-wings/internal/config"
	"github.com/animesao/cardinal-wings/internal/server"
)

func main() {
	fs := flag.NewFlagSet("cardinal-wings", flag.ExitOnError)
	configPath := fs.String("config", "/etc/cardinal-wings/config.toml", "path to config.toml")
	port := fs.Int("port", 0, "override listen port (default from config)")
	host := fs.String("host", "", "override listen host (default from config)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cardinal-wings: %v\n", err)
		os.Exit(1)
	}
	if *port != 0 {
		cfg.Server.Port = *port
	}
	if *host != "" {
		cfg.Server.Host = *host
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "cardinal-wings: invalid config: %v\n", err)
		os.Exit(1)
	}

	if err := server.Run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "cardinal-wings: %v\n", err)
		os.Exit(1)
	}
}
