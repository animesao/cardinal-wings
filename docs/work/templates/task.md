# Task: <short title>

- **Status:** `Planned` | `In progress` | `Blocked`
- **Owner:** <name or agent>
- **Started:** <YYYY-MM-DD>
- **Branch / commit:** <branch or `main@<sha>`>
- **Related issue / plan:** <link or "none">

## Goal

One paragraph. What observable outcome should exist when this is done?

## Scope

- <in scope>

## Non-goals

- <explicitly out of scope>

## Current behavior and evidence

State what the code does today, not what older docs claim. Use the
[`context map`](../../ai/context-map.md) to find the right files.

| Claim | Evidence (file, test, command) | Fact label |
|---|---|---|
| <claim> | `<path>:<line>` or `<command>` | `Implemented` |

## Constraints

- Security controls from the root [`AGENTS.md`](../../../AGENTS.md) must not be
  weakened.
- `docs/openapi.yaml` and `docs/api.md` are the public contract; update both for
  an API change. `info.version` is release-gated.
- Persisted JSON field names under `WINGS_DATA_DIR` are contracts.
- There is no `VERSION` file and no documentation-version script in this repo.
  Do not add cardinal-style marker blocks.
- <other constraints>

## Plan

1. <step>
2. <step>
3. <step>

## Verification plan

| Check | Command | Environment |
|---|---|---|
| Formatting | `gofmt -l .` | any |
| Vet | `go vet ./...` | any |
| Unit/integration | `go test -race ./...` | Linux (race detector); `go test ./...` elsewhere |
| API contract | `npx --yes @redocly/cli lint docs/openapi.yaml` | any with Node |
| Installer | `bash -n install.sh` (+ `shellcheck`) | Linux/macOS/WSL |
| Runtime | `.github/workflows/e2e.yml` or a manual run against real `cardinal` | Linux |

Name the checks that cannot run on this host and why.

## Decisions and open questions

- <decision, with the alternative rejected>
- <question that must be answered before implementation>

## Exit criteria

- [ ] Behavior implemented and covered by a test that fails without the change.
- [ ] `docs/api.md` / `docs/openapi.yaml` updated, or the omission recorded.
- [ ] New drift recorded in [`../../status/known-issues.md`](../../status/known-issues.md).
- [ ] Handoff written with exact commands and results.
