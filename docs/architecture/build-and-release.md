# Build, CI, and release

This page covers how a wings binary is produced, how CI gates it, and what a
release actually contains. The installer's runtime behavior is documented here
too because it is part of the release contract.

The documentation rules (what to bump before a tag) are in
[`../AGENTS.md`](../AGENTS.md).

## Toolchain and dependencies

`go.mod`:

```
module github.com/animesao/cardinal-wings
go 1.26.0
toolchain go1.26.10
```

- The `go` line is the **language** version and stays at `1.26.0`.
- The `toolchain` line is a **floor**: a default `GOTOOLCHAIN=auto` build on
  1.26.0–1.26.9 self-upgrades instead of linking a standard library with known
  CVEs. The comment in `go.mod` names the advisories (`GO-2026-6809` and the
  1.26.9 batch).
- Every CI job pins `go-version: "1.26.10"`. Keep the jobs and `go.mod` in
  sync; a job on a different version can pass while the released binary is
  built with a vulnerable standard library (or vice versa).
- Dependencies are deliberately small: `github.com/pkg/sftp` and
  `golang.org/x/crypto` for the built-in SFTP server, plus their indirect
  `kr/fs` and `x/sys`. The WebSocket implementation is hand-rolled
  (`internal/ws`). `CONTRIBUTING.md` still calls the project "stdlib-only";
  that is drift (`K-06`).

## Local build

```sh
make build      # go build -trimpath -ldflags="-s -w" -o cardinal-wings .
make test       # go test ./...
make vet        # go vet ./...
make lint       # golangci-lint run (optional; prints a note if missing)
make fmt        # gofmt -w .
make install    # build + install -m 0755 to /usr/local/bin
```

`make build` does **not** stamp a version: `internal/server.version` stays at
its default `dev`, and `GET /v1/version` reports `dev`. That is intentional for
local builds; only the release job passes `-X`.

Manual equivalent with a version:

```sh
CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w -buildid= -X github.com/animesao/cardinal-wings/internal/server.version=v0.5.20" \
  -o cardinal-wings .
```

`CGO_ENABLED=0` is what CI uses; the released binaries are static. The project
targets Linux for runtime behavior. A Windows or macOS checkout can compile and
run unit tests, but the daemon exercises Linux-only paths (procfs, mountinfo,
overlay roots) and cannot manage containers.

## Version stamping

- `internal/server/version` is a package variable with the default `dev`.
- CI passes `-X github.com/animesao/cardinal-wings/internal/server.version=<tag>`
  on a tag build, `dev` otherwise.
- `GET /v1/version` returns the stamped value.
- `docs/openapi.yaml` `info.version` is **not** generated from the binary; it is
  a separate release gate. On a `v*` tag push, the `openapi-version` job
  extracts the first `  version:` line and fails if it does not equal the tag
  without the leading `v`. Bump it before tagging, not after.

## CI workflows

### `build.yml`

Triggers: push to `main`, push of a `v*` tag, pull request to `main`, and
`workflow_dispatch`. Concurrency is per-ref; superseded PR runs are canceled.
Permissions: `contents: write` (only the release job needs it).

| Job | What it proves |
|---|---|
| `openapi-version` | On a tag push, `docs/openapi.yaml` `info.version` equals the tag. No-op on branch/PR runs. |
| `lint` | `gofmt -l .` is empty; `go vet ./...`; `govulncheck ./...`; full-history checkout + `gitleaks`; `npx @redocly/cli lint docs/openapi.yaml`. |
| `test` | `go test -race ./...`. |
| `build` | Matrix `linux/amd64` and `linux/arm64`, `CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w -buildid= -X ...version=<tag>"`, per-asset `.sha256`, installer copied into `dist/` for tagged amd64 builds. |
| `release` | Tagged `v*` only. Downloads merged artifacts, builds notes from the previous tag by creation date plus `.github/release-body.md`, and publishes with `softprops/action-gh-release`. |

Notes:

- `govulncheck` is installed with `@latest`, and OpenAPI lint uses
  `npx --yes @redocly/cli` (also latest). Both can change under a green build;
  when a failure is unexplained, check the tool version before the code.
- The release notes list the **last 10 commits** between the previous tag and
  the new one. They are a summary, not a changelog.
- `gitleaks` needs full history; the job re-checks out with `fetch-depth: 0`
  before running it. A shallow clone would scan almost nothing.

### `e2e.yml`

Triggers: push/PR to `main` and `workflow_dispatch`. Timeout 20 minutes. It:

1. checks out wings and the sibling `animesao/cardinal` repository;
2. builds `cardinal` and wings with Go 1.26.10;
3. writes a loopback config with one admin key;
4. starts wings with `WINGS_DATA_DIR=/tmp/wings-data` and waits for
   `/v1/ping`;
5. asserts `/v1/ping`, `/v1/version`, `/v1/self`, `/v1/nodes`,
   `/v1/system/info`, `/v1/images`, `/v1/blueprints`, `/v1/tasks`, a 401
   without a key, a 404 for an unknown node, and the `not_found` error
   envelope.

It does **not** create containers or exec into them: that needs privileges. The
workflow is a startup/auth/routing regression gate, not a container-runtime
test. Do not describe a green e2e as proof that container operations work.

## Release artifacts

A `v*` tag produces:

```
cardinal-wings-linux-amd64
cardinal-wings-linux-amd64.sha256
cardinal-wings-linux-arm64
cardinal-wings-linux-arm64.sha256
install.sh                    # tagged amd64 row only
install.sh.sha256
```

