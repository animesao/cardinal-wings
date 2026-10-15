package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
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

// buildTarGz crafts an in-memory tar.gz from headers+bodies for tests.
func buildTarGz(t *testing.T, entries []tar.Header, bodies map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range entries {
		h := h
		body := bodies[h.Name]
		h.Size = int64(len(body))
		if h.Typeflag == 0 {
			h.Typeflag = tar.TypeReg
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg || h.Typeflag == tar.TypeRegA {
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractTarGzBlocksTraversal(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "a/../../escape", "ok/../../escape"} {
		data := buildTarGz(t, []tar.Header{{Name: name, Mode: 0644}}, map[string]string{name: "x"})
		dir := t.TempDir()
		if err := extractTarGz(dir, bytes.NewReader(data)); err == nil {
			t.Errorf("expected %q to be rejected", name)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape")); !os.IsNotExist(err) {
			t.Errorf("escape file created for %q", name)
		}
	}
}

func TestExtractTarGzKeepsLegitFiles(t *testing.T) {
	data := buildTarGz(t,
		[]tar.Header{{Name: "data", Typeflag: tar.TypeDir, Mode: 0755}, {Name: "data/start.sh", Mode: 0755}, {Name: "eula.txt", Mode: 0644}},
		map[string]string{"data/start.sh": "#!/bin/sh\n", "eula.txt": "eula=true\n"})
	dir := t.TempDir()
	if err := extractTarGz(dir, bytes.NewReader(data)); err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, f := range []string{"data/start.sh", "eula.txt"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	if fi, err := os.Stat(filepath.Join(dir, "data", "start.sh")); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && fi.Mode().Perm() != 0755 {
		// Windows Chmod only honors the read-only bit; exec-bit
		// preservation is verified on Linux CI.
		t.Errorf("exec bit lost: %o", fi.Mode().Perm())
	}
}

func TestExtractTarGzSkipsUnsafeLinks(t *testing.T) {
	data := buildTarGz(t,
		[]tar.Header{
			{Name: "good.txt", Mode: 0644},
			{Name: "ok-link", Typeflag: tar.TypeSymlink, Linkname: "good.txt"},
			{Name: "evil-link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
			{Name: "up-link", Typeflag: tar.TypeSymlink, Linkname: "../outside"},
			{Name: "hard", Typeflag: tar.TypeLink, Linkname: "good.txt"},
			{Name: "devnull", Typeflag: tar.TypeChar, Devmajor: 1, Devminor: 3},
		},
		map[string]string{"good.txt": "ok"})
	dir := t.TempDir()
	if err := extractTarGz(dir, bytes.NewReader(data)); err != nil {
		t.Fatalf("extract: %v", err)
	}
	// In-root symlink is recreated and resolves inside the jail.
	if target, err := os.Readlink(filepath.Join(dir, "ok-link")); err != nil || target != "good.txt" {
		t.Errorf("ok-link = %q, %v; want good.txt", target, err)
	}
	for _, f := range []string{"evil-link", "up-link", "hard", "devnull"} {
		if _, err := os.Lstat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("unsafe entry %s materialized", f)
		}
	}
}
