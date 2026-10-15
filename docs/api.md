# cardinal-wings API reference

Base URL: `http://127.0.0.1:8080` (default). Every request except `/v1/ping`
requires `Authorization: Bearer <key>`.

Errors are always `{"error": {"code": "...", "message": "..."}}` with one of:
`bad_request`, `unauthorized`, `forbidden`, `not_found`, `conflict`,
`method_not_allowed`, `not_implemented`, `upstream_error`, `internal`.

## Node routing

Most resource endpoints accept `?node=<name>` to target a specific cardinal
node (default: the local node). Local-only exceptions (501 or local execution):
networks, volumes, services, functions, blueprints install, image pull.

```bash
curl -H "Authorization: Bearer KEY" "localhost:8080/v1/containers?node=node-2"
```

## Container refs: names or IDs

Every `/v1/containers/{id}` endpoint accepts a cardinal container **name** or
**ID**. Proxied calls (inspect, start/stop, stats, logs, exec, …) go through
`cardinal serve`, which resolves names itself; filesystem calls (fm, backup,
pull, sftp, terminal) resolve the name to the on-disk ID inside wings and
fall back to the raw ref when the node is unreachable.

Panels should therefore address containers by a **stable name**
(e.g. `srv-<server-uuid>`) and never store cardinal's random IDs: names
survive `reinstall` (which recreates the container with a new ID), and
daemon-local snapshot paths plus SFTP credentials stay valid across
reinstalls automatically. Name→ID resolution goes through `Inspect` with a
5 s timeout and falls back to the raw ref when the node is unreachable;
filesystem calls (fm/backup/sftp) always resolve against the **local** data
dir regardless of `?node=` (which only routes the serve-proxied calls).
An empty `Volume.Source` on create/reinstall is auto-filled as
`srv-<container-name>`, so data survives reinstalls.

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

# Prometheus metrics (admin; any key when [remote] metrics_requires_auth=false)
curl -H "Authorization: Bearer KEY" localhost:8080/v1/metrics
```

## Containers

```bash
# List (filters: ?all=1, ?state=running|stopped, ?image=, ?search=, ?limit=, ?offset=,
#        ?sort=name|status|created_at (default name), ?order=asc|desc;
#        limit default 100, capped at 1000, offset default 0)
curl -H "Authorization: Bearer KEY" "localhost:8080/v1/containers?state=running&limit=50"

# Create (admin) — ports are objects, not "host:container" strings.
# Volumes accept cardinal NAMED volumes only (e.g. "srv-<name>"); host bind
# mounts ("/etc:/data") and remote filesystems are refused.
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"image":"nginx:latest","name":"web","ports":[{"host":8080,"container":80}],"restart":"always"}' \
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

# Rename / top / wait / changes (top/changes are reads;
# rename also takes {"name"} — names limited to letters/digits/_/./-,
# commit takes {repo,tag,ref} or ?repo&?tag)
curl -X POST -H "Authorization: Bearer KEY" "localhost:8080/v1/containers/<id>/rename?name=web2"
curl -H "Authorization: Bearer KEY" "localhost:8080/v1/containers/<id>/top?ps_args=aux"
curl -X POST -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>/wait
curl -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>/changes
# Export dumps the whole container filesystem, admin-only:
curl -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>/export -o container.tar
# Commit an image from a container (admin)
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"repo":"myapp","tag":"v1"}' localhost:8080/v1/containers/<id>/commit

# Dynamic ports (admin for add/remove; mapping "host:container[/proto]",
# also accepted as {"port"} or ?mapping= / ?port=)
curl -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>/ports
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"mapping":"443:80/tcp"}' localhost:8080/v1/containers/<id>/ports/add
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"mapping":"443/tcp"}' localhost:8080/v1/containers/<id>/ports/remove

# Limits + update (admin) — limits apply live to the cgroup, the rest on restart
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"memory_bytes":2147483648,"cpus":2}' localhost:8080/v1/containers/<id>/limits
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"startup_script":"#!/bin/sh\nexec python /bot/bot.py","env":["BOT_TOKEN=secret"]}' \
  localhost:8080/v1/containers/<id>/update

