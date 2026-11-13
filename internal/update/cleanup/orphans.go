package cleanup

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container/labels"
)

// orphanedOrchestratorAge is how long ago an orchestrator must have started
// before startup cleanup treats it as orphaned. An orchestrator that never
// started counts from its creation instead. An orchestrator's handoff is
// bounded by a 5-minute timeout plus short cleanup steps, so a younger one may
// still be replacing a Watchtower container.
const orphanedOrchestratorAge = 10 * time.Minute

// RemoveOrphanedOrchestrators removes any ephemeral orchestrator containers
// that may have persisted due to crashes or unexpected termination.
//
// This is called during startup alongside RemoveExcessWatchtowerInstances to
// ensure a clean state. Orchestrators that are not running are checked too,
// including one that a failed start left in the created state. An orchestrator
// is removed only when it started longer ago than any self-update can take or
// was created that long ago without starting. A younger orchestrator is kept because it may
// still be handing off to a new Watchtower, such as the instance running this
// cleanup or one in another scope. An orchestrator whose start and creation
// times cannot be read is kept too.
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
	client Client,
) (int, error) {
	clogVal := log.With().
		Str("function", "RemoveOrphanedOrchestrators").
		Logger()
	clog := &clogVal

	clog.Debug().Msg("Checking for orphaned ephemeral orchestrator containers")

	orchestrators, err := client.ListLabeledContainers(ctx, labels.EphemeralOrchestrator)
	if err != nil {
		clog.Error().
			Err(err).
			Msg("Failed to list ephemeral orchestrators")

		return 0, fmt.Errorf("failed to list ephemeral orchestrators: %w", err)
	}

	removed := 0

	for _, c := range orchestrators {
		// Keep orchestrators that may still be running a self-update.
		started, ok := orchestratorStartTime(c.ContainerInfo())
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
