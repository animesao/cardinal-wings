package agent

import (
	"context"
	"fmt"
	"sort"
)

// NetworkLs runs `cardinal network ls` and returns raw output.
func NetworkLs(ctx context.Context) (string, error) {
	return runCardinalOut(ctx, "network", "ls")
}

// NetworkInspect runs `cardinal network inspect <name>`.
func NetworkInspect(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("network name required")
	}
	return runCardinalOut(ctx, "network", "inspect", name)
}

func networkCreateArgs(name, subnet string) []string {
	args := []string{"network", "create"}
	if subnet != "" {
		args = append(args, "--subnet", subnet)
	}
	return append(args, name)
}

// NetworkCreate runs `cardinal network create [--subnet S] <name>`.
func NetworkCreate(ctx context.Context, name, subnet string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("network name required")
	}
	return runCardinalOut(ctx, networkCreateArgs(name, subnet)...)
}

// NetworkRemove runs `cardinal network rm <name>`.
func NetworkRemove(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("network name required")
	}
	return runCardinal(ctx, "network", "rm", name)
}

// VolumeLs runs `cardinal volume ls` and returns raw output.
func VolumeLs(ctx context.Context) (string, error) {
	return runCardinalOut(ctx, "volume", "ls")
}

// VolumeInspect runs `cardinal volume inspect <name>`.
func VolumeInspect(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("volume name required")
	}
	return runCardinalOut(ctx, "volume", "inspect", name)
}

func volumeCreateArgs(name, driver string, labels map[string]string) []string {
	args := []string{"volume", "create"}
	if driver != "" {
		args = append(args, "-d", driver)
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-l", k+"="+labels[k])
	}
	return append(args, name)
}

// VolumeCreate runs `cardinal volume create [-d driver] [-l k=v] <name>`.
func VolumeCreate(ctx context.Context, name, driver string, labels map[string]string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("volume name required")
	}
	return runCardinalOut(ctx, volumeCreateArgs(name, driver, labels)...)
}

// VolumeRemove runs `cardinal volume rm <name>`.
func VolumeRemove(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("volume name required")
	}
	return runCardinal(ctx, "volume", "rm", name)
}

// VolumePrune runs `cardinal volume prune` and returns raw output.
func VolumePrune(ctx context.Context) (string, error) {
	return runCardinalOut(ctx, "volume", "prune")
}
