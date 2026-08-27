//go:build linux

package server

import "syscall"

// diskTotalBytes returns the total size of the filesystem containing path, in
// bytes. Needed so /v1/system/info can report the node's real disk so the panel
// shows actual VPS capacity instead of zeros. wings runs as root on the host,
// so statfs of "/" is cheap and reliable.
func diskTotalBytes() int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err != nil {
		return 0
	}
	return int64(st.Blocks) * int64(st.Bsize)
}
