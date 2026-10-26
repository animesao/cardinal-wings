# Runtime: processes, nodes, and boot

This page describes what happens between `main()` and a served request, and how
wings reaches one or many cardinal nodes. It is the "how it runs" companion to
[`api-and-auth.md`](api-and-auth.md) ("what it serves").

## Scope

- Process boot and shutdown.
- The local `cardinal serve` subprocess.
- The node registry, `?node=` routing, and health checks.
- Cluster aggregation and panel-driven autostart.

Container, image, filesystem, and backup behavior belongs to cardinal and is
not reimplemented here. See [`SPEC.md`](../../SPEC.md) for the design intent and
[`api-and-auth.md`](api-and-auth.md) for the routes that expose it.

## Boot sequence

`main.go`:

1. Parses `-config` (default `/etc/cardinal-wings/config.toml`), `-port`, and
   `-host` overrides.
2. Loads the TOML subset via `config.Load`. A missing file yields defaults with
   no keys, so the daemon can start and warn.
3. Applies `-port`/`-host` overrides, then `cfg.Validate()`. An invalid config
   exits non-zero before any port is opened.
4. Calls `cfg.ExternalPlainHTTP()`; an external listener without TLS prints a
   warning to stderr but still starts (see `K-03` in
   [`known-issues.md`](../status/known-issues.md)).
5. Calls `server.Run(cfg)`.

`server.Run` (`internal/server/server.go`) then, in order:

1. Sets the package-level `wingCfg`.
2. Tries `agent.StartLocal()`. Success installs the local runtime client;
   failure is logged and the daemon continues in degraded mode.
3. Builds the node registry: `local` first, then every enabled `[[nodes]]`
   entry via `agent.RemoteClients`.
4. Starts the background node health checker.
5. Initializes the audit log, webhooks, and the per-container SFTP server (a
   failure disables SFTP without killing the API).
6. Starts the panel-driven autostart pass in the background.
7. Mounts the public and authenticated muxes, builds the middleware chain, and
   serves HTTP or HTTPS.
8. Waits for `SIGINT`/`SIGTERM`, then shuts down with a 10-second context.

The version string comes from the package variable in `internal/server`, set at
link time by CI. `/v1/version` reports it; a local `go build` reports `dev`.

## Local node

`internal/agent/local.go`:

- Allocates a free loopback port with `net.Listen("tcp", "127.0.0.1:0")`.
- Generates a 24-byte random token (hex-encoded) per boot.
- Runs `cardinal serve -H 127.0.0.1 -p <port> --token <token> -d` and also sets
  `CARDINAL_TOKEN` in the child environment as a fallback. The CLI flag is the
  primary channel; the env copy exists because a past cobra refactor dropped
  remote flags.
- Waits up to 15 s for `/_ping`, polling every 250 ms with a 500 ms timeout.
- Returns a `runtime.Client` bound to `http://127.0.0.1:<port>`.

The token is never written to disk and never returned by the API.

**Known limitation (K-09):** `-d` makes cardinal re-exec the daemon into a new
process group and the launcher exits. `Local.Stop()` therefore kills the
launcher, not the daemon. Treat the current stop path as best-effort; do not
copy it into new code. See [`known-issues.md`](../status/known-issues.md#k-09--the-local-cardinal-serve-child-is-detached-and-cannot-be-stopped).

## Remote nodes and `?node=`

- `[[nodes]]` entries have `name`, `address`, `token`, and `enabled`.
- `agent.RemoteClients` skips disabled entries and prefixes `http://` when the
  address has no scheme.
- `registerNodes` publishes an ordered set; `local` is always present (with a
  nil client in degraded mode).
- Handlers call `clientFor(w, r)` (`internal/server/nodes.go`):
  - empty `?node=` → the local client, or `503 upstream_error` when cardinal is
    missing;
  - known name → that client;
  - unknown name → `404 not_found` with `unknown node: <name>`.
- `/v1/nodes` exposes `name`, `local`, and live `status`. The backend URL and
  error text are admin-only; readonly keys get names and liveness only
  (`internal/server/cluster.go`).

`/v1/cluster/replicas` first asks the daemon; because `cardinal serve` has no
list endpoint it falls back to `cardinal service ls` through `internal/agent`.
Cluster mutations are admin-only.

## Health checks

`internal/server/healthcheck.go`:

- One ticker, 15-second interval, started once at boot.
- Every node is pinged concurrently with a 5-second per-node timeout.
- Results are stored as `up`/`down` plus `checked_at` and, for admins, the last
  error.
- `/healthz` is unauthenticated and returns 200 when the local node is up, 503
  when it is down or unknown. It is a readiness probe for a supervisor or load
  balancer, not an authenticated API.
- A node that was never checked reports `unknown` in `/v1/nodes`.

Health checks do not write to the audit log and do not mutate node state.

## Autostart

`internal/server/autostart.go`:

- The panel owns the per-server autostart flag.
- On boot, wings calls `GET {panel}/api/remote/autostart` with the panel link
  credentials and expects `{"servers":["<ref>", …]}`.
- It retries the panel call up to 5 times with 10-second gaps, then gives up
  without blocking startup.
- For each ref it inspects the container and starts it if it is not running,
  one per second, with a 30-second timeout per operation.
- No panel link or no local cardinal client → the pass is skipped.

The pass is best-effort: a failure is logged, never fatal.

## Streaming and long-lived requests

Some handlers outlive the default 60-second write timeout: logs follow, exec
streams, stats streams, terminal SSE/WebSocket, and events. The server's
`WriteTimeout` is therefore a starting point, not a hard cap for streaming
routes. When changing timeouts, check the streaming handlers and the e2e smoke,
not just a normal JSON response.

## Decisions

- **Why HTTP instead of importing cardinal packages?** wings is a separate Go
  module, and Go forbids importing another module's `internal/*` packages.
  Talking to `cardinal serve` is legal, reuses cardinal's handlers unchanged,
  and gives local and remote nodes one code path. Do not add a `replace`
  directive to shortcut this (`go.mod` says so explicitly).
- **Why a local `cardinal serve` instead of linking the CLI?** The API is the
  same surface the panel uses; a second integration path would drift.
- **Why degraded mode?** A panel must be able to distinguish "wings is dead"
  from "cardinal is missing on this node". Refusing to boot hides that.

## Verification

| Claim | Evidence |
|---|---|
| Boot order, degraded mode, middleware chain | `internal/server/server.go` |
| Local spawn, token, 15 s readiness | `internal/agent/local.go` |
| Remote client construction | `internal/agent/remote.go` |
| `?node=` resolution and 404 | `internal/server/nodes.go`, `resolve_test.go` |
| Health interval and timeout | `internal/server/healthcheck.go` |
| Admin-only node metadata | `internal/server/cluster.go` |
| Autostart behavior | `internal/server/autostart.go`, `autostart_test.go` |
| Detached-daemon limitation | `cardinal/cmd/serve.go` (`-d`), `K-09` |

Runtime behavior needs Linux + the `cardinal` binary. A Windows cross-build
proves compilation only. The `E2E (wings vs real cardinal)` workflow is the
runtime check that does not require root.
