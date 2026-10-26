# State and storage

wings has no database. Everything it remembers between restarts is a small set
of files on disk. This page is the map: who writes each file, its format, its
permissions, and what breaks if you delete it.

The security consequences (secrets in these files, backups of the directory)
are in [`security.md`](security.md). This page is about layout and lifecycle.

## Where state lives

`wingsDataDir()` (`internal/server/sftp.go`, used by the SFTP store, host key,
panel link, audit, and backups):

1. `WINGS_DATA_DIR`, when set;
2. otherwise `cardinalDataDir()`:
   - `CARDINAL_DATA_DIR`, when set;
   - `/root/.cardinal` for uid 0;
   - `$HOME/.cardinal` otherwise.

The systemd unit sets both variables explicitly
(`WINGS_DATA_DIR=/var/lib/cardinal-wings`,
`CARDINAL_DATA_DIR=/root/.cardinal`) and uses `/var/lib/cardinal-wings` as the
working directory (`systemd/cardinal-wings.service`).

**Not every file uses that function.** Tasks and the audit log use a shorter
fallback:

| File | Resolver | Fallback when `WINGS_DATA_DIR` is unset |
|---|---|---|
| `sftp-users.json`, `sftp_host_key`, `panel-link.json`, `backups/` | `wingsDataDir()` | cardinal data dir (`/root/.cardinal` for root) |
| `wings-audit.jsonl` | `server.Run` | `.` (the process working directory) |
| `wings-tasks.json` | `taskDataPath()` | `.` (the process working directory) |

Under systemd the working directory is `/var/lib/cardinal-wings`, so the two
paths converge. Run wings by hand without `WINGS_DATA_DIR` and it will split
state across the current directory and the cardinal data dir. This is recorded
as `K-10` in [`../status/known-issues.md`](../status/known-issues.md); always
set `WINGS_DATA_DIR` explicitly.

## Files

### `wings-audit.jsonl`

- **Writer:** `internal/server/audit.go` (`initAudit`, `record`).
- **Format:** one JSON object per line:
  `{"ts":"<RFC3339 UTC>","key":"<key name>","role":"admin|readonly","method":"POST","path":"/v1/...","remote":"<ip:port>"}`.
- **Content:** mutating requests only (POST, PUT, PATCH, DELETE). No request
  body, no response status.
- **Rotation:** at 10 MiB the file is renamed to `wings-audit.jsonl.1` and a
  new file is started. Only one generation is kept; the previous `.1` is
  overwritten.
- **Mode:** `0600`, directory `0700`.
- **Delete safely?** Yes, you lose history; the daemon recreates it.

### `wings-tasks.json`

- **Writer:** `internal/server/tasks.go` (`taskDataPath`) and
  `internal/tasks.Manager` (`WithPersistence`, `saveLocked`).
- **Format:** JSON array of finished `Task` objects (`id`, `kind`, `status`,
  `created_at`, `started_at`, `finished_at`, `error`, `output`, `progress`).
- **Lifecycle:** loaded at startup; rewritten whenever a task finishes or is
  canceled. Finished tasks older than the manager TTL (1 hour) are pruned on
  the next `GET /v1/tasks` (`taskMgr.Prune()`).
- **Mode:** `0600`.
- **Limits:** the re-runnable body of a task (`jobFunc`) is memory-only, so
  `POST /v1/tasks/{id}/retry` does not work after a restart for a task loaded
  from disk. `output` is persisted verbatim; a noisy task can make this file
  large.
- **Delete safely?** Yes, you lose task history and any not-yet-polled result.

### `panel-link.json`

- **Writer:** `internal/server/panellink.go` (`savePanelLink`), pushed by
  `POST /v1/config`.
- **Format:** `{"url":"https://panel.example","token_id":"…","token":"…"}`.
- **Precedence:** a non-empty `[panel]` section in `config.toml` wins; the file
  is used when the config section is empty. This is how a panel-managed node
  works without editing the config.
- **Write:** temp file `panel-link.json.tmp` + rename, `0600`, directory `0700`.
- **Used for:** SFTP password checks, backup completion callbacks, autostart.
- **Delete safely?** Only if you do not need panel callbacks or SFTP auth; the
  next panel save recreates it.

### `sftp-users.json`

- **Writer:** `internal/server/sftp.go` (`loadSFTPStore`, `set`).
- **Format:** object keyed by container ID; each entry has `username`,
  `password_hash` (bcrypt), `container_id`, and optional user/owner metadata.
  Legacy entries keyed by `srv-<id>` are migrated on load.
- **Usernames:** owner `srv-<server8>`, members `srv-<server8>-<user8>`.
- **Mode:** `0600`, directory `0700`.
- **Delete safely?** No: every SFTP user loses access until the panel reissues
  credentials. It also contains password hashes — treat it as a secret.

