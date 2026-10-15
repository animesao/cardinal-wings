package server

import (
	"testing"
	"time"
)

func newTestSession() *terminalSession {
	return &terminalSession{
		id:      "assemble-test",
		stdinCh: make(chan string, 64),
		done:    make(chan struct{}),
		subs:    map[chan string]struct{}{},
	}
}

// Chunks split mid-line must reach subscribers as whole lines.
func TestBroadcastReassemblesSplitLines(t *testing.T) {
	s := newTestSession()
	sub, _ := s.subscribe()
	s.broadcast("[12:18:17 INF")
	s.broadcast("O]: Tile Max Tick Time: 50ms\n[12:18:17 INFO]: done\n")

	var got []string
	timeout := time.After(2 * time.Second)
	for len(got) < 1 {
		select {
		case l := <-sub:
			got = append(got, l)
		case <-timeout:
			t.Fatalf("no lines arrived: %q", got)
		}
	}
	// The split line arrives whole; the second broadcast (two complete
	// lines in one chunk) goes out as a single message — the panel
	// splits messages on \n itself.
	if got[0] != "[12:18:17 INFO]: Tile Max Tick Time: 50ms\n[12:18:17 INFO]: done\n" {
		t.Fatalf("lines not reassembled: %q", got[0])
	}
}

// A fragment without a newline must not stall forever.
func TestBroadcastFlushesPending(t *testing.T) {
	s := newTestSession()
	sub, _ := s.subscribe()
	s.broadcast("progress 50%\r")
	select {
	case got := <-sub:
		if got != "progress 50%\r" {
			t.Fatalf("flushed fragment wrong: %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending fragment never flushed")
	}
}

// Control markers travel intact, never glued to a held fragment.
func TestBroadcastMarkersNotGlued(t *testing.T) {
	s := newTestSession()
	sub, _ := s.subscribe()
	s.broadcast("half-line")
	s.broadcast("__session_ended__")
	var got []string
	timeout := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case l := <-sub:
			got = append(got, l)
		case <-timeout:
			t.Fatalf("only %d/2 messages arrived: %q", len(got), got)
		}
	}
	if got[0] != "half-line" {
		t.Fatalf("fragment altered: %q", got[0])
	}
	if got[1] != "__session_ended__" {
		t.Fatalf("marker glued: %q", got[1])
	}
}
