# docs — user and maintainer documentation

Read the root [`AGENTS.md`](../AGENTS.md) and
[`ai/context-map.md`](ai/context-map.md) first. This file covers the rules that
keep the documentation set from drifting.

## Layout

| Path | Audience | Notes |
|---|---|---|
| [`api.md`](api.md) | Panel/API users | Endpoint reference with `curl` examples. Grep a section; it is not the machine contract. |
| [`openapi.yaml`](openapi.yaml) | Panel/client generators | Machine-readable schema. `info.version` is release-gated in CI. |
| [`cluster.md`](cluster.md) | Operators | Multi-node wiring: hub, remote `cardinal serve`, firewall. |
| [`../SPEC.md`](../SPEC.md) | Maintainers | Design intent and roadmap. Not proof of current behavior. |
| [`../README.md`](../README.md) | Users | Install, operations, security summary, release process. |
| [`architecture/`](architecture/) | Maintainers/agents | Current subsystem behavior with file evidence. |
| [`status/known-issues.md`](status/known-issues.md) | Maintainers/agents | Verified drift and technical gaps. Read before trusting older docs. |
| [`ai/context-map.md`](ai/context-map.md) | Agents | Routing layer and context budget. |
| [`work/`](work/) | Agents/maintainers | Task and handoff records for multi-step work. |

## Language and version markers

- Documentation is written in **English only**. This repository has no
  `docs/en`/`docs/ru` mirrors; do not create them without an explicit decision.
- There is **no** `cardinal-version` marker block in this repository and no
  `scripts/sync-docs-version.sh`. Do not copy cardinal's marker blocks here.
- The release identity is the git tag. The only documentation value CI checks
  is `info.version` in [`openapi.yaml`](openapi.yaml): on a `v*` tag push it
  must equal the tag without the leading `v`. Update it before tagging.
  `redocly lint` validates structure only, not this value.
- `GET /v1/version` reports the version stamped into the binary. When it
  disagrees with the OpenAPI spec, the running build wins.

## Content rules

- Code and tests override every document here. If a page contradicts the code,
  fix the page or record the drift; do not "fix" the code to match prose.
- Keep `docs/api.md` and `docs/openapi.yaml` aligned with the handlers. An
  endpoint that exists in code but not in the OpenAPI schema is a bug in the
  docs; an endpoint in the schema that code does not serve is worse.
- Do not duplicate architecture detail in `api.md` or `README.md`. Link to
  [`architecture/`](architecture/) instead.
- Security claims must match [`architecture/security.md`](architecture/security.md)
  and the code. In particular: keys are required on external binds, TLS is a
  warning when absent, `?token=` is WebSocket-only, and the error envelope has
  known exceptions listed in [`status/known-issues.md`](status/known-issues.md).
- Relative links are preferred. External links should point at stable upstream
  pages, not search results or temporary URLs.
- Never paste API keys, node tokens, panel tokens, passwords, private keys, or
  real customer data into docs or examples. Use `$WINGS_KEY`, `CHANGE-ME`, or
  another obvious placeholder.
- Historical documents (`SPEC.md` roadmap, release notes, old plans) stay
  historical. Add a date or a superseded note instead of rewriting them to look
  current.

## Verification

This repository has no documentation link checker or secret-scanning script
for docs. Verify manually:

1. Every relative link and image target exists. Run from the repository root,
   for example with a small throwaway script or editor link check.
2. Fenced code blocks with `curl` include the `Authorization` header where the
   route is authenticated, and use a placeholder key.
3. `info.version` in `docs/openapi.yaml` matches the intended release.
4. `npx --yes @redocly/cli lint docs/openapi.yaml` passes after schema edits
   (also part of the `lint` CI job).
5. No real secrets were added. The `gitleaks` CI job scans the history.

## Before closing a documentation change

- [ ] Relative links resolve.
- [ ] New endpoints appear in both `docs/api.md` and `docs/openapi.yaml`, or
      the omission is recorded as a known issue.
- [ ] Security statements match the code, not an older audit or README claim.
- [ ] `info.version` is unchanged, or intentionally bumped for a release.
- [ ] New drift is recorded in [`status/known-issues.md`](status/known-issues.md).
- [ ] No token, key, password, or private key is present in the diff.
