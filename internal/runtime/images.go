package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Image is an API-facing image summary.
type Image struct {
	ID       string   `json:"id"`
	RepoTags []string `json:"repo_tags,omitempty"`
	Size     int64    `json:"size_bytes,omitempty"`
}

// Images returns the local image list.
func (c *Client) Images(ctx context.Context) ([]Image, error) {
	var raw []struct {
		ID       string   `json:"Id"`
		RepoTags []string `json:"RepoTags"`
		Size     int64    `json:"Size"`
	}
	if err := c.do(ctx, "GET", "/images/json", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]Image, 0, len(raw))
	for _, r := range raw {
		out = append(out, Image{ID: r.ID, RepoTags: r.RepoTags, Size: r.Size})
	}
	return out, nil
}

// InspectImage returns details for one image ref (name[:tag]).
func (c *Client) InspectImage(ctx context.Context, ref string, out interface{}) error {
	return c.do(ctx, "GET", "/images/"+ref+"/json", nil, out)
}

// RemoveImage deletes an image ref.
func (c *Client) RemoveImage(ctx context.Context, ref string) error {
	return c.do(ctx, "DELETE", "/images/"+ref, nil, nil)
}

// TagImage tags an existing image ref with repo and optional tag.
func (c *Client) TagImage(ctx context.Context, ref, repo, tag string) error {
	if tag == "" {
		tag = "latest"
	}
	q := url.Values{}
	q.Set("repo", repo)
	q.Set("tag", tag)
	return c.do(ctx, "POST", "/images/"+ref+"/tag?"+q.Encode(), nil, nil)
}

// PushImage pushes an image ref. Registry auth is passed through
// X-Registry-Auth as a JSON map, matching cardinal's handler.
func (c *Client) PushImage(ctx context.Context, ref, username, password string) error {
	auth := "{}"
	if username != "" {
		b, _ := json.Marshal(map[string]string{"username": username, "password": password})
		auth = string(b)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/images/"+ref+"/push", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Registry-Auth", auth)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("push %s: status %d", ref, resp.StatusCode)
	}
	return nil
}

// dockerHubSearchResult mirrors the Docker Hub search API response.
type dockerHubSearchResult struct {
	Results []struct {
		Name        string `json:"repo_name"`
		Description string `json:"repo_description"`
		StarCount   int    `json:"star_count"`
		PullCount   int    `json:"pull_count"`
	} `json:"results"`
}

// SearchImages queries Docker Hub for images matching a term. This service is
// not part of cardinal's serve API, so wings talks to Docker Hub directly.
func (c *Client) SearchImages(ctx context.Context, term string) ([]Image, error) {
	u := "https://hub.docker.com/v2/search/repositories/?query=" + url.QueryEscape(term) + "&page_size=25"
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search docker hub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search docker hub: status %d", resp.StatusCode)
	}
	var out dockerHubSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	res := make([]Image, 0, len(out.Results))
	for _, r := range out.Results {
		name := r.Name
		if !strings.Contains(name, "/") {
			name = "library/" + name
		}
		res = append(res, Image{RepoTags: []string{name + ":latest"}})
	}
	return res, nil
}
