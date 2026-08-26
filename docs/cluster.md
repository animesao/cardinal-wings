# Multi-node cluster with wings

wings is the panel's single entry point over one or many cardinal nodes. A
wings instance manages **its own node** (spawning a local `cardinal serve` on
loopback) plus any **remote nodes** you list in its config. The panel talks
only to wings; wings fans out to every node.

```
        ┌──────────────────────────────┐
        │        web panel             │
        └──────────────┬───────────────┘
                       │ https / bearer token
        ┌──────────────▼───────────────┐
        │        cardinal-wings        │   (hub — runs on any one host)
        └──┬──────────┬──────────┬─────┘
           │ local    │ remote   │ remote
      ┌────▼────┐ ┌────▼─────┐ ┌──▼───────┐
      │ node A  │ │ node B   │ │ node C   │
      │ serve   │ │ serve    │ │ serve    │
      └─────────┘ └──────────┘ └──────────┘
```

## 1. Install wings on the hub

```bash
curl -fsSL https://github.com/animesao/cardinal-wings/releases/latest/download/install.sh | bash
```

## 2. Install + start `cardinal serve` on every node

On each server (A, B, C) run cardinal's own serve with a shared token:

```bash
cardinal serve on -p 2375 -H 0.0.0.0 --token "CHANGE-ME-CLUSTER-TOKEN"
```

> Use the **same token** for all nodes, or generate one per node and list each
> in wings config — both work.

If the nodes are in a cardinal cluster already (`cardinal cluster init/join`),
the serve API also exposes `/cluster/*` (replicas, health) — wings surfaces
those under `/v1/cluster/*`.

## 3. Configure wings with the nodes

Edit `/etc/cardinal-wings/config.toml` on the hub:

```toml
[server]
host = "127.0.0.1"          # loopback + reverse proxy, or 0.0.0.0 with TLS
port = 8080

[[keys]]
name = "panel"
key = "panel-admin-key"
role = "admin"

[[nodes]]
name = "node-a"
address = "http://10.0.0.2:2375"
token = "CHANGE-ME-CLUSTER-TOKEN"
enabled = true

[[nodes]]
name = "node-b"
address = "http://10.0.0.3:2375"
token = "CHANGE-ME-CLUSTER-TOKEN"
enabled = true
```

## 4. Restart and verify

```bash
systemctl restart cardinal-wings
curl -H "Authorization: Bearer panel-admin-key" localhost:8080/v1/nodes
```

You should see `local`, `node-a`, `node-b` with live `status` (health-checked
every 15s).

## Using the cluster from the panel

- **Dashboard:** `GET /v1/system/info` aggregates every node in one call.
- **Target a node:** every resource endpoint takes `?node=<name>`:

```bash
curl -H "Authorization: Bearer panel-admin-key" \
  "localhost:8080/v1/containers?node=node-b&state=running"

curl -X POST -H "Authorization: Bearer panel-admin-key" \
  -d '{"image":"nginx:latest"}' \
  "localhost:8080/v1/containers?node=node-a"
```

- **Events:** `GET /v1/events` streams container events (SSE).
- **Metrics:** `GET /v1/metrics` (admin) exposes per-node gauges for Prometheus.

## Firewall notes

- wings hub: open only the port your proxy/panel needs (8080 by default).
- cardinal serve on nodes: **must be reachable from the hub** (2375). Keep it
  behind the cluster token; do not expose 2375 to the public internet.
- Prefer TLS: run wings behind a reverse proxy (Caddy/nginx) with a real cert,
  or set `tls_cert`/`tls_key` in wings config (`WINGS_TLS=1` during install
  generates a self-signed pair).
