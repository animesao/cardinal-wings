module github.com/animesao/cardinal-wings

go 1.26.0

// 1.26.9 is the first release carrying the net/http, net/textproto and
// crypto/tls fixes for GO-2026-6603/6605/6607/6608/6610/6611/6612/6613/6617.
// The `go` line stays at 1.26.0 so the language version does not move; this
// only raises the *toolchain* floor so a default GOTOOLCHAIN=auto build on
// 1.26.0-1.26.8 self-upgrades instead of silently linking a vulnerable
// standard library into the released binary.
toolchain go1.26.9

// wings talks to cardinal over its Docker-compatible HTTP API (`cardinal
// serve`) rather than importing cardinal's internals, so no `replace` is
// needed here.

require (
	github.com/pkg/sftp v1.13.11
	golang.org/x/crypto v0.57.0
)

require (
	github.com/kr/fs v0.1.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)
