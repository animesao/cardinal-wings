package agent

import (
	"context"
	"fmt"
)

// PortShow runs `cardinal port <container>` and returns raw output.
func PortShow(ctx context.Context, id string) (string, error) {
	return runCardinalOut(ctx, "port", id)
}

// PortAdd runs `cardinal port add <container> <host>:<container>[/proto]`.
func PortAdd(ctx context.Context, id, mapping string) error {
	if mapping == "" {
		return fmt.Errorf("mapping required (host:container[/proto])")
	}
	return runCardinal(ctx, "port", "add", id, mapping)
}

// PortRemove runs `cardinal port rm <container> <host>[/proto]`.
func PortRemove(ctx context.Context, id, mapping string) error {
	if mapping == "" {
		return fmt.Errorf("mapping required (host[/proto])")
	}
	return runCardinal(ctx, "port", "rm", id, mapping)
}

// Commit runs `cardinal commit <container> <image>[:tag]`.
func Commit(ctx context.Context, id, ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("image ref required")
	}
	return runCardinalOut(ctx, "commit", id, ref)
}

// Verify runs `cardinal verify <image>` and returns raw output.
func Verify(ctx context.Context, ref string) (string, error) {
	return runCardinalOut(ctx, "verify", ref)
}

// SystemDF runs `cardinal system df` and returns raw output.
func SystemDF(ctx context.Context) (string, error) {
	return runCardinalOut(ctx, "system", "df")
}

// SystemPrune runs `cardinal system prune` and returns raw output.
func SystemPrune(ctx context.Context) (string, error) {
	return runCardinalOut(ctx, "system", "prune")
}

// ServiceInspect runs `cardinal service inspect <name>` when supported,
// falling back to the raw list output filtered by the caller.
func ServiceInspect(ctx context.Context, name string) (string, error) {
	if out, err := runCardinalOut(ctx, "service", "inspect", name); err == nil {
		return out, nil
	}
	return runCardinalOut(ctx, "service", "ls")
}

// ServiceUpdate runs `cardinal service update <name> [--image X] [--replicas N]`.
func ServiceUpdate(ctx context.Context, name, image string, replicas *int) error {
	args := []string{"service", "update", name}
	if image != "" {
		args = append(args, "--image", image)
	}
	if replicas != nil {
		args = append(args, "--replicas", fmt.Sprintf("%d", *replicas))
	}
	return runCardinal(ctx, args...)
}

// ClusterInfo runs `cardinal cluster info` and returns raw output.
func ClusterInfo(ctx context.Context) (string, error) {
	return runCardinalOut(ctx, "cluster", "info")
}

// ClusterList runs `cardinal cluster ls` and returns raw output.
func ClusterList(ctx context.Context) (string, error) {
	return runCardinalOut(ctx, "cluster", "ls")
}
