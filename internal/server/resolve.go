package server

import (
	"context"
	"time"

	"github.com/animesao/cardinal-wings/internal/runtime"
)

// resolve.go maps panel-facing container refs (name or cardinal ID) to the
// on-disk cardinal container ID.
//
// `cardinal serve` accepts names everywhere (it matches ID, ID prefix and
// name), so proxied endpoints already work with names. The local filesystem
// handlers do not go through serve: beginFMSession and containerDataRoot
// build host paths (overlay/<id>, containers/<id>.json) directly, so they
// need the real cardinal ID. Resolving here lets the panel address containers
// by stable names (e.g. srv-<server-uuid>) without storing cardinal's random
// IDs — names survive reinstalls, IDs do not.
//
// Every resolver falls back to the raw ref when the node is unreachable or
// the ref is already an ID, preserving the previous behaviour exactly.

// resolveTimeout bounds the name→ID lookup so a hung node cannot stall a
// file operation.
const resolveTimeout = 5 * time.Second

// resolveContainerID returns the cardinal container ID for ref using the
// given node client, or ref unchanged when it cannot be resolved.
func resolveContainerID(ctx context.Context, c *runtime.Client, ref string) string {
	if c == nil || ref == "" {
		return ref
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	d, err := c.Inspect(tctx, ref)
	if err != nil || d == nil || d.ID == "" {
		return ref
	}
	return d.ID
}

// resolveLocalID maps a ref through the local node client. All filesystem
// handlers (fm, backup, sftp) operate on the local host's cardinal data dir,
// so the local client is the correct resolver even when the request carries
// ?node= (which only routes the serve-proxied calls).
func resolveLocalID(ref string) string {
	return resolveContainerID(context.Background(), defaultClient, ref)
}
