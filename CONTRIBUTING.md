# Contributors

Thank you to everyone who helps improve **cardinal-wings**.

## Maintainers

- [animesao](https://github.com/animesao) — project maintainer and primary author.

## How to contribute

Contributions are welcome:

1. Fork the repository and create a focused branch.
2. Make the smallest change that solves the problem.
3. Add or update tests for code changes.
4. Run the local checks before opening a pull request:

```bash
gofmt -l .          # must be empty
go vet ./...        # must pass
go test -race ./... # must pass
bash -n install.sh  # installer syntax
```

5. Open a pull request against `main`.

## Project layout

- `internal/server/` — HTTP daemon: routing, middleware, handlers, node registry.
- `internal/runtime/` — HTTP client for cardinal's Docker-compatible API.
- `internal/agent/` — delegating to the `cardinal` CLI (exec, pull, services, fs).
- `internal/auth/` — bearer auth, roles, rate limiting, CORS.
- `internal/config/` — dependency-free TOML subset parser.
- `internal/tasks/` — async job manager with persistence.
- `internal/ws/` — minimal RFC 6455 websocket server (stdlib only).
- `internal/webhooks/` — webhook notifications.
- `docs/` — API reference, OpenAPI schema, cluster guide.

## Design rules

- **No new external dependencies** unless strictly necessary. wings is
  deliberately stdlib-only (the websocket server is hand-rolled for this
  reason).
- **Everything goes through cardinal**, never around it: wings talks to
  `cardinal serve` over HTTP or delegates to the CLI. Do not reimplement
  container/image logic.
- **Uniform errors**: every endpoint returns
  `{"error":{"code":"…","message":"…"}}`.
- **Node-aware**: resource endpoints must accept `?node=<name>`.
- Keep the OpenAPI schema (`docs/openapi.yaml`) in sync when adding endpoints.

## Licensing

MIT. Contributor attribution is maintained here and in git history.
