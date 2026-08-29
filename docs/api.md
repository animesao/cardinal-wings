# cardinal-wings API reference

Base URL: `http://127.0.0.1:8080` (default). Every request except `/v1/ping`
requires `Authorization: Bearer <key>`.

Errors are always `{"error": {"code": "...", "message": "..."}}` with one of:
`bad_request`, `unauthorized`, `forbidden`, `not_found`, `conflict`,
`method_not_allowed`, `not_implemented`, `upstream_error`, `internal`.

## Node routing

Every resource endpoint accepts `?node=<name>` to target a specific cardinal
node (default: the local node):

```bash
curl -H "Authorization: Bearer KEY" "localhost:8080/v1/containers?node=node-2"
```

## System

```bash
# Liveness
curl localhost:8080/v1/ping                      # pong

# Wings version
curl -H "Authorization: Bearer KEY" localhost:8080/v1/version

# Role of the current key (UI gating)
curl -H "Authorization: Bearer KEY" localhost:8080/v1/self
# → {"authenticated":true,"role":"admin","admin":true}

# Cluster nodes + live health (background checker)
curl -H "Authorization: Bearer KEY" localhost:8080/v1/nodes

# Dashboard aggregate: per-node containers/images/cpu/mem + totals
curl -H "Authorization: Bearer KEY" localhost:8080/v1/system/info

# Prometheus metrics (admin only)
curl -H "Authorization: Bearer KEY" localhost:8080/v1/metrics
```

## Containers

```bash
# List (filters: ?all=1, ?state=running|stopped, ?image=, ?search=, ?limit=, ?offset=)
curl -H "Authorization: Bearer KEY" "localhost:8080/v1/containers?state=running&limit=50"

# Create (admin)
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"image":"nginx:latest","name":"web","ports":["8080:80"],"restart":"always"}' \
  localhost:8080/v1/containers

# Inspect
curl -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>

# Lifecycle (admin)
curl -X POST -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>/start
curl -X POST -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>/stop
curl -X POST -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>/restart
curl -X POST -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>/kill

# Remove (admin, ?force=1)
curl -X DELETE -H "Authorization: Bearer KEY" "localhost:8080/v1/containers/<id>?force=1"

# Stats
curl -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>/stats

# Logs (tail; follow=1 → SSE stream)
curl -H "Authorization: Bearer KEY" "localhost:8080/v1/containers/<id>/logs?tail=100"
curl -N -H "Authorization: Bearer KEY" "localhost:8080/v1/containers/<id>/logs?follow=1"

# Exec (admin) — runs a command, returns an exec id
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"Cmd":["ls","-la"],"AttachStdout":true}' \
  localhost:8080/v1/containers/<id>/exec

# Exec stream (admin) — runs a command and streams output as SSE (live log)
curl -N -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"Cmd":["tail","-f","/var/log/app.log"]}' \
  localhost:8080/v1/containers/<id>/exec/stream
```

## SFTP

Wings runs an embedded SSH/SFTP server (default port `2022`, config:
`[server] sftp_enabled / sftp_host / sftp_port`). The panel assigns a
username/password per container; every SFTP session is jailed to that
container's data directory on the host (`merged/data` or
`merged/home/container`), so users can connect with any SFTP client
(Filezilla, WinSCP, …) directly to the container. When the overlay is not
mounted (e.g. after a host reboot), wings temporarily starts the container
for the session and stops it again afterwards.

```bash
# SFTP listener info
curl -H "Authorization: Bearer KEY" localhost:8080/v1/sftp/info
# → {"enabled":true,"host":"0.0.0.0","port":2022}

# Set / update a container's SFTP credential (admin)
curl -X PUT -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"username":"srv-abc123","password":"generated-password"}' \
  localhost:8080/v1/containers/<id>/sftp
# → {"ok":true,"username":"srv-abc123","port":2022}

# SFTP status for a container
curl -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>/sftp
# → {"enabled":true,"username":"srv-abc123"}

# Remove a container's SFTP credential (admin)
curl -X DELETE -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>/sftp
```

