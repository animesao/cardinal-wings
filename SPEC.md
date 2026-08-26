# cardinal-wings — technical specification

> **Status:** in development — nothing stable yet. This document is the plan.

cardinal-wings is a **REST API daemon for managing cardinal over HTTP**. It is
the single entry point a control plane / web panel (e.g. `cardinal-panel`)
talks to instead of SSH + CLI. It is not a Docker-compatible layer; it is `cardinal`'s own
management API.

## Why wings at all

cardinal itself is a daemonless CLI binary. `cardinal serve` already exposes a
**Docker-compatible** API for existing tools (Portainer, Dev Containers, CI).
wings is for **the project's own control plane**:

- own simple REST schema (not locked to Docker's contract)
- multiple API keys with **roles** (read-only vs admin) — `serve` has a single global token
- live streaming (logs follow) and interactive exec — only partially supported in `serve`
- a **unified facade over single-host AND the cluster** (services, functions, nodes)

## What wings reuses (does NOT re-write)

Everything below already exists in the sibling `cardinal` repo and is reached
over the **Docker-compatible HTTP API** that `cardinal serve` exposes.

> **Why HTTP and not in-process?** wings is a separate Go module (a separate
> repository). Go forbids importing another module's `internal/*` packages,
> so wings cannot call `cardinal/internal/container` directly. Instead wings
> talks HTTP to `cardinal serve` — spawning a local instance on loopback for
> this node and reaching other nodes over the network — which is both legal
> and reuses cardinal's whole handler layer unchanged.

| Capability | Where it lives |
|---|---|
| Single-host containers (list/create/inspect/start/stop/kill/rm/stats/top/logs/exec/rename/update) | `cardinal/internal/api` handlers -> `internal/container` |
| Images (list/inspect/remove/pull/search/push/tag) | `cardinal/internal/api` -> `internal/image` |
| Blueprints registry (27 templates + 13 stacks) | `cardinal-blueprints` + `cardinal/cmd/blueprint.go` |
| **Cluster**: nodes, gossip, heartbeat, join tokens | `cardinal/internal/orchestrator/cluster.go` |
| **Services**: create/scale/update/remove, replicas, auto-heal, rolling updates | `cardinal/internal/orchestrator/service.go`, `scheduler.go` |
| **FaaS**: deploy/invoke/remove, scale-to-zero GC | `cardinal/internal/orchestrator/faas.go` |
| **DNS discovery** between containers/services | `cardinal/internal/orchestrator/dns.go` |

> Rule of thumb: if `cardinal serve` exposes it, wings proxies/aggregates it;
> wings adds the **auth layer, the REST schema, the cross-node routing, and the
> streaming**.

## Architecture

```
                 ┌────────── cardinal-wings daemon ──────────┐
                 │  REST API v1 (panel-facing)               │
                 │  auth: multiple API keys + roles          │
                 │  rate-limit · TLS · loopback-by-default   │
                 └────┬───────────────┬────────────────┬─────┘
       local node     │               │   other cluster nodes (HTTP)
   (spawns a local    │               │
    `cardinal serve`) │               │
                 ┌────▼─────┐   ┌────▼────────────────┐
                 │ cardinal │   │ cardinal serve on   │   Docker API + /cluster/*
                 │ serve    │   │ remote node         │
                 │ loopback │   │                     │
                 └──────────┘   └─────────────────────┘
```

- **Local host** — wings spawns `cardinal serve -H 127.0.0.1 -p <ephemeral>
  --token <random>` as a subprocess and talks to it over the Docker-compatible
  API on loopback. So even the local node goes through the same HTTP path as
  remote nodes — one client, one code path.
- **Remote nodes (cluster)** — wings speaks HTTP to each node's
  `cardinal serve` (auth: that API's Bearer token configured in wings config).
  The cluster handles _placement_ via `orchestrator`; wings aggregates and
  exposes the whole cluster to the panel as one tree.

## API surface (proposed)

All endpoints under `/v1`, JSON. Two auth modes per key: `readonly` and `admin`.

### System
| Method | Path | Description |
|---|---|---|
| GET | `/v1/ping` | liveness, returns `"pong"` |
| GET | `/v1/version` | wings version |
| GET | `/v1/nodes` | cluster nodes + per-node health |
| GET | `/v1/system/info` | host summary (containers/images/cpu/mem) |

### Containers (single-host, local node)
| Method | Path |
|---|---|
| GET | `/v1/containers` · `/v1/containers/{id}` |
| POST | `/v1/containers` (create from image) |
| POST | `/v1/containers/{id}/start` · `/stop` · `/restart` · `/kill` |
| POST | `/v1/containers/{id}/rename` · `/update` |
| DELETE | `/v1/containers/{id}` |
| GET | `/v1/containers/{id}/stats` |
| GET | `/v1/containers/{id}/logs?tail=…&follow=1` (SSE/streaming) |
| POST | `/v1/containers/{id}/exec` (interactive, streamed) |

### Images (local node)
| Method | Path |
|---|---|
| GET | `/v1/images` · `/v1/images/{ref}` |
| POST | `/v1/images/pull` · `/v1/images/search?q=` |
| POST | `/v1/images/{ref}/tag` · `/v1/images/{ref}/push` |
| DELETE | `/v1/images/{ref}` |

### Blueprints (from the official registry)
| Method | Path |
|---|---|
| GET | `/v1/blueprints` · `/v1/blueprints/{name}` |
| POST | `/v1/blueprints/{name}/install` (returns a task id) |
| POST | `/v1/blueprints/{name}/uninstall` |
| GET | `/v1/tasks` · `/v1/tasks/{id}` (async job status) |

### Cluster services & functions
| Method | Path |
|---|---|
| GET | `/v1/services` · `/v1/services/{name}` |
| POST | `/v1/services` (create) · `/v1/services/{name}/scale` · `/update` |
| DELETE | `/v1/services/{name}` |
| GET | `/v1/functions` · `/v1/functions/{name}` |
| POST | `/v1/functions` (deploy) · `/v1/functions/{name}/invoke` |
| DELETE | `/v1/functions/{name}` |

## Security model

- **Multiple API keys**, each with a role (`readonly` | `admin`) — set in
  `/etc/cardinal-wings/config.toml`.
- **Auth:** `Authorization: Bearer <key>`, constant-time comparison.
- **Rate limit:** per-IP token bucket (like `cardinal serve`), loopback exempt.
- **Bind:** loopback `127.0.0.1` by default; external bind requires a key and
  TLS (both `--tls-cert` and `--tls-key`).
- **Timeouts:** read/write/idle and max header bytes configured.
- **Remote-node creds** (for cluster) live in the same config.toml, never
  echoed back over the API.
- `/metrics` (Prometheus) behind an admin key only.

## Phases / roadmap

1. **Phase 0 — skeleton (this repo).** go.mod, main.go, config.toml, server
   (stdlib net/http), auth middleware, systemd unit, install.sh, Makefile.
   ✅ Done.
2. **Phase 1 — containers.** list/create/inspect/start/stop/restart/kill/
   remove + stats; roles enforced. ✅ Done (via `cardinal serve` HTTP).
3. **Phase 2 — images.** list/inspect/remove/pull/search/tag/push. ✅ Done.
4. **Phase 3 — blueprints.** list/inspect/install/uninstall (install delegates
   to the `cardinal blueprint` CLI). ✅ Done.
5. **Phase 4 — streaming.** logs `follow` (SSE) and `exec`. cardinal's serve
   logs endpoint supports `tail` but not `follow` and exec is non-interactive,
   so wings implements SSE-follow by polling the logs endpoint client-side.
   ✅ Done for SSE logs + exec proxy.
6. **Phase 5 — cluster facade.** `/v1/nodes`, `/v1/cluster/health`, `/v1/cluster/
   replicas`, `/v1/cluster/containers`; cross-node routing via each node's
   `cardinal serve`. ✅ Basic `/v1/nodes` + cluster views done.
7. **Phase 6 — panel + site.** `cardinal-panel` web UI (separate repo); promote
   the `/wings/` site page to real docs; update the news post. ✅ Site page and
   news updated for the skeleton; the panel remains a separate project.

## Non-goals (v1)

- Rebuilding the cluster scheduler, auto-heal, or DNS — reuse `orchestrator`.
- Full Docker API compatibility — that's `cardinal serve`'s job.
- Building images — `cardinal build` stays a CLI concern for now.

## Dev layout

```
cardinal-wings/
├── go.mod            # module github.com/animesao/cardinal-wings; no replace needed
├── main.go
├── internal/
│   ├── config/       # TOML config: keys, roles, bind, TLS, remote nodes
│   ├── auth/         # constant-time bearer check + role lookup
│   └── server/       # http router, middleware, handlers
├── systemd/cardinal-wings.service
├── install.sh
├── Makefile
└── SPEC.md
```