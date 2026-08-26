# cardinal-wings

> **Status: in active development.** Nothing stable, nothing to install. This
> is the plan and an early skeleton.

cardinal-wings is a REST API daemon for managing
[cardinal](https://github.com/animesao/cardinal) over HTTP. It's the single
entry point a control-plane / web panel (`cardinal-panel`) talks to instead of
SSH + CLI — for one host or across the whole cardinal cluster.

See **[SPEC.md](SPEC.md)** for the full technical plan and
**[docs/api.md](docs/api.md)** for the API reference with curl examples.

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

Live v1 endpoints: `/v1/ping`, `/v1/version`, `/v1/self`, `/v1/system/info`
(dashboard aggregate), `/v1/metrics` (Prometheus, admin), `/v1/events` (SSE
container events), `/v1/containers` (list with `?state=`/`?image=`/`?search=`
filters + `?sort=`/`?limit=`/`?offset=`, create, inspect, lifecycle, stats,
logs SSE, exec, exec/stream SSE, interactive terminal via SSE+input),
`/v1/images` (list/inspect/remove/pull with live progress/tag/push/search),
`/v1/blueprints` (async install/uninstall), `/v1/tasks` (async jobs,
persisted), `/v1/services`, `/v1/functions`, `/v1/nodes` + `/v1/cluster/*`.

An **OpenAPI schema** lives in [docs/openapi.yaml](docs/openapi.yaml) — a
panel client can be generated from it.

Every resource endpoint accepts `?node=<name>` to route to a specific
cluster node (default: local). Errors are uniform:
`{"error":{"code":"…","message":"…"}}`.

## Build & run (development)

```bash
make build
./cardinal-wings --config config.example.toml
curl -s localhost:8080/v1/ping   # pong
```

## Security

- Bearer auth with per-key roles (`readonly`/`admin`) and **per-key + per-IP
  rate limiting** (configurable in `[rate_limit]`).
- Admin mutations are written to an **audit log** (`wings-audit.jsonl`, set
  `WINGS_DATA_DIR` to relocate; rotated at 10 MiB).
- TLS supported in the config; `WINGS_TLS=1` in install.sh generates a
  self-signed cert and wires it up. Server timeouts are configurable.
- Loopback-only by default; external binds require keys + TLS.
- **Webhooks** (`[[webhooks]]` in config) deliver `task.completed` and
  container events to panel URLs with an optional shared secret.

## Operations

- `GET /healthz` (unauthenticated): 200 when the local cardinal node is up,
  503 in degraded mode (cardinal missing). wings still starts without
  cardinal — nodes just show as down.
- Live terminal: `POST /v1/containers/{id}/terminal`, input via
  `/terminal/input`, output via `/terminal/stream` (SSE) or `/terminal/ws`
  (websocket).
- Live stats: `/v1/containers/{id}/stats?stream=1&interval=2s` (SSE).
- Filesystem: `/v1/containers/{id}/fs/ls|cat|tree?path=` and
  `POST /v1/containers/{id}/cp {src, dst}`.
- Multi-node metrics in `/v1/metrics` (per-node labels).
- Cluster deployment guide: [docs/cluster.md](docs/cluster.md).

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

## Contributing & security

See [CONTRIBUTING.md](CONTRIBUTING.md) for the contribution guide and
[SECURITY.md](SECURITY.md) for how to report a vulnerability.

## License

MIT — see [LICENSE](LICENSE).