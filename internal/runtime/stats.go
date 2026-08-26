package runtime

import "context"

// Stats returns current container resource usage (Docker-format JSON).
func (c *Client) Stats(ctx context.Context, id string, out interface{}) error {
	return c.do(ctx, "GET", "/containers/"+id+"/stats?stream=0", nil, out)
}
