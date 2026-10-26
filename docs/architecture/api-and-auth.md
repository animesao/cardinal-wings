# API surface and authentication

This page explains how a request reaches a handler, which routes are public,
how roles and rate limits apply, and what a handler must do to stay consistent.
The endpoint-by-endpoint reference is [`../api.md`](../api.md); the machine
schema is [`../openapi.yaml`](../openapi.yaml).

## Two muxes

`server.Run` builds two `http.ServeMux` instances:

| Mux | Routes | Auth |
|---|---|---|
| `public` | `/v1/ping`, `/healthz`, panel browser flows | none, or a panel JWT verified by the route itself |
| `api` | everything under `/v1` mounted by the route files | Bearer API key through the middleware chain |

`public.Handle("/", handler)` forwards every unmatched path to the
authenticated chain, so an unknown path still requires a key. The public routes
are exact patterns; there is no catch-all bypass.

Public routes:

| Route | Auth | Notes |
|---|---|---|
| `GET /v1/ping` | none | `text/plain`, body `pong\n` |
| `GET /healthz` | none | 200 when the local node is up, 503 otherwise |
| `GET /download/file` | panel JWT, scope `file-download` | browser download |
| `POST /upload/file` | panel JWT, scope `file-upload` | browser upload; audited |
| `GET /download/backup` | panel JWT, scope `backup-download` | snapshot download |
| `GET /api/servers/{uuid}/ws` | panel JWT / websocket protocol | legacy console websocket |

The JWT flows are described under "Panel JWTs" below. `docs/api.md` simplifies
this to "everything except `/v1/ping` needs a Bearer token"; that sentence is
tracked as `K-05` in [`../status/known-issues.md`](../status/known-issues.md).

## Middleware chain

Order is outer to inner (`internal/server/server.go`):

```
CORS
 └─ body limit
     └─ per-IP rate limit
         └─ bearer authentication
             └─ per-key rate limit
                 └─ audit
                     └─ handler
```

Why this order:

- **CORS first** so even an early rejection carries the headers (see `K-02` for
  what is actually sent).
- **Body limit before buffering** so a huge request is rejected before a
  handler allocates memory.
- **Per-IP limit before auth** so unauthenticated floods are cheap to reject.
- **Authentication before the per-key limit** so the bucket is keyed by the
  configured key name, not by an attacker-controlled header string. The limiter
  retains a raw-header fallback (and a hard map ceiling) for the opposite
  order; the comment in `auth.go` is stale — see `K-08`.
- **Audit last** so the key name and role are already in the request context.

### Body limits

`internal/server/limits.go`:

| Route | Cap |
|---|---|
| ordinary JSON bodies | 8 MiB |
| `/fm/write`, `/fm/upload` (whole-file base64) | 384 MiB |
| `/fm/upload-zip` (base64 archive) | 384 MiB |
| `POST|PUT .../backup` (binary streaming restore) | exempt from the middleware; handler caps at 20 GiB |

Archive expansion has separate caps (`maxTarOutputBytes` 64 GiB,
`maxTarEntries` 1 048 576) and zip archives are capped at 100 MiB of
uncompressed content (`internal/server/archive.go`, `fmzip.go`). Do not remove
a cap because a panel needs a larger upload; raise it deliberately and document
the memory cost.

### Rate limits

`internal/auth/auth.go`, configured by `[rate_limit]`:

- per-IP token bucket, default 25 req/s, burst 50; **loopback is exempt**;
- per-key token bucket, default 10 req/s, burst 30; applies to loopback too;
- client maps are pruned and have a hard ceiling (`max_clients`, default 4096;
  the key map is allowed up to 2× before returning 429).

Exceeding a limit returns 429. The per-IP response is
`{"error":"rate limit exceeded"}` and the per-key response is
`{"error":"key rate limit exceeded"}` in the current auth middleware (see
`K-01`).

### Authentication and roles

- Header: `Authorization: Bearer <key>`.
- Missing header → 401 `missing bearer token`.
- Unknown key → 403 `invalid API key`.
- `?token=<key>` is accepted **only** when the request is a WebSocket upgrade
  (`Upgrade: websocket` plus `Connection: upgrade`). Normal routes must use the
  header; query strings leak into proxy logs and history.
- Keys are compared by SHA-256 + `subtle.ConstantTimeCompare`, so length does
  not leak through timing (`config.Authorize`).
- Roles are `readonly` and `admin`. Mutating handlers wrap themselves in
  `mw.AdminOnly`; a readonly key gets 403 `admin role required`.
- `/v1/self` returns the role for UI gating. `/v1/metrics` is admin-only unless
  `[remote] metrics_requires_auth = false`. `/v1/nodes` hides backend URLs and
  error text from readonly keys.

Admin enforcement is per handler, not per HTTP method. When adding a route,
wrap every mutation in `mw.AdminOnly`; a GET that exposes secrets, host paths,
or a destructive action also needs a role check.

## Panel JWTs

The panel's browser flows cannot set an `Authorization` header, so the panel
signs a short-lived JWT with the node's daemon token — the same string that is
one of the wings API keys. `internal/server/paneljwt.go` verifies it:

