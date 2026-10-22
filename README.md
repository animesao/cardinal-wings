# cardinal-wings

> **Status: v1 live.** REST API daemon used by the KT panel in production.

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
(dashboard aggregate), `/v1/system/df`, `/v1/system/prune` (admin),
`/v1/info`, `/v1/metrics` (Prometheus, admin), `/v1/events` (SSE
container events), `/v1/containers` (list with `?state=`/`?image=`/`?search=`
filters + `?sort=`/`?limit=`/`?offset=`, create, inspect, lifecycle,
rename, top, wait, changes, export tar, commit, dynamic ports,
stats, logs SSE, exec, exec/stream SSE, interactive terminal via
SSE+input/ws, fs, fm, cp, backup, sftp),
`/v1/images` (list/inspect/remove/pull with live progress/tag/push/search/
history/get/verify), `/v1/blueprints` (async install/uninstall),
`/v1/tasks` (async jobs, persisted, cancel/retry), `/v1/networks` + `/v1/volumes`
(local node only, via CLI), `/v1/services` (+inspect/update),
`/v1/functions`, `/v1/nodes` + `/v1/cluster/*` (health/info/replicas
create/remove/containers), `/v1/tasks/{id}/cancel|retry`, `/v1/transfers`
(inbound migration registration), `/v1/config` (panel link push),
`/v1/deauthorize`, `/v1/bootstrap/ensure`, JWT browser flows
(`GET /download/file`, `POST /upload/file`, `GET /download/backup`,
`GET /api/servers/{uuid}/ws`), per-container SFTP creds
(`GET|PUT|DELETE /v1/containers/{id}/sftp`, `GET /v1/sftp/info`).
Autostart is panel-driven: on boot wings asks
`GET {panel}/api/remote/autostart` and starts the flagged containers.

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
- Autostart: on daemon start wings asks the panel
  (`GET {panel_url}/api/remote/autostart`) and starts flagged containers
  (background-only, never blocks startup).
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
# installer + binary come straight from GitHub Releases
curl -fsSL https://github.com/animesao/cardinal-wings/releases/latest/download/install.sh -o /tmp/install-wings.sh
sudo bash /tmp/install-wings.sh
```

Or a specific version: `./install.sh v0.1.0` (from a clone), or build locally
with `./install.sh local`.

At the end the installer prints a **panel binding** block with the node
`URL` (`http(s)://<ip>:<port>`) and the generated API `Token` — paste those two
values into the panel's *Admin → Nodes → Add node* form.

### Installer env vars + firewall

| Variable | Default | Meaning |
|---|---|---|
| `WINGS_OWNER` | `animesao` | GitHub owner for releases |
| `WINGS_TLS` | `0` | generate a self-signed cert and enable TLS |
| `WINGS_HOST` | `0.0.0.0` | API bind address (`127.0.0.1` for loopback-only) |
| `WINGS_PORT` | `8080` | API port |
| `WINGS_SFTP` | `1` | enable the per-container SFTP server |
| `WINGS_SFTP_HOST` | `0.0.0.0` | SFTP bind address |
| `WINGS_SFTP_PORT` | `2022` | SFTP port |
| `WINGS_NO_FIREWALL` | `0` | `1` skips automatic firewall opening |
| `WINGS_MIRROR` | _(empty)_ | self-hosted mirror base (GitHub Releases layout) |
| `WINGS_SKIP_VERIFY` | `0` | `1` disables SHA256 verification |
| `WINGS_SYSTEMD_URL` | _(empty)_ | custom systemd unit URL (**https only** - the unit runs as root) |
| `WINGS_UPDATE_CARDINAL` | `0` | reserved: declared but not consumed by the script |
| `WINGS_DATA_DIR` | `/var/lib/cardinal-wings` | where wings keeps its state (audit log, SFTP store, tasks, backups) |

The installer opens firewall ports automatically (skip with
`WINGS_NO_FIREWALL=1`): the API port only for non-loopback binds, the SFTP
port only when SFTP is enabled on a non-loopback host. Backends in order:
`ufw` (one port per call), `firewall-cmd --permanent` + reload, `iptables`
(idempotent `-C` check, persisted via `netfilter-persistent save` or
`/etc/iptables/rules.v4`, with a warning when persistence is unavailable —
raw iptables rules are lost on reboot). Note the installer binds `0.0.0.0` by
default while `config.example.toml` documents a loopback bind.

### What the installer checks

- **SHA-256 of the binary is always verified** (mirror and GitHub alike) and a
  mismatch aborts the install. The checksum is taken from the release asset
  `<asset>.sha256` (or the mirror's `SHA256SUMS`) and matched by file name, so
  it works for both the `dist/<name>` and the plain `<name>` layouts.
  `WINGS_SKIP_VERIFY=1` disables it — do not do that in production.
- **The service is checked after the start.** If it did not come up, the
  installer says so and points at `journalctl -u cardinal-wings` instead of
  reporting success.
- **The API key is printed only on the first install.** On a re-run the
  installer points at `/etc/cardinal-wings/config.toml` instead of dumping the
  secret into your terminal history or a CI log.

The installer verifies the **binary** it downloads. The installer **script
itself** is not verified when you pipe it from `main` — if that matters, take
it from a release instead and check it against the published sum:

```bash
curl -fsSL -O https://github.com/animesao/cardinal-wings/releases/download/v0.5.19/install.sh
curl -fsSL -O https://github.com/animesao/cardinal-wings/releases/download/v0.5.19/install.sh.sha256
sha256sum -c install.sh.sha256 && sudo bash install.sh
```

### Where wings keeps its state

`WINGS_DATA_DIR` (default `/var/lib/cardinal-wings`, created by the
installer) holds `wings-audit.jsonl`, `sftp-users.json`, the SFTP host key,
`wings-tasks.json` and the backup snapshots. The systemd unit sets the
variable and the working directory, so none of these files land in `/`.

> **Slow GitHub in your region?** Point the installer at a self-hosted mirror
> with `WINGS_MIRROR=https://mirror.example.com/wings` (expects
> `<mirror>/<version>/<asset>` + `SHA256SUMS`, same layout as Releases).

## Roadmap

See [SPEC.md](SPEC.md) §Phases. Containers → images → blueprints → streaming →
cluster facade → panel + site.

## Contributing & security

See [CONTRIBUTING.md](CONTRIBUTING.md) for the contribution guide and
[SECURITY.md](SECURITY.md) for how to report a vulnerability.

## License

GNU Affero General Public License v3.0 or later — see [LICENSE](LICENSE).

* License: AGPL-3.0-or-later — [LICENSE](LICENSE)
* Attribution: [NOTICE](NOTICE)
* Brand policy: [TRADEMARKS.md](TRADEMARKS.md)
* Commercial model (incl. dual licensing): [COMMERCIAL.md](COMMERCIAL.md)

cardinal-wings is a separate project of the Cardinal ecosystem.
cardinal itself is licensed differently (Apache-2.0) — see
[animesao/cardinal](https://github.com/animesao/cardinal).