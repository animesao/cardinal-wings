package server

import (
	"strings"
	"testing"
	"time"
)

// Typed input must be echoed to session subscribers (PTY-like behavior):
// otherwise what the operator wrote vanishes while responses persist,
// for every watcher and across replay.
func TestWriteInputEchoesToSubscribers(t *testing.T) {
	s := &terminalSession{
		id:      "echo-test",
		stdinCh: make(chan string, 64),
		done:    make(chan struct{}),
		subs:    map[chan string]struct{}{},
	}
	// Drain stdin like agent.Attach would.
	go func() {
		for range s.stdinCh {
		}
	}()
	sub, _ := s.subscribe()
	if err := s.writeInput("tps\n"); err != nil {
		t.Fatalf("writeInput: %v", err)
	}
	select {
	case got := <-sub:
		if !strings.Contains(got, "tps") {
			t.Fatalf("echo = %q, want it to contain the typed command", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no echo broadcast for typed input")
	}
	// The echo must also sit in the replay ring for late subscribers.
	s.mu.Lock()
	found := false
	for _, l := range s.ring {
		if strings.Contains(l, "tps") {
			found = true
			break
		}
	}
	s.mu.Unlock()
	if !found {
		t.Fatalf("echo missing from replay ring: %q", s.ring)
	}
}
