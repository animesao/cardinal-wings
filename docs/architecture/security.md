# Security model

wings is a remote management API: a compromised instance can control containers
on every node it is configured for. This page lists the controls that actually
exist, where they live in code, and which claims in older documents are
stronger than the implementation.

The endpoint contract is in [`api-and-auth.md`](api-and-auth.md); this page is
about what stops an attacker.

## Trust boundaries

```
browser / panel ──HTTPS+Bearer or panel JWT──▶ wings ──HTTP+Bearer──▶ cardinal serve ──▶ containers
                                                  │
                                                  └──SSH/SFTP (per-container jail)
```

- The panel is trusted to hold admin API keys; a readonly key is a lower tier,
  not an untrusted tier.
- `cardinal serve` is reached over the configured node URL. Its token is a
  shared secret in `config.toml`.
- The local cardinal node binds loopback only. Remote nodes are whatever the
  operator configured — a plain `http://` cluster link carries the node token
  in cleartext.
- The SFTP server exposes a single container data directory per user. It is
  not a host shell.

## Authentication surfaces

| Surface | Credential | Evidence |
|---|---|---|
| `/v1/*` (normal routes) | `Authorization: Bearer <API key>` | `internal/auth/auth.go` |
| WebSocket upgrade only | `?token=<API key>` | `internal/auth/auth.go` (`isWebSocketUpgrade`) |
| Panel browser flows (`/download/*`, `/upload/file`, `/api/servers/{uuid}/ws`) | HS256 panel JWT signed with a configured API key | `internal/server/paneljwt.go` |
| `/v1/ping`, `/healthz` | none | `internal/server/system.go` |
| SFTP | per-user password, bcrypt hash on disk | `internal/server/sftp.go` |
| Outbound panel callbacks | panel link token or `token_id.token` | `internal/server/panellink.go` |
| Remote cardinal node | node token in `[[nodes]]` | `internal/agent/remote.go` |

Keys are compared as SHA-256 digests with `subtle.ConstantTimeCompare`
(`config.Authorize`), so comparison time does not reveal key length or a
prefix. A missing header is 401; a wrong key is 403.

`?token=` exists because a browser WebSocket cannot set an `Authorization`
header. It is gated on the upgrade headers. Do not extend it to ordinary
routes: query strings end up in proxy access logs, browser history, and
`Referer`.

## Authorization

- Two roles: `readonly` and `admin` (`internal/config`).
- Mutations are wrapped in `mw.AdminOnly` in each route file. **Enforcement is
  per handler, not per HTTP method**: a new mutating route that forgets the
  wrapper is an authorization bug even though it is a POST.
- `/v1/nodes` shows backend URLs and last error only to admin keys.
- `/v1/metrics` is admin-only unless `[remote] metrics_requires_auth = false`.
- `/v1/self` returns the caller's role for UI gating; it is not an
  authorization decision by itself.
- Panel JWTs carry a role too. A readonly key can sign a JWT, so JWT handlers
  must downgrade write scopes for non-admin keys. A valid signature is proof of
  *a* configured key, not of admin.

## Panel JWTs

`internal/server/paneljwt.go`:

- exactly three segments, `alg` must be `HS256` (no `none`, no algorithm
  confusion);
- HMAC compared against **every** configured key with `ConstantTimeCompare`,
  without an early break;
- `exp` is mandatory and must be in the future; lifetime is capped at 24 hours
  (`maxPanelJWTTTL`);
- `nbf` is checked with 60 seconds of skew;
- scopes are explicit (`file-download`, `file-upload`, `backup-download`,
  `websocket`) and file/backup tokens bind `file_path` / `backup_uuid` so a
  token for one file cannot read another.

The signing key is the node's daemon token, which is also an API key. Rotating
the key invalidates outstanding browser tokens — that is intended.

## Secrets at rest

| Secret | Location | Mode |
|---|---|---|
| API keys, node tokens, webhook secrets, TLS paths | `/etc/cardinal-wings/config.toml` | `0600`, dir `0700` (`install.sh`) |
| Local cardinal token | process memory + child env/argv | never written to disk |
| Panel link token | `$WINGS_DATA_DIR/panel-link.json` | `0600`, dir `0700` |
| SFTP user hashes | `$WINGS_DATA_DIR/sftp-users.json` | `0600` |
| SFTP host key | `$WINGS_DATA_DIR/sftp_host_key` | `0600` |
| Audit log | `$WINGS_DATA_DIR/wings-audit.jsonl` | `0600`, dir `0700` |
| Backup snapshots | `$WINGS_DATA_DIR/backups/...` | `0700` dirs |

