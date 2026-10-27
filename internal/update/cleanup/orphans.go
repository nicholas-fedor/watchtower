package cleanup

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/moby/moby/api/types/container"

	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
)

// orphanedOrchestratorAge is how long an orchestrator must have been running
// before startup cleanup treats it as orphaned. An orchestrator's handoff is
// bounded by a 5-minute timeout plus short cleanup steps, so a younger one
// may still be replacing a Watchtower container.
const orphanedOrchestratorAge = 10 * time.Minute

// RemoveOrphanedOrchestrators removes any ephemeral orchestrator containers
// that may have persisted due to crashes or unexpected termination.
//
// This is called during startup alongside RemoveExcessWatchtowerInstances to
// ensure a clean state. Only orchestrators running longer than any self-update
// can take are removed. A younger orchestrator may still be handing off to a
// new Watchtower, including the instance running this cleanup or an instance
// in another scope, so it is kept, as is one whose start time cannot be read.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: Container client for Docker operations.
//
// Returns:
//   - int: Number of orphaned orchestrators removed.
//   - error: Non-nil if listing or removal fails, nil on success.
func RemoveOrphanedOrchestrators(log *zerolog.Logger,
	ctx context.Context,
	client containerClient.Client,
) (int, error) {
	clogVal := log.With().
		Str("function", "RemoveOrphanedOrchestrators").
		Logger()
	clog := &clogVal

	clog.Debug().Msg("Checking for orphaned ephemeral orchestrator containers")

	// List all containers to find orphaned orchestrators.
	allContainers, err := client.ListContainers(ctx)
	if err != nil {
		clog.Error().
			Err(err).
			Msg("Failed to list containers for orchestrator cleanup")

		return 0, fmt.Errorf("failed to list containers: %w", err)
	}

	removed := 0

	for _, c := range allContainers {
		containerInfo := c.ContainerInfo()
		if containerInfo == nil || containerInfo.Config == nil {
			continue
		}

		// Check for the orchestrator label.
		if containerInfo.Config.Labels[labels.EphemeralOrchestrator] != "true" {
			continue
		}

		// Keep orchestrators that may still be running a self-update.
		started, ok := orchestratorStartTime(containerInfo)
		if !ok || time.Since(started) < orphanedOrchestratorAge {
			clog.Debug().
				Str("container", c.Name()).
				Str("id", c.ID().ShortID()).
				Bool("start_time_known", ok).
				Msg("Skipping ephemeral orchestrator that may still be running a self-update")

			continue
		}

		clog.Info().
			Str("container", c.Name()).
			Str("id", c.ID().ShortID()).
			Msg("Removing orphaned ephemeral orchestrator container")

		err := client.StopAndRemoveContainer(ctx, c, 0)
		if cerrdefs.IsNotFound(err) {
			clog.Debug().
				Str("container", c.Name()).
				Str("id", c.ID().ShortID()).
				Msg("Orphaned orchestrator container is already gone")

			continue
		}

		if err != nil {
			clog.Warn().
				Err(err).
				Str("container", c.Name()).
				Str("id", c.ID().ShortID()).
				Msg("Failed to remove orphaned orchestrator container")

			continue
		}

		removed++
	}

	if removed > 0 {
		clog.Info().
			Int("count", removed).
			Msg("Removed orphaned ephemeral orchestrator containers")
	} else {
		clog.Debug().Msg("No orphaned ephemeral orchestrator containers found")
	}

	return removed, nil
}

// orchestratorStartTime returns when an orchestrator container started, falling
// back to its creation time when Docker reports no start time.
//
// Parameters:
//   - info: Inspect data of the orchestrator container.
//
// Returns:
//   - time.Time: The start or creation time.
//   - bool: False when neither time can be read.
func orchestratorStartTime(info *dockerContainer.InspectResponse) (time.Time, bool) {
	if info.State != nil {
		started, err := time.Parse(time.RFC3339Nano, info.State.StartedAt)
		if err == nil && !started.IsZero() {
			return started, true
		}
	}

	created, err := time.Parse(time.RFC3339Nano, info.Created)
	if err == nil && !created.IsZero() {
		return created, true
	}

	return time.Time{}, false
}
