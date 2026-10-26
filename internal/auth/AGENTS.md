# internal/auth — keys, roles, rate limits, CORS

Read the root [`AGENTS.md`](../../AGENTS.md) first. The behavioral contract is
in [`docs/architecture/api-and-auth.md`](../../docs/architecture/api-and-auth.md)
and the controls are summarized in
[`docs/architecture/security.md`](../../docs/architecture/security.md).

## Shape

- `auth.go` — `Middleware`, `New`, `Authenticate`, `AdminOnly`, the token
  buckets (`RateLimitIP`, `RateLimitKey`), and `CORS`.
- `context.go` — `RoleFrom`, key-name helpers, and the request-context keys.
- `auth_test.go` — the executable specification: 401 missing, 403 invalid,
  403 readonly-on-admin, 429 over limit, and the CORS preflight case.

`Middleware` only holds a `*config.Config` and the limiter maps. It is
constructed once in `server.Run` and shared by every route.

## Middleware order

`server.Run` owns the order: CORS → body limit → per-IP limit → auth → per-key
limit → audit. `auth.go` has comments describing an older order; `server.go`
wins.

- Per-IP runs before auth so unauthenticated floods are cheap. Loopback is
  exempt.
- Per-key runs **after** auth so buckets are keyed by the configured key name.
  The limiter has a fallback that keys by the raw header when no name is in the
  context (the comment in `auth.go` still claims it runs before auth). The
  `max_clients` ceiling is what keeps that fallback bounded; do not remove it,
  and do not move the middleware back.

## Authentication rules

- `Authorization: Bearer <key>` only, with the exact prefix; anything else is
  treated as missing.
- `?token=` is accepted **only** when `isWebSocketUpgrade(r)` is true
  (`Upgrade: websocket` + `Connection` containing `upgrade`). Do not extend it.
- `config.Authorize` compares SHA-256 digests with `subtle.ConstantTimeCompare`.
  Do not replace it with `==` on the raw strings.
- Missing credential → 401. Unknown key → 403. `AdminOnly` + readonly → 403.
- Errors from this package are `{"error":"<string>"}`, not the `writeErr`
  envelope. That is a known inconsistency (`K-01`); do not add a third shape.
- Identity is put in the request context; handlers read it with `RoleFrom` and
  `KeyNameFrom`. Do not re-parse the header in a handler.

## Rate limiting rules

- Two token buckets: per-IP (default 25/s, burst 50, loopback exempt) and
  per-key (default 10/s, burst 30, applies to loopback too).
- Bucket maps are pruned; there is a hard ceiling (`max_clients`, default
  4096; the key map is allowed up to 2× before returning 429). Preserve both
  the prune and the ceiling when touching the maps; without them a hostile
  client can exhaust memory.
- TPS values are floats (`ip_tps`, `key_tps`) and allow sub-1 rates; bursts are
  ints.
- A rejected request returns 429 and the plain-string error shape.

## CORS

`CORS` handles preflight with 204 and sets no `Access-Control-Allow-Origin`.
There is no origin allowlist in config, despite the comment. The current
behavior is fail-closed for cross-origin browsers (`K-02`). If you add an
allowlist, make it explicit in `config.example.toml`, `README.md`, and
`docs/architecture/security.md`, and keep the default empty.

## Verification

```sh
gofmt -l internal/auth
go vet ./internal/auth
go test -race ./internal/auth -count=1
go test -race ./internal/server -run 'Auth|JWT|Rate|CORS' -count=1
```

For any change here, also run the e2e workflow or a manual request against a
real daemon: the unit tests use `httptest` and cannot prove that a proxy or the
real mux orders the middleware the same way.
