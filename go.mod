module github.com/animesao/cardinal-wings

go 1.26

// wings talks to cardinal over its Docker-compatible HTTP API (`cardinal
// serve`) rather than importing cardinal's internals, so no `replace` is
// needed here.

require (
	github.com/pkg/sftp v1.13.11
	golang.org/x/crypto v0.55.0
)

require (
	github.com/kr/fs v0.1.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)
