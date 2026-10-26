# Context map

This file is the routing layer between the root [`AGENTS.md`](../../AGENTS.md)
and the rest of the repository. Its job is to keep an agent's context small and
correct: read one routing row, open the smallest set of files that can prove or
disprove the task, and stop.

It is not a tutorial and it is not a second copy of the architecture docs.

## Context tiers

| Tier | Read | When |
|---|---|---|
| 0 | [`AGENTS.md`](../../AGENTS.md) | Always. It contains the non-negotiables and the verification matrix. |
| 1 | This map + one local `AGENTS.md` | Before the first edit in an area. |
| 2 | One architecture document + [`docs/status/known-issues.md`](../status/known-issues.md) | Before trusting older docs or changing a contract. |
| 3 | Source files and tests that the task touches | Before editing. Code wins over every document. |
| 4 | `git log` / `git blame` | Only when the question is "why is it like this and no doc says". |

## Routing

| Task | Open first | Then (only if needed) |
|---|---|---|
| Add or change a `/v1` endpoint | `internal/server/<area>.go` | `internal/server/errors.go`, `docs/api.md`, `docs/openapi.yaml`, `internal/server/AGENTS.md` |
| Change middleware order or body limits | `internal/server/server.go`, `limits.go` | `internal/auth/auth.go`, `docs/architecture/api-and-auth.md` |
| Auth key, role, or rate-limit behavior | `internal/config/config.go` (`Authorize`, `KeyName`, `Validate`) and `internal/auth/auth.go` | `internal/server/paneljwt.go`, `internal/auth/auth_test.go` |
| Local cardinal spawn or node health | `internal/agent/local.go`, `internal/server/nodes.go`, `healthcheck.go` | `docs/architecture/runtime.md`, `internal/server/AGENTS.md` |
| Remote node / cluster routing | `internal/server/nodes.go`, `internal/agent/remote.go`, `internal/server/cluster.go` | `docs/cluster.md`, `internal/runtime/client.go` |
| Async task lifecycle | `internal/tasks/tasks.go`, `internal/server/tasks.go` | `internal/server/webhooks.go`, `internal/tasks/AGENTS.md` |
| SSRF / URL fetching | `internal/server/ssrf.go` | `internal/server/ssrf_test.go`, `docs/architecture/security.md` |
| Archive import/export | `internal/server/archive.go`, `fmzip.go` | `internal/server/limits.go`, `docs/architecture/security.md` |
| File manager jail or path handling | `internal/server/filemanager.go`, `filemanager.go` helpers | `internal/server/patharg_test.go`, `sftp_jail_test.go` |
| Backup snapshot/restore | `internal/server/backups_store.go`, `backups.go` | `docs/architecture/state-and-storage.md` |
| SFTP credentials or jail | `internal/server/sftp.go` | `sftp_store_test.go`, `sftp_jail_test.go` |
| Panel JWT browser flows | `internal/server/paneljwt.go`, `panelbrowser.go` | `internal/server/panellink.go`, `docs/architecture/security.md` |
| Config file syntax or validation | `internal/config/config.go`, `config.example.toml` | `internal/config/config_test.go`, `internal/config/AGENTS.md` |
| Prometheus / dashboard aggregates | `internal/server/metrics.go`, `dashboard.go` | `docs/api.md` |
| Installer or firewall behavior | `install.sh` | `docs/architecture/build-and-release.md`, `README.md` |
| CI or release | `.github/workflows/build.yml`, `.github/workflows/e2e.yml` | `Makefile`, `docs/architecture/build-and-release.md` |
| OpenAPI / panel client generation | `docs/openapi.yaml` | `CONTRIBUTING.md` release rule, `docs/AGENTS.md` |

## Context budget

- Root `AGENTS.md` + this map: read fully, always.
- One architecture document: read fully.
- `docs/api.md`: grep the section; it is a usage reference, not a spec.
- `docs/openapi.yaml`: grep the path or schema; 1000+ lines are not a starting
  read.
- `SPEC.md`: read the section you need. It states design intent and a roadmap;
  it is not proof of current behavior.
- `internal/server` has ~60 Go files. Never "read the package". Start from the
  route that calls the handler, then follow one function.
- Tests are the cheapest evidence: `hardening_test.go`, `ssrf_test.go`,
  `archive_test.go`, `sftp_jail_test.go`, `limits` behavior and
  `panelcompat_test.go` encode most security invariants.

## Fast facts

| Question | Answer | Evidence |
|---|---|---|
| Where does the daemon start? | `main.go` parses flags, loads config, validates, warns on plain HTTP, calls `server.Run` | `main.go` |
| Where is the middleware chain? | `server.Run`, outer → inner: CORS, body limit, per-IP limit, auth, per-key limit, audit | `internal/server/server.go` |
| How is the local node reached? | Spawn `cardinal serve -H 127.0.0.1 -p <free> --token <random> -d`, wait up to 15 s | `internal/agent/local.go` |
| How are nodes resolved? | `?node=<name>`, default `local`; unknown node → 404 | `internal/server/nodes.go` |
| How often is node health checked? | Every 15 s, 5 s timeout per node | `internal/server/healthcheck.go` |
| Where is state? | `WINGS_DATA_DIR` → audit, tasks, SFTP store, host key, panel link, backups. Fallback is split: cardinal's data dir for most files, the process CWD for audit/tasks (`K-10`) | `server.go`, `tasks.go`, `sftp.go`, `backups_store.go`, `panellink.go` |
| How is the version set? | `-ldflags -X github.com/animesao/cardinal-wings/internal/server.version=<tag>` | `.github/workflows/build.yml`, `internal/server/server.go` |
| What gates a release? | Tag `v*` + `docs/openapi.yaml` `info.version` must match the tag without `v` | `.github/workflows/build.yml` |
| What is the error shape? | `{"error":{"code":"…","message":"…"}}` from `writeErr` — with known exceptions; see `docs/status/known-issues.md` | `internal/server/errors.go` |

## Do not trust without checking

The following claims exist in older docs and are either imprecise or wrong.
Each has an entry in [`docs/status/known-issues.md`](../status/known-issues.md):

- "External binds require keys **and TLS**" — code requires keys and refuses
  placeholder/short keys; missing TLS is a warning, not a refusal.
- "Every response uses the object error envelope" — auth middleware and some
  405 paths still return `{"error":"string"}`.
- "CORS restricts origins to loopback unless configured" — there is no origin
  allowlist in config; the middleware sets no `Access-Control-Allow-Origin`.
- "`WINGS_UPDATE_CARDINAL` is unused/reserved" — `install.sh` consumes it.
- "Every request except `/v1/ping` requires a Bearer token" — `/healthz` and
  the panel browser routes authenticate differently or are public.
- "`Local.Stop()` shuts down the local cardinal node" — `cardinal serve -d`
  detaches, so the launcher is killed, not the daemon.

## Where to record new knowledge

- New verified drift or a security gap: `docs/status/known-issues.md`.
- A durable design decision: a short note in the relevant architecture
  document under "Decisions"; this repo has no ADR directory yet.
- Multi-step work: `docs/work/` using the templates.
