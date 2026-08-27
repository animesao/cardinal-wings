//go:build !linux

package server

// diskTotalBytes returns 0 on platforms where statfs isn't wired up. Linux
// (the deployed target) returns the real total on the root filesystem.
func diskTotalBytes() int64 {
	return 0
}
