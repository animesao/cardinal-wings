# Known issues and documentation drift

This file records places where documentation and code disagree, plus known
technical gaps. It is a register, not a roadmap. Label each item with the fact
status from the root `AGENTS.md` and point to evidence.

Last audited: 2026-10-26 against the code at `main@75d7636`; documentation
release `v0.5.20`.

## Documentation drift

### K-01 — The error envelope is not uniform

- **Status:** `Partial`.
- **Documents:** `docs/api.md` says errors are always
  `{"error": {"code": "...", "message": "..."}}`; `README.md` and `SPEC.md`
  repeat the same shape.
- **Code:** `internal/server/errors.go` implements that envelope for handlers
  that call `writeErr`/`writeError`. The auth middleware has its own
  `writeError` in `internal/auth/auth.go` that writes `{"error":"<string>"}`
  for 401, 403, and 429. `internal/server/system.go` returns
  `{"error":"method not allowed"}` for non-GET requests to `/v1/ping`,
  `/healthz`, and `/v1/version`.
- **Impact:** a panel that reads `error.code` works on most failures but gets
  `undefined` on auth failures and some 405s. `docs/api.md` also omits the
  `unprocessable` code that exists in `errors.go`.
- **Action:** either route auth middleware errors through a shared envelope or
  document the exception. Do not add a new error shape in the meantime.

### K-02 — CORS comment describes a feature that does not exist

- **Status:** `Partial` (behavior is fail-closed; the comment is wrong).
- **Document/code comment:** `internal/auth/auth.go` says CORS "restricts
  browser cross-origin access to loopback unless extra origins are configured".
- **Code:** `Middleware.CORS` never sets `Access-Control-Allow-Origin`, and
  `internal/config` has no origin allowlist. Preflight `OPTIONS` returns 204,
  but a browser still blocks the response because no origin is echoed.
- **Impact:** cross-origin browser clients cannot use the API directly; they
  must go through a same-origin proxy. Non-browser clients are unaffected.
- **Action:** add a documented origin allowlist to config, or rewrite the
  comment and document the current behavior. Do not assume extra origins are
  configurable.

### K-03 — "External binds require keys + TLS" overstates the check

- **Status:** `Fixed (docs)` — corrected 2026-10-26 in `README.md`,
  `SECURITY.md`, and `config.example.toml`.
- **Documents (before the fix):** `README.md` (Security), `SECURITY.md`, and
  `config.example.toml` said external binds require keys **and TLS**.
- **Code:** `Config.Validate` refuses an external bind with no keys,
  placeholder keys, or keys shorter than 12 characters, and refuses a
  half-configured TLS pair. Missing TLS is only reported by
  `Config.ExternalPlainHTTP` as a startup warning (`main.go`).
- **Impact:** an operator may believe a plain-HTTP external bind cannot start,
  and be surprised that it does (with a warning). This is a deliberate,
  documented risk: LAN/VPN panel links are legitimate.
- **Action:** make the docs say "keys are required; TLS is strongly
  recommended and warned about, not enforced".

### K-04 — `WINGS_UPDATE_CARDINAL` is documented as unused

- **Status:** `Fixed (docs)` — the README row now documents the flag
  (2026-10-26).
- **Document (before the fix):** the installer env table in `README.md` said
  `WINGS_UPDATE_CARDINAL` is "reserved: declared but not consumed by the
  script".
- **Code:** `install.sh` consumes it and updates the `cardinal` runtime
  (commit `1bbf718`). The README row was not updated.
- **Impact:** operators who want the runtime updated may not use the flag;
  operators who set it may not expect the extra network install.
- **Action:** document what the flag does, including the source it fetches
  (see K-07).

### K-05 — "Every request except `/v1/ping` requires Bearer" is false

- **Status:** `Partial`.
- **Document:** `docs/api.md` opening paragraph.
- **Code:** public (no Bearer) routes are `/v1/ping`, `/healthz`, and the panel
  browser flows mounted in `panelBrowserRoutes`:
  `GET /download/file`, `POST /upload/file`, `GET /download/backup`, and
  `GET /api/servers/{uuid}/ws`. Those carry their own JWT or none.
- **Impact:** a security reviewer may mis-scope the unauthenticated surface.
- **Action:** list the public routes and their auth (JWTs / none) explicitly.

### K-06 — `CONTRIBUTING.md` says "stdlib-only", the module is not

- **Status:** `Partial`.
- **Document:** `CONTRIBUTING.md` says wings is "deliberately stdlib-only (the
  websocket server is hand-rolled for this reason)".
- **Code:** `go.mod` requires `github.com/pkg/sftp` and `golang.org/x/crypto`
  for the built-in SFTP server. The websocket implementation (`internal/ws`)
  is hand-rolled, but the module is not dependency-free.
- **Impact:** a contributor may reject a necessary dependency or, conversely,
  assume any dependency is acceptable.
- **Action:** state "standard-library first; only the SFTP server uses
  `pkg/sftp` + `x/crypto`" and describe the bar for a new dependency.

### K-07 — The optional runtime update executes an unpinned remote script as root

- **Status:** `Partial` (accepted trade-off; it must be documented).
- **Code:** when `WINGS_UPDATE_CARDINAL=1`, `install.sh` downloads
  `https://raw.githubusercontent.com/<owner>/cardinal/main/install.sh` and runs
  it with `bash` without a checksum or pinned tag. The main wings installer
  itself verifies the downloaded binary's SHA-256, but not this nested script.
