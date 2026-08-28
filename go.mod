module github.com/animesao/cardinal-wings

go 1.26

// wings talks to cardinal over its Docker-compatible HTTP API (`cardinal
// serve`) rather than importing cardinal's internals, so no `replace` is
// needed here.

require github.com/creack/pty v1.1.24 // indirect