# Live stats stream (SSE, ?interval=2s)
curl -N -H "Authorization: Bearer KEY" "localhost:8080/v1/containers/<id>/stats?stream=1&interval=2s"
```

## Terminal (interactive shell, admin)

```bash
# Open a session attached to the container's main process
curl -X POST -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>/terminal
# → {"session":"...","shell":"attach","cols":80,"rows":24}

# Feed stdin
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"data":"ls -la\n"}' localhost:8080/v1/containers/<id>/terminal/input

# Record the panel viewport (admin)
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"cols":120,"rows":40}' localhost:8080/v1/containers/<id>/terminal/resize
# WS control frame (not stdin): {"type":"resize","cols":120,"rows":40}

# Read output as SSE, or raw duplex over websocket
curl -N -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>/terminal/stream
# ws://localhost:8080/v1/containers/<id>/terminal/ws  (Authorization: Bearer KEY)
# (at most one close frame per direction: a received close is echoed exactly
# once, Close() after a received close only tears down TCP)
```

## Files (read-only fs + read/write file manager + cp)

Query paths (`?path=`, `?src=`, `?dst=`) are passed through **raw**; they are
base64-decoded only when the value round-trips cleanly **and** decodes to
valid UTF-8 (so a plain `/plugins` stays literal instead of turning into
binary garbage). JSON-body paths are always literal and never decoded.

```bash
# Read-only browser (works on stopped containers too)
curl -H "Authorization: Bearer KEY" "localhost:8080/v1/containers/<id>/fs/ls?path=/etc/nginx"
curl -H "Authorization: Bearer KEY" "localhost:8080/v1/containers/<id>/fs/cat?path=/etc/nginx/nginx.conf"
curl -H "Authorization: Bearer KEY" "localhost:8080/v1/containers/<id>/fs/tree?path=/data"

# File manager: list/read/download are reads, the rest is admin
curl -H "Authorization: Bearer KEY" "localhost:8080/v1/containers/<id>/fm/list?path=/"
curl -H "Authorization: Bearer KEY" "localhost:8080/v1/containers/<id>/fm/read?path=/app/config.yml"
curl -H "Authorization: Bearer KEY" "localhost:8080/v1/containers/<id>/fm/download?path=/app/server.jar" -o server.jar
# → {"content":"<base64>","binary":true}
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"path":"/app/config.yml","content":"<base64>"}' localhost:8080/v1/containers/<id>/fm/write
# fm/upload is an alias of fm/write (same base64 body).
# Whole-file uploads up to 256 MiB (384 MiB base64 cap); ordinary JSON
# bodies are capped at 8 MiB.
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"path":"/data/worlds"}' localhost:8080/v1/containers/<id>/fm/mkdir
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"path":"/tmp/old.log"}' localhost:8080/v1/containers/<id>/fm/rm
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"src":"/a.txt","dst":"/b.txt"}' localhost:8080/v1/containers/<id>/fm/move
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"path":"/app/run.sh","mode":"755"}' localhost:8080/v1/containers/<id>/fm/chmod

# Directory as zip (read) / extract zip into directory (admin)
curl -H "Authorization: Bearer KEY" "localhost:8080/v1/containers/<id>/fm/download-zip?path=/data/worlds"
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"path":"/data","content":"<base64 zip>"}' localhost:8080/v1/containers/<id>/fm/upload-zip

# Copy host <-> container (admin, {src, dst} like `cardinal cp`)
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"src":"./index.html","dst":"web:/usr/share/nginx/html/index.html"}' \
  localhost:8080/v1/containers/<id>/cp
