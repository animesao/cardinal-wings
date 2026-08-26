package agent

import (
	"bufio"
	"context"
	"io"
)

// StreamEvents runs `cardinal events` as a subprocess and forwards each JSON
// line to fn. cardinal's HTTP API does not expose the event bus, so wings
// tails the CLI, which subscribes to the in-process event broadcaster. The
// subprocess lives only as long as ctx (or the pipe breaks).
func StreamEvents(ctx context.Context, fn func(line string)) error {
	cmd := buildCardinalCmd(ctx, "events")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line != "" {
			fn(line)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	_ = cmd.Wait()
	return io.EOF
}
