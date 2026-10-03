package server

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// craftZip builds an in-memory zip from name→content pairs (test only).
func craftZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestZipDirRoundTrip(t *testing.T) {
	src := t.TempDir()
	writeTestFile(t, filepath.Join(src, "a.txt"), "hello")
	writeTestFile(t, filepath.Join(src, "sub", "b.txt"), "world")

	data, err := zipDir(src)
	if err != nil {
		t.Fatalf("zipDir: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("zipDir returned empty archive")
	}

	dst := t.TempDir()
	if err := unzipInto(data, dst); err != nil {
		t.Fatalf("unzipInto: %v", err)
	}
	for _, tc := range []struct{ rel, want string }{
		{"a.txt", "hello"},
		{filepath.Join("sub", "b.txt"), "world"},
	} {
		got, err := os.ReadFile(filepath.Join(dst, tc.rel))
		if err != nil {
			t.Fatalf("read %s: %v", tc.rel, err)
		}
		if string(got) != tc.want {
			t.Errorf("%s = %q, want %q", tc.rel, got, tc.want)
		}
	}
}

func TestUnzipRejectsZipSlip(t *testing.T) {
	src := t.TempDir()
	writeTestFile(t, filepath.Join(src, "ok.txt"), "ok")
	data, err := zipDir(src)
	if err != nil {
		t.Fatal(err)
	}
	// Sanity: a good archive extracts fine.
	if err := unzipInto(data, t.TempDir()); err != nil {
		t.Fatalf("good archive rejected: %v", err)
	}

	bad := craftZip(t, map[string]string{"../evil.txt": "x"})
	if err := unzipInto(bad, t.TempDir()); err == nil {
		t.Error("zip-slip entry ../evil.txt accepted, want error")
	}
	abs := craftZip(t, map[string]string{"/abs.txt": "x"})
	if err := unzipInto(abs, t.TempDir()); err == nil {
		t.Error("absolute entry /abs.txt accepted, want error")
	}
}
