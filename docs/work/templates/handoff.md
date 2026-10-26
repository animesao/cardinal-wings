# Handoff: <short title>

- **Task:** <link to task record or issue>
- **Status:** `Implemented` | `Partial` | `Blocked` | `Unknown`
- **Date:** <YYYY-MM-DD>
- **Commit:** `<sha>` or "uncommitted worktree"
- **Agent / author:** <name>

## What changed

| File | Change |
|---|---|
| `<path>` | <one line> |

Do not list every file. List the files a next session must read first.

## Why

One short paragraph. Link the architecture page if durable behavior changed.

## Verification actually run

Copy the exact command and the observed result. Do not summarize a failure away.

| Command | Result |
|---|---|
| `<command>` | <exit code and key output> |

## Verification not run

| Check | Reason | How to run it |
|---|---|---|
| `<check>` | <no Linux host, no root, no network> | `<command>` |

## Remaining work

- <next concrete step>

## Risks and drift

- <risk or new known issue, with the `K-NN` entry if one was added>

## Notes for the next session

- <what to read first>
- <what not to retry>
- <environment quirks, e.g. "Windows host: no bash, no Linux runtime">
