package selfupdate

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container"
	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// isRecoverableOrphan determines whether a container is a recoverable orphaned
// Watchtower instance. It excludes the current container, old-container names,
// non-Watchtower containers, containers that are not in the created state, and
// containers from a different scope.
//
// Parameters:
//   - c: Candidate container to evaluate.
//   - currentContainer: The current running container to exclude.
//   - currentScope: Normalized scope of the current container ("none" when unset).
//
// Returns:
//   - bool: True if the candidate is a recoverable orphan, false otherwise.
func isRecoverableOrphan(
	c types.Container,
	currentContainer types.Container,
	currentScope string,
) bool {
	if !c.IsWatchtower() {
		return false
	}

	if c.ID() == currentContainer.ID() {
		return false
	}

	if container.IsOldContainer(c.Name()) {
		return false
	}

	if !c.IsCreated() {
		return false
	}

	candidateScope, candidateHasScope := c.Scope()
	if !candidateHasScope || candidateScope == "" {
		candidateScope = "none"
	}

	if candidateScope != currentScope {
		return false
	}

	return true
}

// TryRecoverOrphanedContainer attempts to start an orphaned Watchtower container
// that is stuck in the Docker "created" state. This can happen when a self-update
// creates a replacement container but fails to start it, leaving the old container
// running and the new container never started.
//
// It lists all containers, filters for Watchtower containers in the created state
// that are not the current container, and attempts to start the first match.
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

	currentScope, currentHasScope := currentContainer.Scope()
	if !currentHasScope || currentScope == "" {
		currentScope = "none"
	}

	for _, c := range allContainers {
		if !isRecoverableOrphan(c, currentContainer, currentScope) {
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
