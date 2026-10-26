# internal/tasks — async jobs

Read the root [`AGENTS.md`](../../AGENTS.md) first. The storage side is in
[`docs/architecture/state-and-storage.md`](../../docs/architecture/state-and-storage.md),
and the HTTP surface is in
[`docs/architecture/api-and-auth.md`](../../docs/architecture/api-and-auth.md).

## Shape

`Manager` owns a map of `*Task`, a cancellation map, a map of re-runnable jobs,
and an optional JSON persistence file. It is created once as `taskMgr` in
`internal/server/tasks.go` with a 1-hour TTL and
`WINGS_DATA_DIR/wings-tasks.json`.

Lifecycle:

```
queued ──▶ running ──▶ succeeded
                    ├─▶ failed
                    └─▶ canceled
```

`Status` is a string type; the JSON values (`queued`, `running`, `succeeded`,
`failed`, `canceled`) are a contract with the panel.

## API

| Method | Meaning |
|---|---|
| `Submit(kind, fn)` | Run `fn(ctx) (output, error)` in a goroutine; returns the ID immediately |
| `SubmitLines(kind, fn)` | Same, with an `onLine` callback appended to `Progress` |
| `Cancel(id)` | Mark canceled and cancel the context; returns false for unknown/finished tasks |
| `Retry(id)` | Re-run the in-memory job function under a new ID; returns false after a restart |
| `Get(id)` / `List()` | Copies of tasks; `List` is newest first by `Seq` |
| `Prune()` | Drop finished tasks older than the TTL and rewrite the file |
| `OnComplete(fn)` | Callback for every terminal state (used by webhooks) |

`Task.Seq` is `json:"-"` and orders `List`; it is not part of the API shape.
`Output` and `Progress` are persisted/returned verbatim.

## Concurrency rules

- `saveLocked` and every map access happen under `m.mu`. Do not call a
  user-provided callback while holding the lock — `finish` and `Cancel` copy the
  task and invoke `OnComplete` after unlocking for this reason.
- `Progress` append takes the lock. Keep the callback in the job short; a slow
  consumer must not hold the manager lock.
- `finish` checks `StatusCanceled` first: `Cancel` owns the terminal state and
  the callback in that case. Do not "fix" this by letting the job overwrite a
  cancel.
- `Retry` only works while the `jobFunc` is in memory. After a restart, loaded
  tasks can be listed but not retried; return 501/409 rather than pretending.
- There is no cross-process lock. Two daemons sharing `WINGS_DATA_DIR` will
  overwrite the file.

## Memory and disk

- `Progress` and `Output` have no size cap. A chatty job (`image pull`, install)
  can grow the task and the JSON file without bound. If you add a cap, truncate
  with a visible marker and keep the last lines.
- Finished tasks are only removed by `Prune`, which runs on `GET /v1/tasks`.
  A daemon that is never polled keeps tasks in memory until restart.
- `saveLocked` writes the file with `0600` and ignores write errors. A full disk
  silently loses persistence; do not claim the task file is durable.

## Changing this package

1. Add a test in `tasks_test.go` for the success path, cancellation, and any
   status transition you touch.
2. Keep the JSON field names stable. The panel reads `id`, `kind`, `status`,
   `created_at`, `started_at`, `finished_at`, `error`, `output`, `progress`.
3. If a new task kind is exposed over HTTP, mount it in
   `internal/server/tasks.go` and document it in `docs/api.md` +
   `docs/openapi.yaml`.
4. If completion should notify a panel, use the existing `OnComplete` hook in
   `internal/server/webhooks.go`; do not call the notifier from this package.

## Verification

```sh
gofmt -l internal/tasks
go vet ./internal/tasks
go test -race ./internal/tasks -count=1
go test -race ./internal/server -run 'Task' -count=1
```

The race detector matters here: the manager is shared by every request and by
background goroutines. A test that passes without `-race` is not sufficient
evidence.
