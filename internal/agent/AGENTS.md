# internal/agent — cardinal CLI delegation and local node

Read the root [`AGENTS.md`](../../AGENTS.md) first. The process/runtime picture
is in [`docs/architecture/runtime.md`](../../docs/architecture/runtime.md).

## Shape

This package is the only place that shells out to the `cardinal` binary or
builds runtime clients for nodes.

| File | Responsibility |
|---|---|
| `local.go` | Allocate a loopback port, generate a token, spawn `cardinal serve`, wait for readiness |
| `remote.go` | Build `runtime.Client`s for enabled `[[nodes]]` |
| `exec.go` | Exec/attach/raw exec into a container via `cardinal exec` |
| `fs.go` | `cardinal fs` / `cp` helpers |
| `pull.go` | Image pull with progress lines |
| `ops.go` | Ports, commit, verify, `system df/prune`, service inspect/update, cluster info |
| `services.go` | `cardinal service` and `fn` operations |
| `blueprint.go` | Blueprint install/uninstall, bootstrap ensure |
| `netvol.go` | Network and volume CLI wrappers |
| `events.go` | Event stream |

It does **not** import cardinal's Go packages. The module boundary is
deliberate (`go.mod` has no `replace`): wings talks HTTP for everything that
cardinal exposes, and uses the CLI only where `serve` has no endpoint.

## The local node

`StartLocal`:

- `freePort()` listens on `127.0.0.1:0`, reads the port, closes it. There is a
  small race between close and cardinal's bind; the readiness poll absorbs it.
- `randomToken()` is 24 random bytes, hex-encoded, per boot. Never logged and
  never written to disk.
- Runs `cardinal serve -H 127.0.0.1 -p <port> --token <token> -d` and also sets
  `CARDINAL_TOKEN` on the child as a fallback for cardinal's flag handling.
- Polls `/_ping` every 250 ms with a 500 ms per-request timeout, up to 15 s.
- On timeout, kills the command it started and returns an error. `server.Run`
  logs the error and continues in degraded mode.

**Known limitation:** `-d` makes cardinal re-exec the daemon and exit, so
`Local.cmd` is the launcher, not the daemon. `Stop()` kills the launcher; the
actual `serve` process keeps running. See `K-09` in
[`docs/status/known-issues.md`](../../docs/status/known-issues.md). Do not copy
this pattern into new code, and do not assume a wings restart cleaned up the
previous local cardinal.

## Invocation rules

- `buildCardinalCmd` resolves `cardinal` through `PATH`. There is no hard-coded
  absolute path and no download step; deployment must provide the binary.
- Always pass a context with a deadline. Container operations can hang on a
  broken overlay or a stuck image pull.
- `runCardinalOut` combines stdout and stderr into the error message and
  returns trimmed stdout on success. Callers that parse output (CLI has no
  JSON mode everywhere) are coupled to cardinal's human-readable format; if a
  cardinal release changes wording, the wings tests will not catch it. Prefer a
  `serve` endpoint when one exists.
- `RawExec` streams stdout and returns a `wait` function. Call `wait` even if
  you stop reading early, or the child can block on a full pipe.
- Arguments are passed as an argv slice where possible. Do not build a shell
  string from user input. When a command must go through `/bin/sh -c`, quote
  with the existing helper and keep the value out of the format string.
- Never put a token in the argv of a logged command or in an error message.

## Changing this package

1. Check whether `cardinal serve` already exposes the operation. If it does,
   add a `runtime.Client` method instead of a CLI wrapper — the HTTP path is
   the one the local and remote nodes share.
2. Keep one function per command shape, with the argv builder separated
   (`pullArgs`, `networkCreateArgs`, …) so it can be unit-tested.
3. Add a test that asserts argv, not just the error path. `agent_test.go`,
   `netvol_test.go` are the model.
4. Update `docs/architecture/runtime.md` if startup or node behavior changed.

## Verification

```sh
gofmt -l internal/agent
go vet ./internal/agent
go test -race ./internal/agent -count=1
```

The unit tests run anywhere; they do not prove that the real CLI accepts the
arguments. A change to an argv builder needs a manual run on Linux with a real
`cardinal` binary, or the e2e workflow.