```

## Backup (whole data dir as tar.gz)

Snapshots work on stopped and unmounted containers: the overlay mounts
on demand (same mechanism as SFTP sessions) instead of refusing with
`no dedicated data mount`.

```bash
# Daemon-local snapshots (panel "agent" driver):
# create (202, async; completion is POSTed to the panel when linked)
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"uuid":"<backup-uuid>"}' localhost:8080/v1/containers/<id>/backup
# restore a stored snapshot (?clean=1 wipes the data root first, admin)
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"backup_uuid":"<backup-uuid>"}' "localhost:8080/v1/containers/<id>/backup?clean=1"
# restore a remote archive (e.g. S3 presigned URL) without panel streaming
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"download_url":"https://…/backup.tar.gz"}' "localhost:8080/v1/containers/<id>/backup?clean=1"
# delete a stored snapshot (admin; 404 when already gone)
curl -X DELETE -H "Authorization: Bearer KEY" "localhost:8080/v1/containers/<id>/backup?uuid=<backup-uuid>"

# Store details: snapshots live at $WINGS_DATA_DIR/backups/<container>/<uuid>.tar.gz,
# restores accept {"backup_uuid"} or {"download_url"} and are capped at 20 GiB
# (live streams run with a 4 h deadline). Async creates report back with
# POST {panel}/api/remote/backups/{uuid}
# {"successful":bool,"checksum":"<sha256>","checksum_type":"sha256","size":N}.

# Ad-hoc live stream (GET) / direct binary restore (POST, admin)
curl -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>/backup -o backup.tar.gz
curl -X POST -H "Authorization: Bearer KEY" --data-binary @backup.tar.gz \
  "localhost:8080/v1/containers/<id>/backup?clean=1"
```

## Panel parity (reinstall / pull / transfer / config)

```bash
# Reinstall: recreate the container from a full CreateRequest, keeping the
# named data volume (admin)
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"name":"<id>","image":"nginx:alpine",…}' localhost:8080/v1/containers/<id>/reinstall

# Pull a remote file into the container data dir (admin)
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"url":"https://…/mods.zip","root":"/","file_name":"mods.zip"}' \
  localhost:8080/v1/containers/<id>/pull
curl -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>/pull  # [] (no queue in v1)

# Server transfer notification (admin, 202 queued; data moves via backup export/import)
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"server_id":"<uuid>","url":"https://node2:8080/v1/transfers","token":"Bearer …"}' \
  localhost:8080/v1/containers/<id>/transfer

# Inbound transfer registration on the migration target (admin, 202 ack;
# the notify call only queues — bytes move through backup export/import)
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"server_id":"<uuid>","url":"https://node2:8080/v1/transfers","token":"Bearer …"}' \
  localhost:8080/v1/transfers

# Push node configuration from the panel (admin; persists the panel link for
# SFTP verification and backup callbacks, never fails the panel sync flow)
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"panel_url":"https://panel.example.com","daemon_token_id":"…","daemon_token":"…"}' \
  localhost:8080/v1/config
# → {"ok":true,"applied":false,"panel_link":true} (always 200)
# Stored at $WINGS_DATA_DIR/panel-link.json (0600); a static [panel] config
# section wins over the pushed file. Tolerant key spellings are accepted:
# panel_url|panelURL|remote, daemon_token_id|token_id|tokenID,
# daemon_token|token|secret. The same link feeds SFTP auth
# (POST {panel}/api/remote/sftp/auth), backup callbacks
# (POST {panel}/api/remote/backups/{uuid}) and autostart
# (GET {panel}/api/remote/autostart).

