# internal/config — TOML subset, defaults, validation

Read the root [`AGENTS.md`](../../AGENTS.md) first. This file covers the parser
and the security checks in `Validate`; the resulting runtime behavior is in
[`docs/architecture/api-and-auth.md`](../../docs/architecture/api-and-auth.md)
and [`docs/architecture/security.md`](../../docs/architecture/security.md).

## What this package is

A hand-written TOML **subset** parser plus `Config`, `Default`, `Load`,
`Validate`, and the key helpers. There is deliberately no TOML dependency:
`go.mod` stays close to the standard library (see `K-06` for the wording in
`CONTRIBUTING.md`).

## Supported syntax

- Line-based `key = value` inside `[section]` or `[[array-of-tables]]`.
- Sections that are actually consumed: `server`, `rate_limit`, `keys`,
  `nodes`, `remote`, `panel`, `webhooks`. Anything else is ignored.
- `[[keys]]`: a new entry starts at `name`; `key` and `role` fill the last
  entry. An entry without `name` attaches to the previous one.
- `[[webhooks]]`: a new entry starts at `url`; `name`, `events`, `secret`,
  `enabled` fill the last entry. `events` is comma-separated.
- Inline comments: `#` cuts the line only when preceded by whitespace and not
  inside a quote.
- Bools: `parseBool` accepts `true/1/yes/on` (case-insensitive).
- Strings: `unquote` trims matching-or-not leading/trailing `"` or `'`.

**Not supported:** multiline strings, arrays, nested tables (`[a.b]`), dates,
escapes, environment expansion, includes. Do not add a feature to the parser
without a test in `config_test.go` and an example in `config.example.toml`.

## Silent-failure behavior (important)

- Unknown keys in a known section are ignored.
- A value that fails to parse (`port = "8080"`, `sftp_enabled = maybe`) is
  ignored **without an error**; the default stays in place.
- An unreadable file other than "not exist" is an error; a missing file returns
  `Default()` so the daemon can start and warn.
- `Load` does not call `Validate`; `main.go` does. A test that calls `Load`
  alone is not testing startup safety.

If you change this, prefer returning an error for a malformed known key — but do
it as an explicit decision with a migration note, because existing configs rely
on the tolerant behavior.

## Defaults

`Default()` binds `127.0.0.1:8080`, enables SFTP on `0.0.0.0:2022`, sets
`metrics_requires_auth = true`, and rate-limits at 25/50 IP and 10/30 per key
with `max_clients = 4096`. It has **no keys**, so every authenticated route is
unusable until the operator adds one. That is intentional.

## Validate

`Validate` is a startup gate, not a full schema check:

- `server.port` and `server.sftp_port` in 1–65535.
- Every key has a non-empty `key` and a role of `readonly` or `admin`.
- External bind (`isExternal(host)`: anything other than `localhost`,
  `127.0.0.1`, `::1`):
  - at least one key;
  - no placeholder value (`change-me`, `secret`, `password`, …);
  - no key shorter than 12 characters;
  - TLS cert and key must be configured together.
- A **missing** TLS pair on an external bind is not an error; it is reported by
  `ExternalPlainHTTP()` and warned about in `main.go` (`K-03`). Do not describe
  TLS as enforced.

## Key helpers

- `Authorize(bearer)` → role, ok. SHA-256 + `subtle.ConstantTimeCompare`.
- `KeyName(bearer)` → configured key name, ok. Same comparison; used for rate
  limiting and audit.
- `Role.AdminOnly()` is the only role predicate.
- The comparison loops over all keys; there is no early return, so an observer
  cannot learn the key index from timing.

## Changing config

1. Add or change the struct field.
2. Add the parser case (and the array-entry rule if it is a repeated table).
3. Add the default, the `config.example.toml` entry, and the `README.md` table.
4. Add a validation rule only if starting with the value is unsafe; otherwise
   warn and document.
5. Test: parse, default, validation failure, and the `Authorize`/`KeyName`
   boundary.
6. If the key is security-relevant, update
   [`docs/architecture/security.md`](../../docs/architecture/security.md).

## Verification

```sh
gofmt -l internal/config
go vet ./internal/config
go test -race ./internal/config -count=1
```

Parser behavior is platform-independent, so this package can be fully verified
on Windows. Runtime binding and TLS are not: check those on Linux.
