**cardinal-wings** — the remote management REST API daemon for cardinal.

> Early development. This is a building skeleton; the API and config are
> still subject to change until a stable release. See [SPEC.md](https://github.com/animesao/cardinal-wings/blob/main/SPEC.md).

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
- `POST /v1/transfers` (inbound migration registration), `POST /v1/config` (panel link), `POST /v1/deauthorize`
- JWT browser flows: `GET /download/file`, `POST /upload/file`, `GET /download/backup`, `GET /api/servers/{uuid}/ws`
- SFTP: `GET /v1/sftp/info`, `GET|PUT|DELETE /v1/containers/{id}/sftp` (offline-capable sessions)
- `POST /v1/tasks/{id}/cancel|retry`, `POST /v1/bootstrap/ensure`

Auth: multiple API keys with roles (`readonly`/`admin`), rate-limited, loopback by default.

## Docs & versions

Full API reference, live install commands and the release list:
https://animesao.work.gd/wings — versions on that page are pulled
live from GitHub Releases.

[![cardinal-wings docs](https://img.shields.io/badge/docs-cardinal--wings-8b5cf6?style=flat-square)](https://animesao.work.gd/wings)