- Checksums are `sha256sum` format (`<hash>  <file>`), generated inside
  `dist/`, so `sha256sum -c` works after downloading both files.
- The release body is generated; `.github/release-body.md` is the static part.
- The GitHub release is the only distribution channel. `install.sh` resolves
  `latest` through the GitHub API or an optional `WINGS_MIRROR`.

## Release procedure

1. Confirm the intended version. There is no `VERSION` file and no changelog;
   the tag is the version.
2. Set `info.version` in `docs/openapi.yaml` to the tag without `v`.
3. Update `docs/api.md` / `docs/openapi.yaml` for any endpoint change; update
   the relevant architecture page if durable behavior changed.
4. Run the local gates (below), plus the OpenAPI lint if the schema changed.
5. Commit, tag `vX.Y.Z`, push the branch and the tag.
6. Watch `openapi-version` → `lint` → `test` → `build` → `release`. A failure
   in `openapi-version` means the spec value and the tag disagree; fix the spec
   and move the tag deliberately, or cut a new one.
7. Verify the release page: four binaries, four checksums, installer and its
   checksum, notes pointing at the previous tag.
8. Install the new tag on a test host (`install.sh vX.Y.Z` or the mirror) and
   check `GET /v1/version` matches.

Do not tag from a dirty tree, and do not move a published tag. The installer
verifies the asset checksum, but the tag-to-commit mapping is what users trust.

## Installer

`install.sh` is a POSIX-ish bash script that requires root and a prebuilt
release (or `local` to build from a checkout with Go). It detects
`linux`/`darwin` and `amd64`/`arm64`, resolves the version, downloads,
verifies SHA-256, writes config and the systemd unit, enables the service, and
optionally opens firewall ports.

| Variable | Default | Effect |
|---|---|---|
| `WINGS_OWNER` | `animesao` | GitHub owner for release URLs |
| `WINGS_HOST` | `0.0.0.0` | API bind address written to config |
| `WINGS_PORT` | `8080` | API port |
| `WINGS_TLS` | `0` | `1` generates a self-signed pair and enables TLS |
| `WINGS_SFTP` | `1` | enable the per-container SFTP server |
| `WINGS_SFTP_HOST` | `0.0.0.0` | SFTP bind address |
| `WINGS_SFTP_PORT` | `2022` | SFTP port |
| `WINGS_SYSTEMD_URL` | empty | custom unit URL; must be `https://` because the unit runs as root |
| `WINGS_NO_FIREWALL` | `0` | skip automatic `ufw`/`firewalld`/`iptables` opening |
| `WINGS_UPDATE_CARDINAL` | `0` | also install/update the `cardinal` runtime |
| `WINGS_MIRROR` | empty | self-hosted mirror base; falls back to GitHub |
| `WINGS_SKIP_VERIFY` | `0` | skip SHA-256 verification (debug only) |
| `WINGS_DATA_DIR` | `/var/lib/cardinal-wings` | state directory, also baked into the unit |

Positional argument: `latest` (default), a specific `vX.Y.Z`, or `local`.
`CARDINAL_VERSION` can pin the runtime version when
`WINGS_UPDATE_CARDINAL=1`.

What it writes:

- binary `0755` to `${PREFIX}/bin/cardinal-wings`;
- config `0600` in `/etc/cardinal-wings` (`0700`) with a generated admin key;
- TLS key/cert `0600` only when `WINGS_TLS=1`;
- data directory `0700`, `WINGS_DATA_DIR` and `CARDINAL_DATA_DIR` in the unit;
- `/etc/systemd/system/cardinal-wings.service`, enabled and started.

Security-relevant installer behavior:

- `WINGS_SKIP_VERIFY=1` disables the checksum check. It exists to debug a
  mirror; it is not a production option.
- `WINGS_UPDATE_CARDINAL=1` downloads cardinal's `main` branch installer and
  runs it as root without pinning or a checksum (`K-07`).
- The outer installer verifies the **binary** against the published `.sha256`;
  it does not sign anything. Trust is GitHub plus TLS plus the tag.

Known gap: the script accepts `darwin` and constructs
`cardinal-wings-darwin-${ARCH}`, but the build matrix only publishes Linux
assets. A macOS install therefore fails at download (`K-11`).

## Gates before a release

```sh
gofmt -l .                 # must be empty
go vet ./...               # must pass
go test -race ./...        # must pass
bash -n install.sh         # syntax; shellcheck when available
npx --yes @redocly/cli lint docs/openapi.yaml   # after schema edits
```

On Windows without bash, run the Go gates in PowerShell and state that
`bash -n`/`shellcheck` and the Linux e2e were not run. Compilation is not
runtime verification.

## Evidence

| Claim | Evidence |
|---|---|
| Language/toolchain floor | `go.mod` |
| Pinned CI Go version | `.github/workflows/build.yml`, `.github/workflows/e2e.yml` |
| Job graph and gates | `.github/workflows/build.yml` |
| E2E scope | `.github/workflows/e2e.yml` |
| Version injection | `build.yml` (`-X ...internal/server.version`), `internal/server/system.go` |
| OpenAPI gate | `build.yml` (`openapi-version`) |
| Artifact names and checksums | `build.yml` (build + release jobs) |
| Release notes | `build.yml`, `.github/release-body.md` |
| Installer behavior | `install.sh`, `systemd/cardinal-wings.service` |
| Local targets | `Makefile` |
