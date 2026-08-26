package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// StreamExec runs `cardinal exec <id> <cmd...>` on the local node with
// stdout+stderr piped to fn line by line. It returns the process exit error
// (nil on success). Because cardinal's exec output is not exposed over its
// HTTP API, wings delegates to the local CLI and streams the output — the
// panel sees command output in real time. Interactive stdin (a true TTY) is
// not supported by cardinal's exec yet.
func StreamExec(ctx context.Context, id string, cmd []string, fn func(line string)) error {
	if len(cmd) == 0 {
		return fmt.Errorf("cmd required")
	}
	args := append([]string{"exec", id}, cmd...)
	c := buildCardinalCmd(ctx, args...)

	stdout, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := c.StderrPipe()
	if err != nil {
		return err
	}

	if err := c.Start(); err != nil {
		return fmt.Errorf("cardinal exec: %w", err)
	}

	done := make(chan struct{})
	stream := func(r io.Reader) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 1024*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if line != "" {
				fn(line)
			}
		}
		done <- struct{}{}
	}
	go stream(stdout)
	go stream(stderr)

	// Wait for both pipes to drain, then for the process.
	<-done
	<-done
	err = c.Wait()
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		msg := err.Error()
		if exit, ok := err.(*exec.ExitError); ok {
			msg = strings.TrimSpace(string(exit.Stderr))
			if msg == "" {
				msg = fmt.Sprintf("exit code %d", exit.ExitCode())
			}
		}
		return fmt.Errorf("cardinal exec: %s", msg)
	}
	return nil
}
