package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// PullImage pulls an image via the local `cardinal pull` CLI. cardinal serve
// exposes pulling only implicitly (container create), so for the explicit
// pull endpoint wings delegates to the CLI on this host.
func PullImage(ctx context.Context, ref, platform string) error {
	args := []string{"pull", ref}
	if platform != "" {
		args = append(args, "--platform", platform)
	}
	cmd := exec.CommandContext(ctx, "cardinal", args...)
	var stderr, stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cardinal pull %s: %v: %s", ref, err, stderr.String())
	}
	return nil
}
