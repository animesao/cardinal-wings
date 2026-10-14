package ws

import (
	"bufio"
	"io"
	"net"
	"testing"
)

// TestPeerCloseEchoedOnce verifies a client-initiated close gets exactly one
// close frame back. Echoing it AND re-sending one in Close() produced two
// frames, which browsers report as "Close received after close".
func TestPeerCloseEchoedOnce(t *testing.T) {
	s, c := net.Pipe()
	srv := &Conn{conn: s, r: bufio.NewReader(s), w: bufio.NewWriter(s)}

	readDone := make(chan error, 1)
	go func() {
		_, err := srv.ReadText()
		readDone <- err
	}()

	// Client sends masked close(1000).
	cw := bufio.NewWriter(c)
	if err := writeMaskedFrame(cw, 0x8, []byte{0x03, 0xE8}); err != nil {
		t.Fatal(err)
	}
	if err := cw.Flush(); err != nil {
		t.Fatal(err)
	}

	// The single echo must be FIN+close, len 2, same status payload.
	echo := make([]byte, 4)
	if _, err := io.ReadFull(c, echo); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if echo[0] != 0x88 || echo[1] != 0x02 || echo[2] != 0x03 || echo[3] != 0xE8 {
		t.Fatalf("echo = % x, want 88 02 03 e8", echo)
	}

	if err := <-readDone; err != io.EOF {
		t.Fatalf("ReadText err = %v, want EOF", err)
	}

	// Close() after a received close must not emit a second frame.
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	rest, err := io.ReadAll(c)
	if err != nil {
		t.Fatalf("read tail: %v", err)
	}
	if len(rest) != 0 {
		t.Fatalf("second close frame sent: % x", rest)
	}
}

// TestInitiatorCloseSendsOnce verifies Close() emits exactly one close frame
// even when called twice (closeOnce still holds).
func TestInitiatorCloseSendsOnce(t *testing.T) {
	s, c := net.Pipe()
	srv := &Conn{conn: s, r: bufio.NewReader(s), w: bufio.NewWriter(s)}

	// net.Pipe writes block until read: drain concurrently.
	readDone := make(chan []byte, 1)
	go func() {
		rest, _ := io.ReadAll(c)
		readDone <- rest
	}()

	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	rest := <-readDone
	if len(rest) != 2 || rest[0] != 0x88 || rest[1] != 0x00 {
		t.Fatalf("frames = % x, want a single 88 00", rest)
	}
}
