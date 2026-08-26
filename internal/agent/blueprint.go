package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// BlueprintInstall runs `cardinal blueprint install <name>` with optional
// override flags, returning combined output.
func BlueprintInstall(ctx context.Context, name string, memory, cpus string, env []string, yes bool) error {
	args := []string{"blueprint", "install", name}
	if memory != "" {
		args = append(args, "--memory", memory)
	}
	if cpus != "" {
		args = append(args, "--cpus", cpus)
	}
	for _, kv := range env {
		args = append(args, "-e", kv)
	}
	if yes {
		args = append(args, "-y")
	}
	return runCardinal(ctx, args...)
}

// BlueprintUninstall runs `cardinal blueprint uninstall <name>`.
func BlueprintUninstall(ctx context.Context, name string) error {
	return runCardinal(ctx, "blueprint", "uninstall", name)
}

func runCardinal(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "cardinal", args...)
	var stderr, stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		out := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
		return fmt.Errorf("cardinal %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return nil
}
