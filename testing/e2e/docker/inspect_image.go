package docker

import (
	"context"

	"github.com/moby/moby/client"
)

// HasImage reports whether the inner daemon already has the named image.
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Inner Docker client.
//   - ref: Image name or tag.
//
// Returns:
//   - bool: True when ImageInspect succeeds.
func HasImage(ctx context.Context, cli *client.Client, ref string) bool {
	if cli == nil || ref == "" {
		return false
	}

	_, err := cli.ImageInspect(ctx, ref)

	return err == nil
}
