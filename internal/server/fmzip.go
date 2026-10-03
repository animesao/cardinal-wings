package server

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Caps keep a hostile or absurd archive from eating disk/memory.
const (
	maxZipBytes   = 100 << 20 // 100 MiB total per archive
	maxZipEntries = 10000
)

// zipDir packs a host directory into a zip archive. Symlinks are skipped
// (never followed), so a link pointing outside the jail cannot smuggle
// outside files into the archive.
func zipDir(root string) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	var total int64
	entries := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		entries++
		if entries > maxZipEntries {
			return fmt.Errorf("too many entries (limit %d)", maxZipEntries)
		}
		name := filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			_, err := w.Create(name + "/")
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		fw, err := w.Create(name)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		n, err := io.Copy(fw, io.LimitReader(f, maxZipBytes-total+1))
		total += n
		if err != nil {
			return err
		}
		if total > maxZipBytes {
			return fmt.Errorf("archive exceeds %d bytes", maxZipBytes)
		}
		return nil
	})
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// unzipInto extracts a zip archive under dest, rejecting absolute paths,
// ".." escapes and oversized archives (zip-slip defense). Everything is
// created as regular files/dirs; modes and links from the archive are
// ignored.
func unzipInto(data []byte, dest string) error {
	if len(data) > maxZipBytes {
		return fmt.Errorf("archive exceeds %d bytes", maxZipBytes)
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	if len(r.File) > maxZipEntries {
		return fmt.Errorf("too many entries (limit %d)", maxZipEntries)
	}
	var total uint64
	for _, f := range r.File {
		if !cleanZipName(f.Name) {
			return fmt.Errorf("unsafe entry %q", f.Name)
		}
		total += f.UncompressedSize64
		if total > maxZipBytes {
			return fmt.Errorf("archive exceeds %d bytes", maxZipBytes)
		}
	}
	for _, f := range r.File {
		target := filepath.Join(dest, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(rc, maxZipBytes+1))
		closeErr := out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

// cleanZipName reports whether a zip entry name stays inside the destination.
func cleanZipName(name string) bool {
	if name == "" || filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(name), "/") {
		if part == ".." {
			return false
		}
	}
	return true
}