Consequences to keep in mind:

- A backup of `WINGS_DATA_DIR` contains the panel token, SFTP password hashes,
  and the host key. Treat it as a secret store, not as application data.
- The local cardinal token is passed as `--token` on the process command line,
  so a local user can read it from `/proc/<pid>/cmdline` while the process
  exists. The loopback bind and the short-lived daemon limit the exposure; do
  not widen the bind to compensate for something else. Related leak: `K-09` in
  [`../status/known-issues.md`](../status/known-issues.md).
- Health and version endpoints never return secrets. Error messages redact the
  wings data directory, cardinal data directory, and `$HOME`
  (`redactHostPaths` in `internal/server/errors.go`).

## Transport and bind policy

`Config.Validate` (`internal/config/config.go`):

- A non-loopback `server.host` requires at least one key.
- Placeholder values (`change-me`, `secret`, `password`, …) and keys shorter
  than 12 characters are refused on an external bind.
- `tls_cert` and `tls_key` must be set together; a half-configured pair is an
  error.
- **A missing TLS pair is not an error.** `Config.ExternalPlainHTTP` reports it
  and `main.go` prints a warning. This is a deliberate trade-off for LAN/VPN
  panel links; older docs claim TLS is mandatory (`K-03`).

Server limits (`internal/server/server.go`): `ReadHeaderTimeout` 10 s,
`MaxHeaderBytes` 1 MiB, read/write/idle timeouts from config (60/60/120 s by
default). Long-lived streaming routes intentionally outlive the write timeout;
check them when changing timeouts.

The systemd unit adds process hardening: `NoNewPrivileges`, `PrivateTmp`,
`RestrictSUIDSGID`, `LockPersonality`, `ProtectKernelTunables`,
`ProtectKernelModules`, and a narrow `ReadWritePaths`
(`systemd/cardinal-wings.service`). `ProtectControlGroups=false` is required
for cardinal to manage cgroups.

## SSRF policy

Two helpers in `internal/server/ssrf.go`:

- **`validateFetchURL` / `fetchGet`** — for caller-supplied URLs (container
  file pull, remote backup restore). Only `http`/`https`, no credentials in the
  URL. The host is resolved and **every** resolved address must pass
  `checkFetchIP`: loopback, link-local (including `169.254.169.254` metadata),
  multicast, unspecified, and non-global-unicast addresses are rejected. RFC
  1918 / LAN ranges are allowed on purpose because node-to-node transfers use
  them. Redirects are re-validated per hop, up to 5.
- **`safeDialContext`** — pins the resolved IP and connects to that address, so
  a DNS-rebinding host cannot pass validation and then resolve to loopback at
  connect time. This is the part that makes the check meaningful; keep the
  pinning if you refactor the validator.
- **`validatePanelURL`** — for a panel-pushed or configured panel URL. It
  requires `http(s)`, a host, and no credentials, but does **not** restrict IP
  ranges: a panel may legitimately live on the LAN. A link that fails this
  check is never persisted, which stops typos and attacker-supplied URLs from
  diverting credential checks and backup callbacks.

Webhook URLs are **not** passed through the SSRF validator. They come from the
root-owned config file, so the threat is "whoever can edit the config" rather
than "whoever can call the API" — but do not expose webhook configuration over
the API without adding validation.

## Filesystem and archive safety

- **File manager jail** (`filemanager.go`): every request path is cleaned,
  joined under the session root, and checked with `filepath.Rel`; symlinks are
  resolved and the real path must remain inside the root. For paths that do
  not exist yet, the nearest existing parent is resolved first, so a file
  cannot be created through a symlinked directory that points outside.
- **SFTP jail** (`sftp.go`): the same rule for every SFTP operation, with the
  same allow-missing handling for create/rename.
- **Tar import** (`archive.go`): absolute paths, `..`, and entries that clean
  outside the root are rejected before extraction. Expansion is capped at
  64 GiB and 1 048 576 entries.
- **Zip import/export** (`fmzip.go`): 100 MiB of uncompressed content and a
  bounded entry count; symlinks are skipped on export.
- **Backups**: the streaming restore cap is 20 GiB (`io.LimitReader` in
  `backups.go`, plus a `ContentLength` pre-check). Restore refuses a root of
  `/` or empty, so a bad container reference cannot untar over the host.
  `?clean=1` wipes the data root first; it does not touch anything outside it.
- Container references used for filesystem paths are validated
  (`validContainerID`) before being joined into a host path.

## Request-size and resource limits

