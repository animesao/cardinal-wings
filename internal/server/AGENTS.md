# internal/server — HTTP daemon

Read the root [`AGENTS.md`](../../AGENTS.md) and
[`docs/ai/context-map.md`](../../docs/ai/context-map.md) first. This file covers
traps specific to the `server` package, not the API contract; for that read
[`docs/architecture/api-and-auth.md`](../../docs/architecture/api-and-auth.md).

## Shape

- `server.go` is the entry point: `Run(cfg)` wires state, builds the two muxes,
  starts background workers, and serves. It is the mount point for every route.
- One file per area (`containers.go`, `images.go`, `filemanager.go`,
  `backups.go`, `cluster.go`, `tasks.go`, `sftp.go`, …). Roughly 60 files; do
  not "read the package".
- `routes.go` is a stale comment, not a router. Ignore its file list and follow
  `Run` (`K-08` in [`docs/status/known-issues.md`](../../docs/status/known-issues.md)).
- Package-level singletons are created or replaced in `Run`: `registry`,
  `checker`, `defaultClient`, `wingCfg`, `audit`, `taskMgr`, `notifier`, the
  SFTP store, and the panel link. Handlers assume they are initialized.

## Handler rules

- Use `writeJSON` for success and `writeErr` for errors. `writeError` exists as
  a legacy string envelope; do not add new uses (see `K-01`).
- Resolve the target node with `clientFor(w, r)` (`nodes.go`). Empty `?node=`
  means local; unknown names produce a 404 envelope.
- Wrap every mutation in `mw.AdminOnly`, regardless of method. Enforcement is
  per handler.
- Respect the body caps in `limits.go`; if a route needs a streaming body, add
  it to `largeBodyRoute` and cap it inside the handler.
- Filesystem operations resolve the container name to an on-disk ID with
  `validContainerID` / `hostDataRoot`; never join a raw request reference into a
  host path.
- Long-lived handlers (logs follow, exec, stats, terminal, events) outlive the
  server `WriteTimeout`. Do not "fix" a timeout by lowering it globally.

## Traps

- **Global state and tests.** Tests that touch `registry`, `audit`, `taskMgr`,
  or the SFTP store must set them up per test (`t.Setenv`, temp dirs) and must
  not assume a previous test left them clean. `Run` is not called by most
  handler tests.
- **Nil local client.** When the `cardinal` binary is missing, the local node
  entry exists with a nil client. `clientFor` turns that into
  `503 upstream_error`; other code paths must not dereference it.
- **Paths vs. names.** Proxied operations go through cardinal and resolve names
  itself. Local filesystem operations (fm, backup, pull, SFTP, terminal) must
  resolve the name to an ID first. Mixing them up produces "not found" or, worse,
  a host path built from user input.
- **Panel JWTs are not admin proof.** A readonly key can sign one. Downgrade
  write scopes in the handler.
- **`?token=`** is WebSocket-only. Do not read it on ordinary routes.
- **Webhooks are fire-and-forget.** A failing webhook must never propagate an
  error into the request path (`internal/webhooks`).
- **Backups run in a goroutine.** `handleBackupStoreCreate` returns 202 before
  the tar finishes. Any error must be reported through `reportBackupStatus`, not
  by writing to an already-closed response.

## Adding a route

1. Put the handler in the matching area file.
2. Mount it in that area's `*Routes` function, or in `Run` for a public route.
3. Use `clientFor` unless the operation is explicitly local-only.
4. Wrap mutations in `mw.AdminOnly`.
5. Add a handler test: success, role boundary, malformed input.
6. Update `docs/api.md`, `docs/openapi.yaml`, and `docs/architecture/` if
   behavior changed.
7. Run `go test ./internal/server/... ./internal/auth/...`.

## Verification

```sh
gofmt -l internal/server
go vet ./internal/server
go test -race ./internal/server -count=1
```

Runtime behavior (real cardinal, overlay mounts, SFTP) needs Linux; the
`E2E (wings vs real cardinal)` workflow is the non-root gate. Useful tests:
`hardening_test.go`, `ssrf_test.go`, `archive_test.go`, `fmzip_test.go`,
`filemanager_test.go`, `sftp_jail_test.go`, `sftp_store_test.go`,
`panelcompat_test.go`, `resolve_test.go`, `autostart_test.go`.
