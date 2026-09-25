// Package webhooks delivers POST notifications to configured URLs when events
// occur (task completion, container events). Each configured webhook filters
// by its Events list; "*" matches everything.
package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/animesao/cardinal-wings/internal/config"
)

// Notifier holds the configured webhooks.
type Notifier struct {
	mu       sync.Mutex
	hooks    []config.Webhook
	client   *http.Client
	lastSent map[string]time.Time // URL -> last send, for rate limiting
}

// New builds a Notifier from config.
func New(hooks []config.Webhook) *Notifier {
	enabled := make([]config.Webhook, 0, len(hooks))
	for _, h := range hooks {
		if h.Enabled && h.URL != "" {
			enabled = append(enabled, h)
		}
	}
	return &Notifier{
		hooks:    enabled,
		client:   &http.Client{Timeout: 10 * time.Second},
		lastSent: map[string]time.Time{},
	}
}

// Event is the payload delivered to a webhook.
type Event struct {
	Event   string      `json:"event"`
	Payload interface{} `json:"payload"`
	Time    string      `json:"time"`
}

// Fire delivers an event to every subscribed, enabled webhook. It never
// blocks the caller: deliveries run in background goroutines and failures are
// swallowed (a webhook must never take down wings).
func (n *Notifier) Fire(event string, payload interface{}) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n == nil || len(n.hooks) == 0 {
		return
	}
	body, err := json.Marshal(Event{
		Event:   event,
		Payload: payload,
		Time:    time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return
	}
	for _, h := range n.hooks {
		if !matches(h.Events, event) {
			continue
		}
		hook := h
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, bytes.NewReader(body))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "cardinal-wings")
			if hook.Secret != "" {
				req.Header.Set("X-Webhook-Secret", hook.Secret)
			}
			_, _ = n.client.Do(req)
		}()
	}
}

func matches(list []string, event string) bool {
	for _, e := range list {
		if e == "*" || e == event {
			return true
		}
	}
	return false
}