- exactly three segments, header `alg` must be `HS256`;
- signature HMAC-SHA256 over `header.payload`, compared against **every**
  configured key (no early break, so the matching key index does not leak);
- `exp` is required and must be in the future; TTL is capped at 24 hours;
- `nbf`, when present, is checked with a 60-second clock skew;
- scopes are space-separated: `file-download`, `file-upload`,
  `backup-download`, `websocket`;
- claims also bind the token to `server_uuid`, and for file/backup flows to
  `file_path` / `backup_uuid`.

A readonly key can sign a JWT, so handlers must downgrade write permissions for
non-admin roles. Do not treat a valid JWT as proof of admin. JWT routes live on
the public mux and are audited explicitly where they mutate (`POST
/upload/file`).

## Errors

The intended envelope is:

```json
{"error":{"code":"not_found","message":"container not found: web"}}
```

Codes are defined in `internal/server/errors.go`: `bad_request`, `unauthorized`,
`forbidden`, `not_found`, `conflict`, `unprocessable`,
`method_not_allowed`, `not_implemented`, `upstream_error`, `internal`.

`writeErr` also redacts the wings data directory, cardinal data directory, and
`$HOME` from messages so a backend error does not disclose host paths to a
readonly caller.

Known exceptions: the auth middleware writes `{"error":"<string>"}` for
401/403/429, and three 405 paths in `system.go` do the same. A new handler
should use `writeErr`; do not add another shape. See `K-01`.

## Route map

Handlers are grouped by file. Use this to find the code, then open
[`../api.md`](../api.md) for request shapes and examples.

| Area | Route file(s) | Notes |
|---|---|---|
| system, version, health, metrics | `system.go`, `dashboard.go`, `metrics.go` | `/v1/version`, `/v1/self`, `/v1/system/info`, `/v1/metrics` |
| containers | `containers.go`, `containers_extra.go`, `terminal.go`, `exec.go` | list/create/inspect/lifecycle/logs/exec/terminal |
| images | `images.go` | list/inspect/pull/push/tag/search/verify |
| files | `filemanager.go`, `fs.go`, `fmzip.go`, `archive.go` | jailed file manager and host/container copy |
| backups | `backups.go`, `backups_store.go` | streaming snapshot/restore + daemon-local snapshots |
| networks, volumes | `networks.go`, `volumes.go` | local node only, via cardinal CLI |
| blueprints | `blueprints.go` | catalog + async install/uninstall |
| services, functions | `services.go` | delegated to `cardinal service` / `fn` |
| cluster, nodes | `cluster.go`, `nodes.go` | registry, health, replicas, `?node=` |
| tasks | `tasks.go` | async job list/poll/cancel/retry |
| panel parity | `panel.go`, `panellink.go`, `panelbrowser.go` | reinstall, transfers, config push, JWT browser flows |
| bootstrap | `bootstrap.go` | ensure the cardinal boot supervisor |
| SFTP | `sftp.go` | credentials + jailed SFTP server |
| events, streams | `events.go`, `terminal.go` | SSE and WebSocket |

Every resource endpoint accepts `?node=<name>` and defaults to `local`
(`clientFor` in `nodes.go`). Filesystem and SFTP operations resolve against the
local data directory even when `?node=` is set; see `docs/api.md`.

## Timeouts and transport

`server.Run` sets:

- `ReadHeaderTimeout` 10 s, `MaxHeaderBytes` 1 MiB;
- `ReadTimeout`, `WriteTimeout`, `IdleTimeout` from config, defaulting to
  60/60/120 s (`[server] read_timeout_seconds`, etc.);
- TLS when both `tls_cert` and `tls_key` are set; `http.Server` still requires
  a modern TLS config, and the installer can generate a self-signed pair for
  testing.

Streaming handlers write for longer than `WriteTimeout`; verify a change against
the streaming e2e/manual smoke, not only a JSON request.

## Adding or changing an endpoint

1. Mount it in the right `*Routes` function (or `server.Run` for a special
   case). Keep the handler in the area file.
2. Use `writeErr`/`writeJSON`. Do not invent a status-only response.
3. Wrap mutations in `mw.AdminOnly`, even if the route pattern already implies
   a POST.
4. Keep `?node=` support unless the operation is explicitly local-only; then
   document why in `docs/api.md`.
5. Update `docs/api.md`, `docs/openapi.yaml`, and, if the release is being cut,
   `info.version`.
6. Add a handler test: at minimum the success path, the role boundary, and one
   malformed input.
7. Run `go test ./internal/server/... ./internal/auth/...` and the OpenAPI lint.

## Evidence

| Claim | Evidence |
|---|---|
| Mux split and middleware order | `internal/server/server.go` |
| Public JWT routes | `internal/server/panelbrowser.go` |
| Bearer/role/rate-limit behavior | `internal/auth/auth.go`, `auth_test.go` |
| JWT verification | `internal/server/paneljwt.go` |
| Error envelope and path redaction | `internal/server/errors.go` |
| Body caps | `internal/server/limits.go` |
| Endpoint reference | `docs/api.md`, `docs/openapi.yaml` |
| Runtime e2e | `.github/workflows/e2e.yml` |
