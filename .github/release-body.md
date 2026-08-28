**cardinal-wings** — the remote management REST API daemon for cardinal.

> Early development. This is a building skeleton; the API and config are
> still subject to change until a stable release. See [SPEC.md](SPEC.md).

## What's in this release

- Linux/amd64 and Linux/arm64 binaries (version stamped into `/v1/version`)
- `install.sh` — installs this daemon and wires the systemd service
- Automatic Cardinal boot supervisor setup via `cardinal bootstrap --install`
- `POST /v1/bootstrap/ensure` — idempotently enables and starts `cardinal-bootstrap.service` for panel-managed hosts
- Database Host provisioning can ensure boot recovery before creating `restart=always` containers

## Install

```bash
curl -fsSL https://github.com/animesao/cardinal-wings/releases/latest/download/install.sh | bash
```

Or download the binary for your platform and run:

```bash
./cardinal-wings --config config.example.toml
```

## Live API surface

- `GET /v1/ping`, `GET /v1/version`
- `GET|POST /v1/containers`, `/v1/containers/{id}` — list/create/inspect/start/stop/restart/kill/remove/stats
- `GET /v1/containers/{id}/logs?follow=1` (SSE), `POST /v1/containers/{id}/exec`
- `GET /v1/images`, `/v1/images/{ref}` — list/inspect/remove/tag/push/pull/search
- `GET /v1/blueprints`, `/v1/blueprints/{name}`, `POST .../install|uninstall`
- `GET /v1/nodes`, `/v1/cluster/health|replicas|containers`

Auth: multiple API keys with roles (`readonly`/`admin`), rate-limited, loopback by default.