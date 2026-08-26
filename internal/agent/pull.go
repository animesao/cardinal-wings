package agent

import (
	"context"
)

// pullArgs builds `cardinal pull [--platform X] <ref>` args.
func pullArgs(ref, platform string) []string {
	args := []string{"pull", ref}
	if platform != "" {
		args = append(args, "--platform", platform)
	}
	return args
}

// PullImage pulls an image via the local `cardinal pull` CLI. cardinal serve
// exposes pulling only implicitly (container create), so for the explicit
// pull endpoint wings delegates to the CLI on this host.
func PullImage(ctx context.Context, ref, platform string) error {
	return runCardinal(ctx, pullArgs(ref, platform)...)
}

// PullImageOut pulls an image and returns the CLI output.
func PullImageOut(ctx context.Context, ref, platform string) (string, error) {
	return runCardinalOut(ctx, pullArgs(ref, platform)...)
}