### `sftp_host_key`

- **Writer:** `internal/server/sftp.go` (`loadHostKey`).
- **Format:** PEM-encoded Ed25519 private key, generated on first start.
- **Mode:** `0600`.
- **Delete safely?** Technically yes, but every SFTP client will see a new host
  fingerprint and must re-trust it. Do not rotate casually.

### `backups/`

- **Layout:** `$WINGS_DATA_DIR/backups/<container-id>/<uuid>.tar.gz`.
- **Writer:** `internal/server/backups_store.go`.
- **Flow:** `POST /v1/containers/{id}/backup {uuid}` returns 202 immediately,
  then a goroutine tars the data root to `<uuid>.tar.gz.part`, renames it to
  `<uuid>.tar.gz` on success, and (when a panel link exists) reports
  `{successful, checksum, checksum_type:"sha256", size}` to
  `POST {panel}/api/remote/backups/{uuid}`.
- **Permissions:** directories `0700`; the archive itself is created with
  `os.Create` and therefore the process umask, not an explicit mode. The
  directory is the effective protection.
- **Content:** the container data directory only. The backup job refuses an
  empty root or `/`, so it cannot archive the host filesystem.
- **Delete safely?** Yes, you drop the stored snapshots; the panel may still
  list them until it refreshes.

### `config.toml`

Written by `install.sh` with `umask 077` (`0600`) in `/etc/cardinal-wings`
(`0700`). Contains API keys, node tokens, webhook secrets, TLS paths, and the
`[panel]` fallback. It is not read by the API at runtime; edits require a
restart because `main.go` loads it once.

### Cardinal's data directory

`CARDINAL_DATA_DIR` (default `/root/.cardinal` for root) is **owned by
cardinal**, not wings. wings reads container paths from it:

```
$CARDINAL_DATA_DIR/overlay/<container-id>/merged          container rootfs
$CARDINAL_DATA_DIR/overlay/<container-id>/merged/data     itzg-style data root
$CARDINAL_DATA_DIR/overlay/<container-id>/merged/home/container
```

`containerDataRoot`, `ensureDataRoot`, `fmRoot`, and the SFTP jail derive their
roots from this layout (`internal/server/filemanager.go`, `sftp.go`). When the
overlay is not mounted, the file manager, SFTP, and backup paths either fail or
temporarily start the container for the duration of the operation. Do not
"fix" a missing mount by pointing wings at `/`.

## Single-writer assumption

- The audit log appends under a mutex, and the task manager serializes writes
  under its own mutex — but **only within one process**.
- Two wings daemons pointed at the same `WINGS_DATA_DIR` will interleave audit
  lines, overwrite `wings-tasks.json` on every completion, and race on
  `panel-link.json`. There is no lock file. Run one daemon per state directory.
- The task file is rewritten in place; a crash during the write can leave
  invalid JSON. On load, a parse failure is ignored and the manager starts
  empty rather than refusing to boot.

## Backup, migration, and cleanup

- To migrate a node: stop wings, copy `/etc/cardinal-wings/config.toml` and the
  whole `WINGS_DATA_DIR`, then start wings on the target with the same
  `WINGS_DATA_DIR`. The directory contains secrets (panel token, SFTP hashes,
  host key) — move it over a trusted channel.
- To reset panel integration: delete `panel-link.json` and let the panel push
  the node again.
- To reclaim disk: prune `backups/` and rotate/truncate `wings-audit.jsonl`.
  Do not delete `sftp-users.json` or `sftp_host_key` to save space.
- `WINGS_DATA_DIR` must be writable by the daemon user. The installed unit runs
  as root because cardinal needs root for namespaces, cgroups, and loop
  devices; a different user will fail at container operations, not just at
  state writes.

## Evidence

| Claim | Evidence |
|---|---|
| Data-dir resolution and fallback | `internal/server/sftp.go` (`wingsDataDir`, `cardinalDataDir`) |
| Audit path, fields, rotation, mode | `internal/server/audit.go`, `internal/server/server.go` |
| Task persistence, TTL, prune, retry | `internal/tasks/tasks.go`, `internal/server/tasks.go` |
| Panel link format, precedence, atomic write | `internal/server/panellink.go`, `panel.go` |
| SFTP store and host key | `internal/server/sftp.go`, `sftp_store_test.go` |
| Backup layout and checksum report | `internal/server/backups_store.go` |
| Cardinal overlay layout | `internal/server/filemanager.go`, `sftp.go` |
| Unit environment and hardening | `systemd/cardinal-wings.service`, `install.sh` |

Verification is `go test ./internal/server/... ./internal/tasks/...` for the
file logic. The paths themselves are Linux host paths; a Windows run exercises
the same code with `t.TempDir()` and environment overrides.
