# AGENTS.md — cardinal-wings

This is the entry point for AI agents and new maintainers. It is a map, not an
encyclopedia. Read only what the task needs, then verify against code and tests.

## First 10 minutes

1. `git status --short --branch` and `git log -5 --oneline` — never overwrite
   work you did not create.
2. Read this file, then [`docs/ai/context-map.md`](docs/ai/context-map.md).
3. Read the nearest local `AGENTS.md` for the area you will edit.
4. Read the one relevant architecture document, not all of them.
5. Check [`docs/status/known-issues.md`](docs/status/known-issues.md) before
   trusting older docs.
6. Open the smallest set of source files that can prove or disprove the task.

## What this repository is

`cardinal-wings` is a single Go daemon that exposes the panel-facing REST API
for one cardinal host or a whole cluster. It is **not** a container runtime and
**not** a Docker API implementation: it manages cardinal through `cardinal
serve` over HTTP and adds authentication, roles, streaming, cross-node routing,
and a REST schema of its own.

Facts to keep in mind:

- Separate Go module `github.com/animesao/cardinal-wings`, standard-library
  first. The only external modules are `github.com/pkg/sftp` and
  `golang.org/x/crypto` for the built-in SFTP server (`go.mod`).
- The local node is a `cardinal serve` subprocess spawned on loopback with an
  ephemeral port and a random token (`internal/agent/local.go`). When the
  `cardinal` binary is missing, wings still starts in degraded mode and every
  node health check reports `down` (`internal/server/server.go`).
- Remote nodes are `[[nodes]]` entries in `/etc/cardinal-wings/config.toml`;
  their `cardinal serve` Bearer tokens never leave the config.
- State lives under `WINGS_DATA_DIR`. The installed systemd unit sets
  `/var/lib/cardinal-wings`. Code has two fallbacks when the variable is unset:
  the SFTP store, host key, panel link, and backups fall back to cardinal's
  data directory, while the audit log and task file fall back to the process
  working directory (`K-10` in `docs/status/known-issues.md`). Always set the
  variable; see `docs/architecture/state-and-storage.md`.
- The version is a build-time variable in `internal/server` set with
  `-ldflags -X ...internal/server.version=<tag>`; local builds report `dev`.
- There is no `VERSION` file and no documentation-version script. The release
  identity is the git tag plus `info.version` in `docs/openapi.yaml`, which CI
  compares on tag pushes (`.github/workflows/build.yml`).
- Documentation is English-only. There are no `docs/en`/`docs/ru` mirrors.
- The API is mounted in Go 1.22+ `http.ServeMux` patterns; there is no router
  dependency. Middleware order is CORS → body limit → per-IP rate limit →
  bearer auth → per-key rate limit → audit (`internal/server/server.go`).

## Source-of-truth order

When documents disagree, trust this order:

1. Runtime behavior: code and tests.
2. Public contracts: handlers, `docs/openapi.yaml`, `docs/api.md`, and the
   persisted JSON shapes in `wings-tasks.json`, `sftp-users.json`, and
   `panel-link.json`.
3. Build and release: `go.mod`, `Makefile`, `.github/workflows/*.yml`,
   `install.sh`.
4. `SPEC.md` (design intent) and `README.md` (user-facing claims).
5. Maintainer docs in `docs/architecture/`, `docs/status/`.
6. Git history and commit messages for the "why" when no document explains it.

If you find drift, record it in `docs/status/known-issues.md`; do not silently
rewrite history or invent a rationale.

## Context routing

| If the task is about… | Read first | Then |
|---|---|---|
| Boot, local `cardinal serve` spawn, node registry, health checks | [`internal/agent/AGENTS.md`](internal/agent/AGENTS.md) | [`docs/architecture/runtime.md`](docs/architecture/runtime.md), `internal/server/nodes.go`, `healthcheck.go` |
| Routes, middleware chain, HTTP handlers, error envelope | [`internal/server/AGENTS.md`](internal/server/AGENTS.md) | [`docs/architecture/api-and-auth.md`](docs/architecture/api-and-auth.md) |
| API keys, roles, rate limiting, JWTs, CORS | [`internal/auth/AGENTS.md`](internal/auth/AGENTS.md) | [`docs/architecture/security.md`](docs/architecture/security.md), `internal/server/paneljwt.go` |
| TOML parsing, defaults, `Validate`, external-bind rules | [`internal/config/AGENTS.md`](internal/config/AGENTS.md) | `config.example.toml`, `internal/config/config.go` |
| Async tasks, cancellation, retry, webhooks | [`internal/tasks/AGENTS.md`](internal/tasks/AGENTS.md) | [`docs/architecture/state-and-storage.md`](docs/architecture/state-and-storage.md) |
| SSRF, archive extraction, file jail, backups, SFTP | [`docs/architecture/security.md`](docs/architecture/security.md) | `internal/server/ssrf.go`, `archive.go`, `filemanager.go`, `backups_store.go`, `sftp.go` |
| Endpoint reference for a panel client | [`docs/api.md`](docs/api.md) | `docs/openapi.yaml`, the matching handler |
| Build, CI, release, installer | [`docs/architecture/build-and-release.md`](docs/architecture/build-and-release.md) | `Makefile`, `.github/workflows/build.yml`, `install.sh` |
| Cluster wiring and `?node=` routing | [`docs/architecture/runtime.md`](docs/architecture/runtime.md) | [`docs/cluster.md`](docs/cluster.md), `internal/server/cluster.go`, `nodes.go` |

