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
// panel sees command output in real time.
func StreamExec(ctx context.Context, id string, cmd []string, fn func(line string)) error {
	return streamExec(ctx, id, cmd, nil, fn)
}

// InteractiveExec runs `cardinal exec -i <id> <cmd...>` with stdin connected
// to the provided input reader, so a panel can drive an interactive command
// (a shell) over a terminal. stdout/stderr lines go to fn.
func InteractiveExec(ctx context.Context, id string, cmd []string, stdin io.Reader, fn func(line string)) error {
	return streamExec(ctx, id, cmd, stdin, fn)
}

func streamExec(ctx context.Context, id string, cmd []string, stdin io.Reader, fn func(line string)) error {
	if len(cmd) == 0 {
		return fmt.Errorf("cmd required")
	}
	args := []string{"exec", id}
	if stdin != nil {
		args = append(args, "-i")
	}
	args = append(args, cmd...)
	c := buildCardinalCmd(ctx, args...)

	if stdin != nil {
		c.Stdin = stdin
	}
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

	done := make(chan struct{}, 2)
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
