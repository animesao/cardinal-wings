package runtime

import "context"

// Prune removes unused containers and images
// (cardinal POST /system/prune). Returns cardinal's response.
func (c *Client) Prune(ctx context.Context) (map[string]interface{}, error) {
	var out map[string]interface{}
	if err := c.do(ctx, "POST", "/system/prune", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
