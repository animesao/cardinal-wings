# cardinal-wings

> **Status: in active development.** Nothing stable, nothing to install. This
> is the plan and an early skeleton.

cardinal-wings is a REST API daemon for managing
[cardinal](https://github.com/animesao/cardinal) over HTTP. It's the single
entry point a control-plane / web panel (`cardinal-panel`) talks to instead of
SSH + CLI — for one host or across the whole cardinal cluster.

See **[SPEC.md](SPEC.md)** for the full technical plan.

## Why

- **Own REST**, not locked to the Docker API contract (`cardinal serve`
  already covers Docker-compatible tooling).
- **Multiple API keys + roles** (read-only vs admin) — `serve` has one global
  token.
- **Live streaming** — logs `follow` and interactive `exec`, only partially
  available in `serve`.
- **One facade over the cluster** — services, functions and nodes already
  exist in `cardinal/internal/orchestrator`; wings exposes and aggregates them.

## Architecture in one line

wings talks to cardinal over its **Docker-compatible HTTP API** (`cardinal
serve`). It spawns a local `cardinal serve` on loopback (ephemeral port +
random token) to manage this node, and reaches **other cluster nodes over
HTTP** through their `cardinal serve` API. wings adds the auth layer, the
REST schema, cross-node routing and streaming — it re-implements nothing.

> Why HTTP instead of importing `internal/*`? wings is a separate Go module
> (separate repo). Go does not allow importing another module's `internal/*`
> packages, so HTTPS-to-`cardinal serve` is the legal, clean way to reuse
> cardinal's handlers on every node — local and remote alike.

## Layout

```
cardinal-wings/
├── go.mod                        # module github.com/animesao/cardinal-wings (no replace)
├── main.go                       # flag parsing + boot
├── config.example.toml           # documented keys / roles / nodes
├── internal/
│   ├── config/                   # dependency-free TOML subset parser + Validate/Authorize
│   ├── auth/                     # constant-time bearer check, roles, rate limit, CORS
│   ├── agent/                    # spawn/stop local cardinal serve; remote node clients
│   ├── runtime/                  # HTTP client + Docker API schema + blueprints/cluster
│   └── server/                   # http router, middleware chain, v1 handlers
├── systemd/cardinal-wings.service
├── install.sh
└── Makefile
```

Live v1 endpoints so far: `/v1/ping`, `/v1/version`, `/v1/containers` (list/
create/inspect/start/stop/restart/kill/remove/stats/logs/exec), `/v1/images`
(list/inspect/remove/pull/search/tag/push), `/v1/blueprints` (list/inspect/
install/uninstall) and `/v1/nodes` + `/v1/cluster/*`.

## Build & run (development)

```bash
make build
./cardinal-wings --config config.example.toml
curl -s localhost:8080/v1/ping   # pong
```

## Releases & CI (GitHub Actions)

The [build workflow](.github/workflows/build.yml) lints, tests and builds
linux/amd64 + linux/arm64 binaries on every push/PR, and **publishes a GitHub
Release** when a `v*` tag is pushed:

```bash
git tag v0.1.0 && git push origin v0.1.0
```

The tag name becomes the release version and is stamped into the binary via
`-ldflags -X internal/server.version=<tag>`, so `/v1/version` reports the exact
tagged build. CI attaches per-platform binaries + checksums + the installer.

Install the latest release:

```bash
curl -fsSL https://github.com/animesao/cardinal-wings/releases/latest/download/install.sh | bash
```

Or a specific version: `./install.sh v0.1.0` (from a clone), or build locally
with `./install.sh local`.

## Roadmap

See [SPEC.md](SPEC.md) §Phases. Containers → images → blueprints → streaming →
cluster facade → panel + site.