package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/moby/moby/client"
)

// resetPrefix removes inner containers and networks whose names start with prefix.
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Inner Docker client.
//   - prefix: Case resource prefix.
//
// Returns:
//   - error: List or remove failure.
func resetPrefix(ctx context.Context, cli *client.Client, prefix string) error {
	if prefix == "" {
		return ErrEmptyResetPrefix
	}

	list, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}

	for _, item := range list.Items {
		if !nameHasPrefix(item.Names, prefix) {
			continue
		}

		_, removeErr := cli.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{
			Force:         true,
			RemoveVolumes: true,
		})
		if removeErr != nil {
			return fmt.Errorf("remove container %s: %w", item.ID, removeErr)
		}
	}

	return nil
}

// nameHasPrefix reports whether any Docker name starts with prefix.
//
// Parameters:
//   - names: Container names, possibly with a leading slash.
//   - prefix: Case resource prefix.
//
// Returns:
//   - bool: True when a name matches.
func nameHasPrefix(names []string, prefix string) bool {
	for _, name := range names {
		trimmed := strings.TrimPrefix(name, "/")
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}

	return false
}
