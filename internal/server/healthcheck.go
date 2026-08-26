package server

import (
	"context"
	"sync"
	"time"

	"github.com/animesao/cardinal-wings/internal/runtime"
)

// nodeStatus holds the last observed health of a node.
type nodeStatus struct {
	status    string // up | down
	checkedAt time.Time
	err       string
}

// healthChecker pings every node on an interval so /v1/nodes reflects live
// status instead of the configured-at-boot snapshot.
type healthChecker struct {
	interval time.Duration
	mu       sync.Mutex
	statuses map[string]nodeStatus
}

var checker = &healthChecker{interval: 15 * time.Second, statuses: map[string]nodeStatus{}}

// start launches the background loop; it stops when ctx is done.
func (hc *healthChecker) start(ctx context.Context) {
	ticker := time.NewTicker(hc.interval)
	go func() {
		hc.checkAll(ctx)
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				hc.checkAll(ctx)
			}
		}
	}()
}

// checkAll pings every registered node concurrently and records status.
func (hc *healthChecker) checkAll(ctx context.Context) {
	names := registry.names()
	var wg sync.WaitGroup
	for _, name := range names {
		c := registry.byName(name)
		if c == nil {
			continue
		}
		wg.Add(1)
		go func(name string, c *runtime.Client) {
			defer wg.Done()
			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			status, err := c.PingNode(checkCtx)
			hc.mu.Lock()
			if err != nil {
				hc.statuses[name] = nodeStatus{status: "down", checkedAt: time.Now(), err: err.Error()}
			} else {
				hc.statuses[name] = nodeStatus{status: status, checkedAt: time.Now()}
			}
			hc.mu.Unlock()
		}(name, c)
	}
	wg.Wait()
}

// snapshot returns a copy of the last known statuses.
func (hc *healthChecker) snapshot() map[string]nodeStatus {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	out := make(map[string]nodeStatus, len(hc.statuses))
	for k, v := range hc.statuses {
		out[k] = v
	}
	return out
}
