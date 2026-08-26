package agent

import (
	"context"
)

// FsList lists a container directory (or root) via `cardinal fs ls`.
func FsList(ctx context.Context, id, path string) (string, error) {
	args := []string{"fs", "ls", id}
	if path != "" {
		args = append(args, path)
	}
	return runCardinalOut(ctx, args...)
}

// FsCat prints a file from a container via `cardinal fs cat`.
func FsCat(ctx context.Context, id, path string) (string, error) {
	if path == "" {
		path = "/"
	}
	return runCardinalOut(ctx, "fs", "cat", id, path)
}

// FsTree renders a container tree via `cardinal fs tree`.
func FsTree(ctx context.Context, id, path string) (string, error) {
	args := []string{"fs", "tree", id}
	if path != "" {
		args = append(args, path)
	}
	return runCardinalOut(ctx, args...)
}

// Cp copies a file into a container via `cardinal cp <src> <dst>`.
func Cp(ctx context.Context, src, dst string) error {
	return runCardinal(ctx, "cp", src, dst)
}
