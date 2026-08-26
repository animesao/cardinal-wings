package agent

import (
	"bufio"
	"context"
	"io"
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

// PullImageLines pulls an image and forwards each output line to onLine as it
// is produced, returning the combined output at the end.
func PullImageLines(ctx context.Context, ref, platform string, onLine func(line string)) (string, error) {
	cmd := buildCardinalCmd(ctx, pullArgs(ref, platform)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}

	var combined string
	stream := func(r io.Reader) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 1024*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if line != "" {
				onLine(line)
				combined += line + "\n"
			}
		}
	}
	go stream(stdout)
	stream(stderr)

	err = cmd.Wait()
	if err != nil {
		return combined, err
	}
	return combined, nil
}
