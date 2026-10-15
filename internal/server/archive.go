package server

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Caps keep a hostile or absurd archive from eating the disk. The HTTP layer
// already caps the compressed input (20 GiB); these cap the expanded output.
const (
	maxTarOutputBytes = 64 << 30
	maxTarEntries     = 1 << 20
)

// validateTarEntry rejects absolute paths and traversal before extraction.
// Tar names always use slash separators, even on Windows hosts.
func validateTarEntry(name string) error {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := filepath.ToSlash(filepath.Clean("/" + name))
	if name == "" || name == "." || strings.HasPrefix(name, "/") || clean != "/"+strings.TrimPrefix(name, "./") {
		return fmt.Errorf("unsafe archive path %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return fmt.Errorf("unsafe archive path %q", name)
		}
	}
	return nil
}

// validateTarStream scans a gzip-decoded tar stream and validates every entry.
// It is intended for bounded temporary restore streams, not untrusted archives
// of arbitrary size; the HTTP layer applies the total body limit.
func validateTarStream(r io.Reader) error {
	t := tar.NewReader(r)
	for {
		h, err := t.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("invalid tar archive: %w", err)
		}
		if err := validateTarEntry(h.Name); err != nil {
			return err
		}
	}
}

// tarSymlink is a validated in-root symlink, created after all regular
// entries so a later archive entry cannot redirect an earlier one.
type tarSymlink struct {
	path   string
	target string
}

// extractTarGz streams a tar.gz archive into dest without ever leaving it:
// absolute paths, ".." escapes, devices/fifos/sockets and hardlinks are
// rejected; symlinks are recreated only when their target stays inside dest
// (unsafe ones are skipped); output size and entry count are capped
// (decompression-bomb defense). Regular files keep their rwx permission bits
// (setuid/setgid are dropped); ownership is left to the running user.
func extractTarGz(dest string, src io.Reader) error {
	gz, err := gzip.NewReader(src)
	if err != nil {
		return fmt.Errorf("invalid gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	var total int64
	var entries int
	var links []tarSymlink
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("invalid tar archive: %w", err)
		}
		entries++
		if entries > maxTarEntries {
			return fmt.Errorf("too many entries (limit %d)", maxTarEntries)
		}
		if err := validateTarEntry(h.Name); err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(h.Name))
		// Belt and suspenders: the join must stay inside dest even if the
		// validator above ever regresses.
		if rel, err := filepath.Rel(dest, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe archive path %q", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
			if err != nil {
				return err
			}
			n, copyErr := io.Copy(out, io.LimitReader(tr, maxTarOutputBytes-total+1))
			closeErr := out.Close()
			total += n
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			if total > maxTarOutputBytes {
				return fmt.Errorf("archive exceeds %d bytes expanded", maxTarOutputBytes)
			}
			if mode := os.FileMode(h.Mode & 0777); mode != 0 {
				_ = os.Chmod(target, mode)
			}
		case tar.TypeSymlink:
			// Defer: create only after files, and only in-root targets.
			if h.Linkname == "" || strings.HasPrefix(h.Linkname, "/") {
				continue
			}
			bad := false
			for _, part := range strings.Split(filepath.ToSlash(h.Linkname), "/") {
				if part == ".." {
					bad = true
					break
				}
			}
			if bad {
				continue
			}
			// Must also stay inside the destination tree: resolve the
			// link against its parent directory and re-check containment.
			resolved := filepath.Join(filepath.Dir(target), filepath.FromSlash(h.Linkname))
			if rel, err := filepath.Rel(dest, resolved); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			links = append(links, tarSymlink{path: target, target: h.Linkname})
		default:
			// Hardlinks, devices, fifos, sockets and other specials from
			// a backup archive are never materialized on the host.
			continue
		}
	}
	for _, l := range links {
		_ = os.Remove(l.path)
		if err := os.MkdirAll(filepath.Dir(l.path), 0755); err != nil {
			return err
		}
		if err := os.Symlink(l.target, l.path); err != nil {
			return err
		}
	}
	return nil
}
