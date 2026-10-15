package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pkg/sftp"
)

func symlinkCapable(t *testing.T, dir string) {
	t.Helper()
	probe := filepath.Join(dir, "probe-link")
	if err := os.Symlink("target", probe); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_ = os.Remove(probe)
}

func TestJailLexicalEscape(t *testing.T) {
	root := t.TempDir()
	fs := jailFS{root: root}
	if p, err := fs.resolve(".."); err != nil || p != root {
		t.Fatalf("resolve(..) = %q, %v; want jail root", p, err)
	}
	for _, p := range []string{"/../x", "a/../../x"} {
		if _, err := fs.resolve(p); err == nil {
			t.Fatalf("resolve(%q) escaped", p)
		}
	}
	if _, err := fs.resolve(""); err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	if _, err := fs.resolve("sub/dir"); err == nil {
		// Missing path without allowMissing: EvalSymlinks fails — must not resolve.
		t.Fatal("resolve(missing) unexpectedly succeeded")
	}
	if _, err := fs.resolveAllowMissing("sub/dir", true); err != nil {
		t.Fatalf("resolveAllowMissing(nested): %v", err)
	}
}

func TestJailEscapingSymlinkBlocked(t *testing.T) {
	root := t.TempDir()
	symlinkCapable(t, root)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	fs := jailFS{root: root}
	if _, err := fs.resolve("link"); err == nil {
		t.Fatal("escaping symlink resolved")
	}
	if _, err := fs.resolveAllowMissing("link", true); err == nil {
		t.Fatal("escaping symlink resolved with allowMissing")
	}
}

func TestJailInnerSymlinkAllowed(t *testing.T) {
	root := t.TempDir()
	symlinkCapable(t, root)
	if err := os.WriteFile(filepath.Join(root, "inner.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("inner.txt", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	fs := jailFS{root: root}
	if _, err := fs.resolve("alias"); err != nil {
		t.Fatalf("inner symlink blocked: %v", err)
	}
}

func TestJailSymlinkStoresVerbatim(t *testing.T) {
	root := t.TempDir()
	symlinkCapable(t, root)
	fs := jailFS{root: root}
	// Correct pkg/sftp semantics: Filepath = link to create,
	// Target = link content. Only relative in-jail targets pass.
	r := &sftp.Request{Method: "Symlink", Filepath: "/alias", Target: "rel-target"}
	if err := fs.Filecmd(r); err != nil {
		t.Fatalf("Filecmd symlink: %v", err)
	}
	got, err := os.Readlink(filepath.Join(root, "alias"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "rel-target" {
		t.Fatalf("link content = %q, want verbatim rel-target", got)
	}
}

func TestJailSymlinkRejectsEscape(t *testing.T) {
	root := t.TempDir()
	symlinkCapable(t, root)
	fs := jailFS{root: root}
	for _, tc := range []struct{ filepath, target string }{
		{"/alias", "/etc/passwd"},
		{"/alias", "../outside"},
		{"/alias", "sub/../../outside"},
		{"/alias", ""},
		{"/", "rel-target"},
	} {
		r := &sftp.Request{Method: "Symlink", Filepath: tc.filepath, Target: tc.target}
		if err := fs.Filecmd(r); err == nil {
			t.Errorf("escaping symlink %+v accepted", tc)
		}
	}
}

func TestJailMkdirNested(t *testing.T) {
	root := t.TempDir()
	fs := jailFS{root: root}
	r := &sftp.Request{Method: "Mkdir", Filepath: "/newdir"}
	if err := fs.Filecmd(r); err != nil {
		t.Fatalf("Filecmd mkdir: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(root, "newdir")); err != nil || !fi.IsDir() {
		t.Fatalf("nested dir not created: %v", err)
	}
}
