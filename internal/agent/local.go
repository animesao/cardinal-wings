// Package agent manages connections to cardinal hosts: it starts the local
// `cardinal serve` HTTP API as a subprocess (loopback + ephemeral token) so
// wings can manage this node, and exposes clients for configured remote nodes.
package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os/exec"
	"sync"
	"time"

	"github.com/animesao/cardinal-wings/internal/runtime"
)

// Local is a handle to the local cardinal node.
type Local struct {
	host   string
	port   int
	token  string
	cmd    *exec.Cmd
	client *runtime.Client
	mu     sync.Mutex
}

// StartLocal launches `cardinal serve -H 127.0.0.1 -p <port> --token <token>`
// as a subprocess and waits for it to become reachable.
func StartLocal() (*Local, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	tok, err := randomToken()
	if err != nil {
		return nil, err
	}
	host := "127.0.0.1"
	cmd := exec.Command("cardinal", "serve", "-H", host, "-p", fmt.Sprintf("%d", port), "--token", tok, "-d")
	l := &Local{host: host, port: port, token: tok, cmd: cmd}
	client := runtime.NewClient(fmt.Sprintf("http://%s:%d", host, port), tok)
	l.client = client

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start local cardinal serve: %w", err)
	}

	// Wait for the API to come up (daemon detaches, so exec.Cmd is short-lived).
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := timeoutCtx(500 * time.Millisecond)
		if client.Ping(ctx) == nil {
			cancel()
			return l, nil
		}
		cancel()
		time.Sleep(250 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return nil, fmt.Errorf("local cardinal serve did not become ready on :%d", port)
}

// Client returns the runtime client for the local node.
func (l *Local) Client() *runtime.Client { return l.client }

// Stop shuts down the local cardinal serve subprocess.
func (l *Local) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cmd != nil && l.cmd.Process != nil {
		_ = l.cmd.Process.Kill()
	}
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func timeoutCtx(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
