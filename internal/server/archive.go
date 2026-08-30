package server

import (
	"archive/tar"
	"fmt"
	"io"
	"path/filepath"
	"strings"
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
