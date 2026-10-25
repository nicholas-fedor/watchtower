package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"

	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/internal/container/recreate"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// errStartContainerFailed indicates a failure to start a newly created container.
var errStartContainerFailed = errors.New("failed to start container")

// startTargetContainer starts a container created from sourceContainer.
//
// It leaves the container stopped when sourceContainer is not running and
// reviveStopped is false. When the start fails, it removes the container if it
// is still stopped.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - api: Docker API client.
//   - clog: Logger with container context fields.
//   - sourceContainer: Source container the new container replaces.
//   - containerID: ID of the created container to start.
//   - reviveStopped: If true, starts the new container even if the source is stopped.
//
// Returns:
//   - error: Non-nil if the start fails, nil on success or when the start is skipped.
func startTargetContainer(
	ctx context.Context,
	api dockerClient.APIClient,
	clog *zerolog.Logger,
	sourceContainer types.Container,
	containerID types.ContainerID,
	reviveStopped bool,
) error {
	if !sourceContainer.IsRunning() && !reviveStopped {
		clog.Debug().
			Str("new_id", string(containerID)).
			Msg("Created container, not starting due to stopped state")

		return nil
	}

	_, err := api.ContainerStart(
		ctx,
		string(containerID),
		dockerClient.ContainerStartOptions{},
	)
	if err != nil {
		clog.Debug().
			Err(err).
			Str("new_id", string(containerID)).
			Msg("Failed to start new container")

		// Start failures from cancellation or transport errors can leave the
		// container state ambiguous. Inspect first and force-remove only when
		// the container is still stopped so a late-started instance is kept.
		cleanupFailedStartContainer(ctx, api, clog, containerID)

		return fmt.Errorf("%w: %w", errStartContainerFailed, err)
	}

	message := "Started new container"
	if sourceContainer.IsLinkedToRestarting() {
		message = "Started linked container"
	}

	clog.Debug().
		Str("new_id", containerID.ShortID()).
		Msg(message)

	return nil
}

// cleanupFailedStartContainer inspects a container that failed to start and
// force-removes it only when it is still stopped. Cancellation or transport
// failures can leave state ambiguous. A late-started container is left alone.
//
// Parameters:
//   - parentCtx: Parent context. Deadlines are detached so cleanup can finish.
//   - api: Interface for container operations.
//   - clog: Logger with container context fields.
//   - containerID: ID of the container created before the start failure.
func cleanupFailedStartContainer(
	parentCtx context.Context,
	api dockerClient.APIClient,
	clog *zerolog.Logger,
	containerID types.ContainerID,
) {
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(parentCtx),
		recreate.CleanupTimeout,
	)
	defer cancel()

	inspectResult, inspectErr := api.ContainerInspect(
		cleanupCtx,
		string(containerID),
		dockerClient.ContainerInspectOptions{},
	)
	if inspectErr != nil {
		clog.Debug().
			Err(inspectErr).
			Str("new_id", string(containerID)).
			Msg("Failed to inspect container after start error")

		return
	}

	state := inspectResult.Container.State
	if state != nil && state.Running {
		clog.Debug().
			Str("new_id", string(containerID)).
			Msg("Skipped cleanup of running container after start error")

		return
	}

	_, rmErr := api.ContainerRemove(
		cleanupCtx,
		string(containerID),
		dockerClient.ContainerRemoveOptions{Force: true},
	)
	if rmErr != nil {
		clog.Debug().
			Err(rmErr).
			Str("new_id", string(containerID)).
			Msg("Failed to clean up container after start error")
	}
}
