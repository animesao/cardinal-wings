package server

import (
	"github.com/animesao/cardinal-wings/internal/config"
	"github.com/animesao/cardinal-wings/internal/tasks"
	"github.com/animesao/cardinal-wings/internal/webhooks"
)

// notifier delivers webhook events to configured URLs.
var notifier *webhooks.Notifier

// initWebhooks wires the notifier and the task-completion hook.
func initWebhooks(cfg *config.Config) {
	notifier = webhooks.New(cfg.Webhooks)
	taskMgr.OnComplete(func(t tasks.Task) {
		if notifier == nil {
			return
		}
		notifier.Fire("task.completed", map[string]interface{}{
			"id":     t.ID,
			"kind":   t.Kind,
			"status": t.Status,
			"error":  t.Error,
		})
	})
}
