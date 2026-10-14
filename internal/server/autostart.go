package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/animesao/cardinal-wings/internal/config"
	"github.com/animesao/cardinal-wings/internal/runtime"
)

// autostart.go raises panel-flagged containers after a host boot. The panel
// owns the per-server autoStart flag; wings asks
// GET {panel}/api/remote/autostart (node-token authed, same scheme as the
// SFTP auth call) and starts every listed container that is not running.
// Best effort and background-only: a missing panel link or an unreachable
// panel skips the pass without ever blocking daemon startup.

// bootAutostart starts the autostart pass in the background. It returns
// immediately when there is no local cardinal client or no panel link.
func bootAutostart(cfg *config.Config, c *runtime.Client) {
	if c == nil {
		return
	}
	link, ok := effectivePanelLink(cfg)
	if !ok {
		return
	}
	go runAutostartPass(link, c)
}

func runAutostartPass(link *panelLink, c *runtime.Client) {
	refs, err := fetchAutostartRefs(link)
	if err != nil {
		last := err
		for i := 0; i < 5; i++ {
			time.Sleep(10 * time.Second)
			if refs, last = fetchAutostartRefs(link); last == nil {
				err = nil
				break
			}
		}
		if err != nil {
			logf("autostart: panel unreachable, skipping: %v", last)
			return
		}
	}
	for _, ref := range refs {
		startAutostartRef(c, ref)
		time.Sleep(time.Second)
	}
}

func startAutostartRef(c *runtime.Client, ref string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	d, err := c.Inspect(ctx, ref)
	if err != nil {
		logf("autostart: inspect %s: %v", ref, err)
		return
	}
	if d != nil && isRunningStatus(d.Status) {
		return
	}
	if err := c.Action(ctx, ref, "start"); err != nil {
		logf("autostart: start %s: %v", ref, err)
		return
	}
	logf("autostart: started %s", ref)
}

func fetchAutostartRefs(link *panelLink) ([]string, error) {
	if link == nil || link.URL == "" {
		return nil, fmt.Errorf("no panel link configured")
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(link.URL, "/")+"/api/remote/autostart", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if link.TokenID != "" {
		req.Header.Set("Authorization", "Bearer "+link.TokenID+"."+link.Token)
	} else {
		req.Header.Set("Authorization", "Bearer "+link.Token)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("panel autostart: status %d", resp.StatusCode)
	}
	var out struct {
		Servers []string `json:"servers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Servers, nil
}