Client connection (same address any SFTP client uses):

```text
sftp://<node-host>:2022   user: <username>   pass: <generated-password>
```

The SSH host key is persisted at `$WINGS_DATA_DIR/sftp_host_key` (generated
on first start); credentials live in `$WINGS_DATA_DIR/sftp-users.json`
(bcrypt-hashed).

## Images

```bash
# List / inspect
curl -H "Authorization: Bearer KEY" localhost:8080/v1/images
curl -H "Authorization: Bearer KEY" localhost:8080/v1/images/nginx:latest

# Search Docker Hub
curl -H "Authorization: Bearer KEY" "localhost:8080/v1/images/search?q=postgres"

# Pull (admin) — async, returns a task id
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"image":"postgres:16"}' \
  localhost:8080/v1/images/postgres:16/pull
# → {"task_id":"task-3","action":"pull","image":"postgres:16"}

# Tag / push (admin)
curl -X POST -H "Authorization: Bearer KEY" "localhost:8080/v1/images/nginx:latest/tag?repo=myreg/nginx&tag=v1"
curl -X POST -H "Authorization: Bearer KEY" "localhost:8080/v1/images/myreg/nginx:v1/push"

# Remove (admin)
curl -X DELETE -H "Authorization: Bearer KEY" localhost:8080/v1/images/nginx:latest
```

## Blueprints

```bash
# Catalog / detail
curl -H "Authorization: Bearer KEY" localhost:8080/v1/blueprints
curl -H "Authorization: Bearer KEY" localhost:8080/v1/blueprints/minecraft-server

# Install (admin) — async, poll the returned task id
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"memory":"2g","cpus":"2","env":["EULA=TRUE"]}' \
  localhost:8080/v1/blueprints/minecraft-server/install
# → {"task_id":"task-5","action":"install","name":"minecraft-server"}

# Uninstall (admin) — async
curl -X POST -H "Authorization: Bearer KEY" localhost:8080/v1/blueprints/minecraft-server/uninstall
```

## Tasks (async jobs)

```bash
# List / poll
curl -H "Authorization: Bearer KEY" localhost:8080/v1/tasks
curl -H "Authorization: Bearer KEY" localhost:8080/v1/tasks/task-5
# → {"id":"task-5","kind":"blueprint_install","status":"succeeded",
#    "created_at":"…","started_at":"…","finished_at":"…","output":"…"}
```

## Services & functions (delegated to cardinal CLI)

```bash
# Services
curl -H "Authorization: Bearer KEY" localhost:8080/v1/services

curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"name":"web","image":"nginx:latest","replicas":2,"ports":["8080:80"]}' \
  localhost:8080/v1/services

curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"replicas":3}' localhost:8080/v1/services/web/scale

curl -X DELETE -H "Authorization: Bearer KEY" localhost:8080/v1/services/web

# Functions
curl -H "Authorization: Bearer KEY" localhost:8080/v1/functions

curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"name":"hello","image":"ghcr.io/example/hello:latest"}' \
  localhost:8080/v1/functions

curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"data":"{\"name\":\"world\"}"}' localhost:8080/v1/functions/hello/invoke

curl -X DELETE -H "Authorization: Bearer KEY" localhost:8080/v1/functions/hello
```

## Cluster

```bash
curl -H "Authorization: Bearer KEY" localhost:8080/v1/cluster/health
curl -H "Authorization: Bearer KEY" localhost:8080/v1/cluster/replicas
curl -H "Authorization: Bearer KEY" localhost:8080/v1/cluster/containers
```

## Config

```toml
[server]
host = "127.0.0.1"
port = 8080

[[keys]]
name = "main"
key = "change-me"
role = "admin"

[[nodes]]                       # optional remote cluster nodes
name = "node-2"
address = "http://10.0.0.2:2375"
token = "that-node-serve-token"
enabled = true
```