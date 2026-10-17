package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoopDevicesAvailableIn(t *testing.T) {
	devDir := t.TempDir()
	if loopDevicesAvailableIn(devDir) {
		t.Fatalf("empty dev dir reported loop devices")
	}
	if err := os.WriteFile(filepath.Join(devDir, "loop3"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if !loopDevicesAvailableIn(devDir) {
		t.Fatalf("pre-created loop3 was not detected")
	}
	if err := os.WriteFile(filepath.Join(devDir, "loop-control"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if !loopDevicesAvailableIn(devDir) {
		t.Fatalf("loop-control was not detected")
	}
}
