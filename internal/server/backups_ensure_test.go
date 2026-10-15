package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDataRootMounted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CARDINAL_DATA_DIR", dir)
	data := filepath.Join(dir, "overlay", "ctr1", "merged", "data")
	if err := os.MkdirAll(data, 0755); err != nil {
		t.Fatal(err)
	}
	release, root := ensureDataRoot("ctr1")
	defer release()
	if root != data {
		t.Fatalf("root = %q, want %q", root, data)
	}
}

func TestEnsureDataRootMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CARDINAL_DATA_DIR", dir)
	release, root := ensureDataRoot("nope-missing")
	defer release()
	if root != "" {
		t.Fatalf("root = %q, want empty", root)
	}
}
