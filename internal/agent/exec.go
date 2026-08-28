package agent

import (
	"bufio"
	"bytes"
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

// ExecCapture runs a command in a container and returns all stdout as a
// string (with optional stdin piped in). This powers the file manager: wings
// runs `sh -c '<script>'` and the panel can read/write/upload/download files
// even though cardinal's own `fs` CLI is read-only.
func ExecCapture(ctx context.Context, id, script string, stdin []byte) (string, error) {
	args := []string{"exec", id, "/bin/sh", "-c", script}
	var in io.Reader
	if len(stdin) > 0 {
		in = bytes.NewReader(stdin)
		args = []string{"exec", id, "-i", "/bin/sh", "-c", script}
	}
	c := buildCardinalCmd(ctx, args...)
	if in != nil {
		c.Stdin = in
	}
	out, err := c.Output()
	if err != nil {
		msg := err.Error()
		if exit, ok := err.(*exec.ExitError); ok {
			msg = strings.TrimSpace(string(exit.Stderr))
			if msg == "" {
				msg = fmt.Sprintf("exit code %d", exit.ExitCode())
			}
		}
		return string(out), fmt.Errorf("cardinal exec: %s", msg)
	}
	return string(out), nil
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
			fn(line)
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
