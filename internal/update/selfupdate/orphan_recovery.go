package selfupdate

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container"
	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TryRecoverOrphanedContainer attempts to start an orphaned Watchtower container
// that is stuck in the Docker "created" state. This can happen when a self-update
// creates a replacement container but fails to start it, leaving the old container
// running and the new container never started.
//
// It lists all containers and starts the first one that
// [container.IsOrphanedReplacement] reports as an orphaned replacement of the
// current container.
//
// Parameters:
//   - log: Process logger. Required and must be non-nil. A nil logger panics on the first log call.
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker API operations.
//   - currentContainer: The current running container to exclude from recovery.
//
// Returns:
//   - types.Container: The recovered container if successful, nil otherwise.
//   - bool: True if a container was found and started, false otherwise.
func TryRecoverOrphanedContainer(
	log *zerolog.Logger,
	ctx context.Context,
	client containerClient.Client,
	currentContainer types.Container,
) (types.Container, bool) {
	if currentContainer == nil {
		return nil, false
	}

	allContainers, err := client.ListContainers(ctx, filters.NoFilter)
	if err != nil {
		log.Debug().
			Err(err).
			Msg("Failed to list containers for orphaned Watchtower recovery")

		return nil, false
	}

	for _, c := range allContainers {
		if !container.IsOrphanedReplacement(c, currentContainer) {
			continue
		}

		log.Info().
			Str("container_id", string(c.ID())).
			Str("container_name", c.Name()).
			Msg("Attempting to recover orphaned Watchtower container")

		err := client.StartContainerByID(ctx, c.ID())
		if err != nil {
			log.Debug().
				Err(err).
				Str("container_id", string(c.ID())).
				Str("container_name", c.Name()).
				Msg("Failed to start orphaned Watchtower container")

			continue
		}

		log.Info().
			Str("container_id", string(c.ID())).
			Str("container_name", c.Name()).
			Msg("Successfully recovered orphaned Watchtower container")

		return c, true
	}

	return nil, false
}
