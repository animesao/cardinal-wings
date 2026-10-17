package server

import (
	"fmt"
	"os"
	"path/filepath"
)

// loopDevicesAvailable reports whether the host can back a loop mount at all:
// either the loop-control node or at least one pre-created /dev/loopN exists.
// Container-type VDS hosts (LXC/OpenVZ) provide neither, and cardinal itself
// runs containers there without a disk quota.
func loopDevicesAvailable() bool {
	return loopDevicesAvailableIn("/dev")
}

func loopDevicesAvailableIn(devDir string) bool {
	if _, err := os.Stat(filepath.Join(devDir, "loop-control")); err == nil {
		return true
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(devDir, fmt.Sprintf("loop%d", i))); err == nil {
			return true
		}
	}
	return false
}
