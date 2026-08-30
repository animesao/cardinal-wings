package server

import (
	"archive/tar"
	"bytes"
	"testing"
)

func TestValidateTarEntry(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "a/../../escape", `..\\escape`} {
		if err := validateTarEntry(name); err == nil {
			t.Errorf("expected %q to be rejected", name)
		}
	}
	for _, name := range []string{"save/world.dat", "./config.yml", "nested/file.txt"} {
		if err := validateTarEntry(name); err != nil {
			t.Errorf("expected %q to be accepted: %v", name, err)
		}
	}
}

func TestValidateTarStream(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: "../escape", Mode: 0600, Size: 0})
	_ = tw.Close()
	if err := validateTarStream(&buf); err == nil {
		t.Fatal("expected unsafe tar stream to be rejected")
	}
}
