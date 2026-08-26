package agent

import (
	"context"
	"fmt"
)

// ServiceCreate delegates to `cardinal service create --name <name>
// [--replicas N] [--port P:T] [-e K=V] <image>`.
func ServiceCreate(ctx context.Context, name, image string, replicas int, ports, env []string) error {
	args := []string{"service", "create", "--name", name}
	if replicas > 0 {
		args = append(args, "--replicas", fmt.Sprintf("%d", replicas))
	}
	for _, p := range ports {
		args = append(args, "--port", p)
	}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(args, image)
	return runCardinal(ctx, args...)
}

// ServiceScale delegates to `cardinal service scale <name> <replicas>`.
func ServiceScale(ctx context.Context, name string, replicas int) error {
	return runCardinal(ctx, "service", "scale", name, fmt.Sprintf("%d", replicas))
}

// ServiceRemove delegates to `cardinal service rm <name>`.
func ServiceRemove(ctx context.Context, name string) error {
	return runCardinal(ctx, "service", "rm", name)
}

// ServiceList runs `cardinal service ls` and returns the raw output.
func ServiceList(ctx context.Context) (string, error) {
	return runCardinalOut(ctx, "service", "ls")
}

// FnDeploy delegates to `cardinal fn deploy --name <name> <image>`.
func FnDeploy(ctx context.Context, name, image string) error {
	return runCardinal(ctx, "fn", "deploy", "--name", name, image)
}

// FnInvoke delegates to `cardinal fn call <name> [--data <payload>]`.
func FnInvoke(ctx context.Context, name, data string) (string, error) {
	args := []string{"fn", "call", name}
	if data != "" {
		args = append(args, "--data", data)
	}
	return runCardinalOut(ctx, args...)
}

// FnRemove delegates to `cardinal fn rm <name>`.
func FnRemove(ctx context.Context, name string) error {
	return runCardinal(ctx, "fn", "rm", name)
}

// FnList runs `cardinal fn ls` and returns the raw output.
func FnList(ctx context.Context) (string, error) {
	return runCardinalOut(ctx, "fn", "ls")
}