| Limit | Value | Where |
|---|---|---|
| ordinary JSON body | 8 MiB | `limits.go` |
| `fm/write`, `fm/upload` | 384 MiB | `limits.go` |
| `fm/upload-zip` | 384 MiB | `limits.go` |
| streaming backup restore | 20 GiB | `backups.go` |
| tar expansion | 64 GiB / 1 048 576 entries | `archive.go` |
| zip expansion | 100 MiB | `fmzip.go` |
| headers | 1 MiB | `server.go` |
| per-IP rate | 25 req/s, burst 50, loopback exempt | `auth.go` |
| per-key rate | 10 req/s, burst 30 | `auth.go` |
| client maps | 4096 (`max_clients`), keys up to 2× | `auth.go` |

The per-key limiter runs **after** authentication, so buckets are keyed by the
configured key name. The limiter still has a fallback that keys by the raw
`Authorization` value when no key name is in the context (the comment in
`auth.go` describes the opposite order); that fallback is why the hard
`max_clients` ceiling exists. Do not move the limiter before auth.

## Audit log

`internal/server/audit.go` appends one JSON object per mutating request:
`ts`, `key`, `role`, `method`, `path`, `remote`. The file rotates at 10 MiB to
`<path>.1` and is opened `0600`.

It records POST, PUT, PATCH, and DELETE only. Successful and failed reads —
including `GET /download/file`, `GET /v1/containers/{id}/fm/read`, and
`GET /v1/containers/{id}/logs` — are **not** recorded. Do not describe it as a
complete access log.

## Webhooks

`internal/webhooks`: fire-and-forget delivery, one goroutine per hook, 10 s
timeout, no retries, no HMAC signature. A configured secret is sent as a static
`X-Webhook-Secret` header. A dead endpoint loses the event silently; that is
the designed behavior so a webhook cannot take wings down.

## Installer

`install.sh`:

- downloads the release binary and verifies it against the per-asset `.sha256`
  (or a mirror `SHA256SUMS`);
- writes the config with `umask 077` (`0600`) and the data directory `0700`;
- generates a self-signed TLS pair only when `WINGS_TLS=1`, with the key `0600`;
- opens API/SFTP ports in `ufw`, `firewalld`, or `iptables` unless
  `WINGS_NO_FIREWALL=1`;
- `WINGS_SKIP_VERIFY=1` disables checksum verification — treat it as a
  debugging switch, never a production default;
- `WINGS_UPDATE_CARDINAL=1` downloads and runs cardinal's `main` branch
  installer as root without pinning or a checksum. Documented risk: `K-07`.

## Known gaps

| Topic | Status | Record |
|---|---|---|
| Non-uniform error envelope weakens client-side handling | `Partial` | `K-01` |
| CORS comment describes a nonexistent allowlist | `Partial` | `K-02` |
| TLS not enforced on external binds | `Partial` | `K-03` |
| Unpinned nested installer as root | `Partial` | `K-07` |
| Detached local `cardinal serve` leaks a process per restart | `Partial` | `K-09` |
| Audit covers mutations only | `Partial` | this page |
| Webhook URLs bypass the SSRF validator | `Partial` | this page |

## Changing security-sensitive code

1. Write the failing test first. The existing invariants live in
   `hardening_test.go`, `ssrf_test.go`, `archive_test.go`, `fmzip_test.go`,
   `filemanager_test.go`, `sftp_jail_test.go`, `auth_test.go`, and
   `panelcompat_test.go`.
2. Do not relax a limit or a validation to make a panel flow pass. Raise a cap
   deliberately, document the memory cost, and keep the old failing test as a
   regression test with the new boundary.
3. Keep DNS pinning in `safeDialContext`; validating and then re-resolving is
   the classic TOCTOU bug.
4. Any new route that mutates state gets `mw.AdminOnly`, a body cap, and an
   audit entry via the existing middleware.
5. Any new persisted file under `WINGS_DATA_DIR` is written `0600` in a `0700`
   directory and contains no recoverable plaintext secret.
6. Record a behavior change in [`../status/known-issues.md`](../status/known-issues.md)
   if it changes an externally visible guarantee.

## Verification

```sh
gofmt -l .
go vet ./...
go test -race ./...
go test -race ./internal/auth/... ./internal/server/... -run 'SSRF|Auth|JWT|Archive|Jail|Hardening|Redact|Backup|Rate'
bash -n install.sh && shellcheck install.sh   # when available
```

Runtime checks (real namespaces, loopback, cardinal binary) need Linux; the
`E2E (wings vs real cardinal)` workflow is the non-root runtime gate. A
Windows cross-build verifies compilation only.
