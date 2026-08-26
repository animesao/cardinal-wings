package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultRegistryURL mirrors cardinal's default blueprint repository so that
// wings surfaces the same catalog a `cardinal blueprint install` would use.
const DefaultRegistryURL = "https://raw.githubusercontent.com/cardinal-organization/cardinal-blueprints/main/registry.json"

// BlueprintCatalogEntry is a single blueprint in the registry.
type BlueprintCatalogEntry struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Image       string `json:"image"`
	File        string `json:"file"`
}

// BlueprintCatalog is the merged registry listing.
type BlueprintCatalog struct {
	Version     int                              `json:"version"`
	Description string                           `json:"description"`
	BaseURL     string                           `json:"base_url"`
	Blueprints  map[string]BlueprintCatalogEntry `json:"blueprints"`
}

// BlueprintTemplate mirrors cardinal's template.json schema so the panel can
// show install-time configuration before calling install.
type BlueprintTemplate struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Image       string `json:"image"`
	Tag         string `json:"tag"`
	Command     string `json:"command"`
	Env         string `json:"env"`
	Ports       string `json:"ports"`
	Volumes     string `json:"volumes"`
	CapAdd      string `json:"cap_add"`
	Network     string `json:"network"`
	Memory      string `json:"memory"`
	CPUs        string `json:"cpus"`
	Restart     string `json:"restart"`
}

// BlueprintRegistry fetches and renders the blueprint catalog. url may be
// empty to use the default official registry.
func (c *Client) BlueprintRegistry(ctx context.Context, url string) (*BlueprintCatalog, error) {
	if url == "" {
		url = DefaultRegistryURL
	}
	var cat BlueprintCatalog
	if err := fetchJSON(ctx, c.hc, url, &cat); err != nil {
		return nil, err
	}
	// Use the catalog's declared base_url so template URLs are correct.
	return &cat, nil
}

// BlueprintTemplate fetches a single blueprint template JSON by full URL.
func (c *Client) BlueprintTemplate(ctx context.Context, templateURL string) (*BlueprintTemplate, error) {
	var tpl BlueprintTemplate
	if err := fetchJSON(ctx, c.hc, templateURL, &tpl); err != nil {
		return nil, err
	}
	return &tpl, nil
}

// List returns a flat, sorted-friendly list of catalog entries.
func (cat *BlueprintCatalog) List() []BlueprintCatalogEntry {
	out := make([]BlueprintCatalogEntry, 0, len(cat.Blueprints))
	for _, e := range cat.Blueprints {
		out = append(out, e)
	}
	return out
}

func fetchJSON(ctx context.Context, hc *http.Client, rawURL string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	client := hc
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", rawURL, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