# Deauthorize a user (admin; no-op — wings uses short-lived JWT/Bearer, no JTI store)
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"user":"username","servers":[]}' localhost:8080/v1/deauthorize
```

## Autostart (panel-flagged containers on boot)

The panel owns the per-server auto-start flag; on daemon startup wings asks
`GET {panel_url}/api/remote/autostart` (node-token Bearer, same scheme as the
SFTP auth call) and starts every listed container that is not running:

```text
GET {panel}/api/remote/autostart
→ {"servers":["srv-<uuid>", …]}   (only this node's servers)
```

The pass is background-only and never blocks startup: skipped with no local
cardinal client or no panel link, retried 5×10 s while the panel comes up,
then each stopped ref is inspected and started with a 1 s stagger.

## Panel browser flows (panel-signed JWT, no Bearer)

The panel signs short-lived JWTs (HMAC-SHA256 with the node daemon token,
which is a wings API key) for direct browser↔daemon traffic. Wings verifies
them against its configured keys — the React client works unchanged.
Token rules: `alg` must be HS256, `exp` is required (max TTL 24h; the panel
uses 5–10 min), and a token signed by a readonly key is read-only on the
console (control.* permissions are stripped — only `websocket.connect`
survives).

```text
GET  /download/file?token=…        scope file-download (claims server_uuid + file_path)
POST /upload/file?token=…          scope file-upload (claims server_uuid), multipart files + ?directory=
GET  /download/backup?token=…      scope backup-download (claims server_uuid + backup_uuid)
GET  /api/servers/{uuid}/ws        legacy console protocol: {"event":"auth","args":[token]}
                                   (scope websocket, claims server_uuid + permissions),
                                   then status / console output / stats events,
                                   send command / set state / send logs.
```

## Events & bootstrap

```bash
# Container events as SSE (started/stopped/removed)
curl -N -H "Authorization: Bearer KEY" localhost:8080/v1/events

# Ensure the cardinal boot supervisor on this node (admin, idempotent)
curl -X POST -H "Authorization: Bearer KEY" localhost:8080/v1/bootstrap/ensure
```

## SFTP

Wings runs an embedded SSH/SFTP server (default port `2022`, config:
`[server] sftp_enabled / sftp_host / sftp_port`). The panel assigns a
username/password **per user**: the owner gets `srv-<server8>`, each member
`srv-<server8>-<user8>` — several users can hold credentials for one
container at once (store is keyed by username; old container-keyed files
are re-keyed on load). Every SFTP session is jailed to that
container's data directory on the host (`merged/data` or
`merged/home/container`), so users can connect with any SFTP client
(Filezilla, WinSCP, …) directly to the container — including stopped ones.
On session start wings mounts the stopped container's overlay on demand (the
file manager's mechanism) and shares the mount between concurrent sessions;
the container stays off. Only when the data cannot be mounted (e.g. the image
rootfs is missing) does wings fall back to temporarily starting the container
for the session and stopping it again afterwards.

```bash
# SFTP listener info
curl -H "Authorization: Bearer KEY" localhost:8080/v1/sftp/info
# → {"enabled":true,"host":"0.0.0.0","port":2022}

# Set / update a container's SFTP credential (admin)
curl -X PUT -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"username":"srv-abc123","password":"generated-password"}' \
  localhost:8080/v1/containers/<id>/sftp
# → {"ok":true,"username":"srv-abc123","port":2022}

# SFTP status for a container (all credential holders)
curl -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>/sftp
# → {"enabled":true,"usernames":["srv-abc123","srv-abc123-du456789"]}

# Remove one user's credential, or the whole container's (admin)
curl -X DELETE -H "Authorization: Bearer KEY" "localhost:8080/v1/containers/<id>/sftp?username=srv-abc123-du456789"
curl -X DELETE -H "Authorization: Bearer KEY" localhost:8080/v1/containers/<id>/sftp
```

Client connection (same address any SFTP client uses):

```text
sftp://<node-host>:2022   user: <username>   pass: <generated-password>
```

The SSH host key is persisted at `$WINGS_DATA_DIR/sftp_host_key` (generated
on first start); credentials live in `$WINGS_DATA_DIR/sftp-users.json`
(bcrypt-hashed).

Panel-password logins (`user.uuidShort` + panel password/SSH key) are
verified against the panel's `/api/remote/sftp/auth` when a panel link is
configured (`[panel]` section or panel-pushed `/v1/config`), exactly like
classic wings — no per-user credential sync needed.

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

# Tag / push (admin). Registry credentials go in the JSON body
# (preferred — query params end up in logs); ?username=&password= still works.
curl -X POST -H "Authorization: Bearer KEY" "localhost:8080/v1/images/nginx:latest/tag?repo=myreg/nginx&tag=v1"
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"username":"u","password":"secret"}' localhost:8080/v1/images/myreg/nginx:v1/push

# Remove (admin)
curl -X DELETE -H "Authorization: Bearer KEY" localhost:8080/v1/images/nginx:latest

# History / download / offline verify
curl -H "Authorization: Bearer KEY" localhost:8080/v1/images/nginx:latest/history
curl -H "Authorization: Bearer KEY" localhost:8080/v1/images/nginx:latest/get -o image.tar
curl -X POST -H "Authorization: Bearer KEY" localhost:8080/v1/images/nginx:latest/verify
```

