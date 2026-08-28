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

// InteractiveExec runs cardinal exec -it <id> <cmd...> with PTY allocation.
// The -t flag tells cardinal to allocate a pseudo-terminal so the shell gets
// echo, prompts, and line editing. Output is forwarded raw (not line-scanned)
// because PTY output includes control sequences that scanners would mangle.
func InteractiveExec(ctx context.Context, id string, cmd []string, stdin io.Reader, fn func(string)) error {
	if len(cmd) == 0 {
		return fmt.Errorf("cmd required")
	}
	args := []string{"exec", id, "-i", "-t"}
	args = append(args, cmd...)
	c := buildCardinalCmd(ctx, args...)

	if stdin != nil {
		c.Stdin = stdin
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		return err
	}

	if err := c.Start(); err != nil {
		return fmt.Errorf("cardinal exec: %w", err)
	}

	// Read raw output (PTY produces binary data with control sequences)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				fn(string(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}()

	waitErr := c.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		msg := waitErr.Error()
		if exit, ok := waitErr.(*exec.ExitError); ok {
			msg = strings.TrimSpace(string(exit.Stderr))
			if msg == "" {
				msg = fmt.Sprintf("exit code %d", exit.ExitCode())
			}
		}
		return fmt.Errorf("cardinal exec: %s", msg)
	}
	return nil
}

// Attach runs `cardinal attach <id>` which connects to the container's
// running main process via its unix console socket. stdin/stdout are piped
// through the provided channels so the caller can drive an interactive
// session (e.g. a WebSocket terminal).
//
// stdinCh: caller sends user input here; the goroutine writes it to cardinal's stdin.
// fn:      called for each chunk of output received from the container.
func Attach(ctx context.Context, id string, stdinCh <-chan string, fn func(string)) error {
	c := buildCardinalCmd(ctx, "attach", id)

	stdinW, err := c.StdinPipe()
	if err != nil {
		return fmt.Errorf("attach stdin pipe: %w", err)
	}
	stdoutR, err := c.StdoutPipe()
	if err != nil {
		return fmt.Errorf("attach stdout pipe: %w", err)
	}
	c.Stderr = nil // merge stderr into stdout

	if err := c.Start(); err != nil {
		return fmt.Errorf("cardinal attach: %w", err)
	}

	// Read container output → caller
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdoutR.Read(buf)
			if n > 0 {
				fn(string(buf[:n]))
			}
			if err != nil {
				done <- err
				return
			}
		}
	}()

	// Caller input → container stdin
	go func() {
		defer stdinW.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case line, ok := <-stdinCh:
				if !ok {
					return
				}
				if _, err := io.WriteString(stdinW, line); err != nil {
					return
				}
			}
		}
	}()

	// Wait for process to exit
	waitErr := c.Wait()
	<-done // wait for reader goroutine

	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		return fmt.Errorf("cardinal attach: %w", waitErr)
	}
	return nil
}

// RawExec runs `cardinal exec [-i] <id> <cmd...>` with raw binary stdin/stdout
// and returns the piped stdout stream plus a Wait function — no line
// processing anywhere. Unlike StreamExec (line-scanned text) this is for
// binary payloads: container data backups where `tar czf -` streams out of
// stdout and archives stream into stdin.
func RawExec(ctx context.Context, id string, cmd []string, stdin io.Reader) (stdout io.ReadCloser, wait func() error, err error) {
	if len(cmd) == 0 {
		return nil, nil, fmt.Errorf("cmd required")
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
	out, err := c.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	c.Stderr = nil // stderr merged into stdout would corrupt binary output

	if err := c.Start(); err != nil {
		return nil, nil, fmt.Errorf("cardinal exec: %w", err)
	}
	return out, c.Wait, nil
}
