// Package runtime talks to a cardinal host over its Docker-compatible HTTP
// API (`cardinal serve`). Each runtime.Client points at one cardinal node —
// the local one started by wings, or a remote cluster node — so local and
// remote management share the same code path.
package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is a thin wrapper around cardinal serve's HTTP API.
type Client struct {
	base  string // e.g. http://127.0.0.1:2375
	token string // Bearer token, empty when auth disabled
	hc    *http.Client
}

// NewClient builds a client for a cardinal base URL.
func NewClient(base, token string) *Client {
	return &Client{
		base:  base,
		token: token,
		hc:    &http.Client{Timeout: 120 * time.Second},
	}
}

// Base returns the configured base URL.
func (c *Client) Base() string { return c.base }

func (c *Client) do(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, string(b))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// Ping returns nil when the cardinal host responds.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/_ping", nil)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
