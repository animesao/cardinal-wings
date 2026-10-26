# cardinal-wings documentation

Start with the root [`AGENTS.md`](../AGENTS.md) if you are an agent or a new
maintainer. It is the map; this page is the index.

## For users

| Page | What it covers |
|---|---|
| [`../README.md`](../README.md) | Install, security summary, release process, state directory |
| [`api.md`](api.md) | Endpoint reference with `curl` examples |
| [`openapi.yaml`](openapi.yaml) | Machine-readable schema; `info.version` is release-gated |
| [`cluster.md`](cluster.md) | Multi-node setup: hub, remote `cardinal serve`, firewall |

## For maintainers and agents

| Page | What it covers |
|---|---|
| [`AGENTS.md`](AGENTS.md) | Documentation rules, language/version policy, checklist |
| [`ai/context-map.md`](ai/context-map.md) | Routing table, context budget, fast facts |
| [`architecture/runtime.md`](architecture/runtime.md) | Boot, local `cardinal serve`, nodes, health, autostart |
| [`architecture/api-and-auth.md`](architecture/api-and-auth.md) | Muxes, middleware, roles, JWTs, errors, route map |
| [`architecture/security.md`](architecture/security.md) | Trust boundaries and every security control |
| [`architecture/state-and-storage.md`](architecture/state-and-storage.md) | `WINGS_DATA_DIR`, audit, tasks, SFTP, backups |
| [`architecture/build-and-release.md`](architecture/build-and-release.md) | Toolchain, CI jobs, release artifacts, installer |
| [`status/known-issues.md`](status/known-issues.md) | Verified drift and technical gaps; read before trusting older docs |
| [`work/`](work/README.md) | Task and handoff templates for multi-step work |

## Reading order

- **A small code change:** root `AGENTS.md` → the local `AGENTS.md` for the
  package → the file you will edit.
- **An API change:** `architecture/api-and-auth.md` → `docs/api.md` →
  `docs/openapi.yaml` → the handler.
- **A security change:** `architecture/security.md` → the relevant test
  (`hardening_test.go`, `ssrf_test.go`, `archive_test.go`, `sftp_jail_test.go`).
- **A release:** `architecture/build-and-release.md` → `docs/AGENTS.md` →
  `.github/workflows/build.yml`.

Code and tests override every page here. If a document is wrong, fix the page
or add an entry to [`status/known-issues.md`](status/known-issues.md).