Do not read `docs/openapi.yaml` (1000+ lines) or `docs/api.md` (400+ lines)
end to end unless the task is about the contract. Grep for the path or field.

## Non-negotiables

- Do not weaken a security control to make a test, demo, or panel flow pass. In
  particular: bearer-key checks, role enforcement, per-IP/per-key rate limits,
  JWT signature/expiry/TTL checks, SSRF dial-time pinning, archive path checks,
  the file-manager jail, request-body caps, and the audit log.
- `?token=` is accepted for WebSocket upgrades only. Do not extend it to normal
  routes; query strings leak into proxy logs and browser history.
- An external bind requires configured keys; placeholder values and keys
  shorter than 12 characters are refused. TLS is strongly required but only
  warned about in code (`ExternalPlainHTTP`) — keep that distinction when
  writing docs.
- Panel JWTs are HS256, verified against every configured key, with a required
  `exp` and a 24-hour TTL cap. Non-admin keys never gain write scopes.
- Everything goes through cardinal: `cardinal serve` over HTTP or the
  `cardinal` CLI. Do not reimplement container, image, or cluster logic in
  wings.
- Every resource endpoint stays node-aware (`?node=<name>`, default local).
- Do not change persisted JSON field names or response shapes without checking
  the panel client and `docs/openapi.yaml`.
- Never commit API keys, panel tokens, node tokens, private keys, or a filled
  `config.toml`. Examples use placeholders.
- Do not run `git reset --hard`, `git clean`, force-push, or delete branches
  without explicit user permission.
- Do not claim a test, lint, or e2e passed unless you ran it and saw the
  result. On a non-Linux host, a successful cross-compile is not runtime
  verification.

## Verification matrix

Run the narrowest check that can falsify the change, then the repository gates.

| Change | Minimum verification |
|---|---|
| Go code | `gofmt -l .`, `go vet ./...`, `go test -race ./...` |
| Windows host, no Linux runtime | `$env:GOOS='linux'; $env:GOARCH='amd64'; go build ./...` plus `go vet ./...`; say which checks were not run |
| API route or response | Above + `npx --yes @redocly/cli lint docs/openapi.yaml`, `docs/api.md`, `docs/openapi.yaml`, and `go test ./internal/server/...` |
| Auth / security control | `go test ./internal/auth/... ./internal/server/...` and, for runtime behavior, the e2e workflow (`.github/workflows/e2e.yml`) — compile-only is not enough |
| Config parser/validation | `go test ./internal/config/...` + a manual start with a bad config |
| Installer | `bash -n install.sh` (and `shellcheck install.sh` when available) |
| Docs only | relative links resolve; no secrets; `docs/openapi.yaml` untouched unless the API changed |
| Release | tag `v*`; `docs/openapi.yaml` `info.version` equals the tag without `v`; build + e2e workflows green |

Repository gates from [`CONTRIBUTING.md`](CONTRIBUTING.md):

```sh
gofmt -l .          # must be empty
go vet ./...        # must pass
go test -race ./... # must pass
bash -n install.sh  # installer syntax
```

On Windows without `bash`, run the Go gates in PowerShell and state that
`bash -n`/`shellcheck` and the Linux e2e were not executed.

## Fact status labels

Use these labels in plans, status files, and handoffs:

- `Implemented` — code exists and a test or runtime check proves it.
- `Partial` — code exists but a known gap remains.
- `Planned` — accepted intent, no working implementation.
- `Deprecated` — still present but should not be used for new work.
- `Unknown` — not verified; say so instead of guessing.

Every non-obvious claim should point to a file, test, command, or commit.

## Change protocol

1. State the goal, scope, and non-goals in a task file for multi-step work
   (see [`docs/work/`](docs/work/README.md)).
2. Make the smallest change that satisfies the goal.
3. Update tests with behavior changes. A security fix needs a test that fails
   before it and passes after it.
4. Keep `docs/api.md` and `docs/openapi.yaml` in sync when the API changes;
   set `info.version` before tagging.
5. Update the relevant architecture doc only if durable behavior changed.
6. Add or close an entry in `docs/status/known-issues.md` when you discover or
   fix drift.
7. Write a handoff with exact commands and results, including failures.

## Context maintenance

Add a new architecture document only when a subsystem cannot be understood from
an existing one. Add a local `AGENTS.md` only where a future agent would
otherwise repeat a costly mistake. Keep this file short; move detail down the
tree.
