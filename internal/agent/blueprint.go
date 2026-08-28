package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// blueprintInstallArgs builds `cardinal blueprint install <name>` args.
func blueprintInstallArgs(name, memory, cpus string, env []string, yes bool) []string {
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
	return args
}

// BlueprintInstall runs `cardinal blueprint install <name>` with optional
// override flags.
func BlueprintInstall(ctx context.Context, name string, memory, cpus string, env []string, yes bool) error {
	return runCardinal(ctx, blueprintInstallArgs(name, memory, cpus, env, yes)...)
}

// BlueprintInstallOut runs `cardinal blueprint install` and returns its output.
func BlueprintInstallOut(ctx context.Context, name string, memory, cpus string, env []string, yes bool) (string, error) {
	return runCardinalOut(ctx, blueprintInstallArgs(name, memory, cpus, env, yes)...)
}

// BootstrapEnsure installs and starts Cardinal's persistent boot supervisor.
func BootstrapEnsure(ctx context.Context) error {
	return runCardinal(ctx, "bootstrap", "--install")
}

// BlueprintUninstall runs `cardinal blueprint uninstall <name>`.
func BlueprintUninstall(ctx context.Context, name string) error {
	return runCardinal(ctx, "blueprint", "uninstall", name)
}

// buildCardinalCmd builds an exec.Cmd for the cardinal CLI with the given args.
func buildCardinalCmd(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "cardinal", args...)
}

// runCardinal runs cardinal, returning an error that includes output.
func runCardinal(ctx context.Context, args ...string) error {
	_, err := runCardinalOut(ctx, args...)
	return err
}

// runCardinalOut runs cardinal and returns trimmed stdout on success.
func runCardinalOut(ctx context.Context, args ...string) (string, error) {
	cmd := buildCardinalCmd(ctx, args...)
	var stderr, stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		out := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
		return "", fmt.Errorf("cardinal %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(stdout.String()), nil
}