- **Impact:** a compromised `cardinal` `main` branch, or a MITM with a valid
  certificate for `raw.githubusercontent.com`, runs as root on the node. The
  same class of risk is acknowledged in `README.md` for piping the wings
  installer from `main`.
- **Action:** document the risk next to the flag, and prefer a pinned release
  URL or checksum in a future change. Do not describe the flag as safe because
  the outer installer verifies the binary.

### K-08 — Stale route and middleware comments

- **Status:** `Deprecated` (comments only).
- **Code comment (`internal/server/routes.go`):** says the v1 surface is mounted
  in `images.go`, `blueprints.go`, `containers.go`, and `system.go`, and that
  "future phases (cluster, streaming) add their own route files".
- **Reality:** cluster and streaming routes exist (`cluster.go`, `tasks.go`,
  `panel.go`, `terminal.go`, `events.go`, `metrics.go`). The file itself is
  only a comment.
- **Code comment (`internal/auth/auth.go`, `RateLimitKey`):** says the per-key
  limiter runs before authentication. `server.Run` puts authentication first
  and the per-key limiter after it. The function's raw-header fallback still
  exists, so the comment is not harmless: it invites a reorder that changes
  bucket identity.
- **Impact:** an agent may miss existing route files when tracing an endpoint,
  or move the limiter based on a comment that contradicts `server.go`.
- **Action:** update both comments to the current mount point and middleware
  order, or delete `routes.go` and rely on `server.Run` as the single source.

### K-09 — The local `cardinal serve` child is detached and cannot be stopped

- **Status:** `Partial` (resource leak; not a remote vulnerability).
- **Code:** `internal/agent/local.go` starts
  `cardinal serve -H 127.0.0.1 -p <free> --token <random> -d` and keeps the
  `exec.Cmd` in `Local.cmd`. `Local.Stop()` calls `cmd.Process.Kill()`.
- **Reality:** cardinal's `-d` flag re-execs the daemon into a new process group
  and the launcher exits (`cardinal/cmd/serve.go`). By the time `Stop()` runs,
  `cmd.Process` is the already-exited launcher, so the actual `serve` daemon
  keeps running on its loopback port. Wings also never calls `cmd.Wait()`.
- **Impact:** every wings restart can leave one orphaned `cardinal serve`
  process behind, each listening on a loopback port with a random token. The
  orphan is not reachable off-host, but it consumes memory, file descriptors,
  and ports; on a long-lived node the processes accumulate.
- **Action:** either drop `-d` and manage the foreground child (Wait + signal),
  or capture the daemon PID and stop it explicitly. Record the change here.

### K-10 — State files use two different fallbacks

- **Status:** `Partial`.
- **Code:** `wingsDataDir()` (`internal/server/sftp.go`) falls back to
  `CARDINAL_DATA_DIR` (`/root/.cardinal` for root, `$HOME/.cardinal`
  otherwise). The SFTP store, host key, panel link, and backups use it. The
  audit log (`server.Run`) and the task file (`taskDataPath`) instead fall back
  to the literal current working directory.
- **Impact:** started by hand without `WINGS_DATA_DIR`, wings writes
  `wings-audit.jsonl` and `wings-tasks.json` into whatever directory the
  operator ran it from, and the rest into the cardinal data dir. Under the
  installed systemd unit both are `/var/lib/cardinal-wings`, so production is
  unaffected. A backup or migration that copies only one location loses part
  of the state.
- **Action:** route all state paths through one resolver, or document the split
  in the installer and refuse to start without `WINGS_DATA_DIR` when no
  systemd unit is detected. Covered in
  [`../architecture/state-and-storage.md`](../architecture/state-and-storage.md).

### K-11 — The installer advertises macOS, the release has no macOS asset

- **Status:** `Partial`.
- **Code:** `install.sh` maps `uname -s` `darwin` to
  `cardinal-wings-darwin-${ARCH}`; `.github/workflows/build.yml` builds only
  `linux/amd64` and `linux/arm64`.
- **Impact:** a macOS user reaches the download step and gets a 404. The error
  is a failed download, not a clear "unsupported platform" message.
- **Action:** either add a darwin build target (runtime support is still
  unverified — most of the daemon is Linux-specific) or remove `darwin` from
  the platform switch and print a clear error.

## Security-relevant gaps worth knowing

These are not confirmed vulnerabilities. They are behaviors a reviewer or
agent should check before changing the surrounding code.

- **Local cardinal token is a subprocess argument.** `internal/agent/local.go`
  passes `--token <random>` on the command line and also exports
  `CARDINAL_TOKEN` to the child. On a multi-user host, process arguments can be
  visible via `/proc`. Loopback-only bind and a short-lived daemon reduce the
  window; do not widen the bind.
- **Audit coverage is method-based.** `auditMiddleware` records POST, PUT,
  PATCH, and DELETE. Read-only access to sensitive files (`GET /download/file`,
  `GET /v1/containers/{id}/fm/read`) is not audited. Do not claim the audit log
  is a complete access log.
- **Remote node tokens are sent as Bearer over the configured scheme.**
  `[[nodes]]` addresses may be plain `http://`; cluster traffic then carries
  the node token in cleartext. The config example uses `http://` for a LAN.
  Treat cluster links as trusted networks or terminate TLS.
- **SFTP host key and user store live in `WINGS_DATA_DIR`.** The store is
  written `0600`, but backups of that directory contain password hashes and
  the host key. Protect the directory like any other secret store.