## System

```bash
# Disk usage (cardinal system df)
curl -H "Authorization: Bearer KEY" localhost:8080/v1/system/df

# Prune unused containers/images (admin)
curl -X POST -H "Authorization: Bearer KEY" localhost:8080/v1/system/prune

# Raw cardinal /info for a node
curl -H "Authorization: Bearer KEY" "localhost:8080/v1/info?node=local"
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

# Cancel a queued/running task (admin)
curl -X POST -H "Authorization: Bearer KEY" localhost:8080/v1/tasks/task-5/cancel

# Re-run a finished task (admin) — returns the new task
curl -X POST -H "Authorization: Bearer KEY" localhost:8080/v1/tasks/task-5/retry
```

## Networks & volumes (local node only, raw CLI output)

```bash
# Networks
curl -H "Authorization: Bearer KEY" localhost:8080/v1/networks
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"name":"mynet","subnet":"10.10.0.0/24"}' localhost:8080/v1/networks
curl -H "Authorization: Bearer KEY" localhost:8080/v1/networks/mynet
curl -X DELETE -H "Authorization: Bearer KEY" localhost:8080/v1/networks/mynet

# Volumes
curl -H "Authorization: Bearer KEY" localhost:8080/v1/volumes
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"name":"data","driver":"local","labels":{"app":"web"}}' localhost:8080/v1/volumes
curl -H "Authorization: Bearer KEY" localhost:8080/v1/volumes/data
curl -X DELETE -H "Authorization: Bearer KEY" localhost:8080/v1/volumes/data
curl -X POST -H "Authorization: Bearer KEY" localhost:8080/v1/volumes/prune

# Remote nodes (?node=) answer 501 — networks/volumes have no serve API,
# the CLI only manages the local host.
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

# Inspect / update (admin for update)
curl -H "Authorization: Bearer KEY" localhost:8080/v1/services/web
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"replicas":3}' localhost:8080/v1/services/web/update

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

`?node=` also works on health/replicas/containers. `GET /v1/cluster/replicas`
tries the daemon list first and falls back to `cardinal service ls`, so the
panel always gets a list instead of a 405.

```bash
curl -H "Authorization: Bearer KEY" localhost:8080/v1/cluster/health
curl -H "Authorization: Bearer KEY" localhost:8080/v1/cluster/info
curl -H "Authorization: Bearer KEY" localhost:8080/v1/cluster/replicas
curl -H "Authorization: Bearer KEY" localhost:8080/v1/cluster/containers

# Replicas create/remove (admin)
curl -X POST -H "Authorization: Bearer KEY" -H "Content-Type: application/json" \
  -d '{"service_name":"web","image":"nginx:alpine"}' localhost:8080/v1/cluster/replicas
curl -X DELETE -H "Authorization: Bearer KEY" localhost:8080/v1/cluster/replicas/<id>
```

## Config

```toml
[server]
host = "127.0.0.1"   # loopback default; external binds without tls_cert/tls_key
port = 8080          # start with a loud cleartext warning — use TLS, loopback
                     # or a VPN. Keys must be strong (min 12 chars, no
                     # placeholders like "change-me") or startup refuses.

[[keys]]
name = "main"
key = "change-me"    # generate a real secret before exposing wings
role = "admin"

[[nodes]]                       # optional remote cluster nodes
name = "node-2"
address = "http://10.0.0.2:2375"
token = "that-node-serve-token"
enabled = true
```