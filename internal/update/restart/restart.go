package restart

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/rs/zerolog"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/hooks"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/update/cleanup"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/selfupdate"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
)

// Client recreates containers. It also covers what the lifecycle hooks and the
// self-update need.
type Client interface {
	hooks.Client
	selfupdate.OrchestratorCreator
	selfupdate.RestartPolicySetter

	// GetContainer returns the container with the given ID.
	GetContainer(ctx context.Context, containerID container.ContainerID) (*container.Container, error)
	// StopAndRemoveContainer stops the container and removes it. The stop
	// waits up to timeout.
	StopAndRemoveContainer(ctx context.Context, target *container.Container, timeout time.Duration) error
	// CreateContainer creates a container from the given container's
	// configuration without starting it. It returns the new container's ID.
	CreateContainer(ctx context.Context, target *container.Container) (container.ContainerID, error)
	// RenameContainer gives the container a new name.
	RenameContainer(ctx context.Context, target *container.Container, newName string) error
	// StartContainerByID starts an existing container.
	StartContainerByID(ctx context.Context, containerID container.ContainerID) error
	// WaitForContainerHealthy waits until the container is healthy, for up to
	// timeout. A timeout of zero or less waits without a limit.
	WaitForContainerHealthy(ctx context.Context, containerID container.ContainerID, timeout time.Duration) error
	// SnapshotCopyFiles stores the container's labeled copy files, so they
	// can be written into its replacement.
	SnapshotCopyFiles(ctx context.Context, source *container.Container) error
	// DiscardCopyFiles drops the stored copy files of a container that is not
	// replaced.
	DiscardCopyFiles(containerID container.ContainerID)
}

// selfUpdateRename records how a rename-based self-update renamed the running
// Watchtower container, so a failed replacement can undo it.
type selfUpdateRename struct {
	// originalName is the name the rename freed. It is empty when this restart
	// did not rename the container.
	originalName string
	// oldName is the "watchtower-old-" name the container was given.
	oldName string
	// renamed is true while the container is under its "watchtower-old-" name.
	renamed bool
}

// Errors for restarting containers.
var (
	// errVerifyConfigFailed indicates a failure to verify container configuration for recreation.
	errVerifyConfigFailed = errors.New("failed to verify container configuration")
	// errSkipUpdate signals that a container update should be skipped due to a specific condition (e.g., EX_TEMPFAIL).
	errSkipUpdate = errors.New(
		"skipping container due to pre-update command exit code 75 (EX_TEMPFAIL)",
	)
	// errContainerGone indicates a container no longer existed when the update tried to stop it.
	errContainerGone = errors.New("container no longer exists")
	// errNameNotRestored indicates that a failed self-update left Watchtower under its "watchtower-old-" name.
	errNameNotRestored = errors.New("could not restore Watchtower's container name")
)

const (
	// defaultHealthCheckTimeout defines the default timeout for waiting for container health checks.
	defaultHealthCheckTimeout = 5 * time.Minute

	// defaultCreateStartTimeout defines the minimum timeout for container create
	// and start operations during restart. It guards against slow hosts where
	// Docker API calls can exceed the bound of other detached calls, which is
	// the stop timeout plus 30 seconds.
	defaultCreateStartTimeout = 5 * time.Minute
)

// Run stops and recreates the containers an update session restarts, and
// records in progress each container that was not replaced.
//
// The containers must be in dependency order. With rolling restarts, each
// container is stopped and recreated before the next one. Otherwise every
// stale container is marked for update, then every container stops in reverse
// order before any is recreated in order. A container the session left
// untouched on purpose is recorded as skipped. One example is a container whose
// pre-update hook asked to skip it. Any other failure is recorded as failed.
// The old images of replaced containers are added to cleanupImageInfos.
//
// Parameters:
//   - log: Session logger.
//   - ctx: Context for cancellation and timeouts.
//   - containers: Containers to restart, in dependency order.
//   - client: Container client for Docker operations.
//   - config: Update options controlling restart behavior.
//   - cleanupImageInfos: Images to remove after the session, extended with the
//     old images of replaced containers.
//   - progress: Session progress to update.
//
// Returns:
//   - error: Non-nil when the context is canceled during rolling restarts.
func Run(
	log *zerolog.Logger,
	ctx context.Context,
	containers []*container.Container,
	client Client,
	config updateParams.Params,
	cleanupImageInfos *[]cleanup.RemovedImageInfo,
	progress *state.Progress,
) error {
	if config.RollingRestart {
		failed, err := performRollingRestart(
			log,
			ctx,
			containers,
			client,
			config,
			cleanupImageInfos,
			progress,
		)
		recordRestartOutcomes(log, progress, failed)

		return err
	}

	for _, c := range containers {
		if c.IsStale() {
			progress.MarkForUpdate(log, c.ID())
		}
	}

	failedStop, stoppedImages := stopContainersInReversedOrder(
		log,
		ctx,
		containers,
		client,
		config,
	)
	recordRestartOutcomes(log, progress, failedStop)

	failedStart := restartContainersInSortedOrder(
		log,
		ctx,
		containers,
		client,
		config,
		stoppedImages,
		cleanupImageInfos,
		progress,
	)
	recordRestartOutcomes(log, progress, failedStart)

	return nil
}

// performRollingRestart updates containers with rolling restarts.
//
// It processes containers sequentially in forward order, stopping and restarting each as needed,
// collecting cleaned image info for stale containers only to ensure proper cleanup.
// The function checks for context cancellation at the start of each iteration to enable
// prompt exit when the context is canceled.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - containers: List of containers to update or restart.
//   - client: Container client for Docker operations.
//   - config: Update options controlling restart behavior.
//   - cleanupImageInfos: Pointer to slice to collect cleaned image info for deferred cleanup.
//   - progress: Progress tracker to update with new container IDs.
//
// Returns:
//   - map[container.ContainerID]error: Map of container IDs to errors for failed updates.
//   - error: Non-nil if context was canceled, nil otherwise.
func performRollingRestart(
	log *zerolog.Logger,
	ctx context.Context,
	containers []*container.Container,
	client Client,
	config updateParams.Params,
	cleanupImageInfos *[]cleanup.RemovedImageInfo,
	progress *state.Progress,
) (map[container.ContainerID]error, error) {
	failed := make(map[container.ContainerID]error, len(containers))

	containerNames := make([]string, len(containers))
	for i, c := range containers {
		containerNames[i] = c.Name()
	}

	log.Debug().
		Strs("processing_order", containerNames).
		Msg("Starting performRollingRestart")

	// Process containers in forward order to respect dependency chains.
	for i := range containers {
		// Check for context cancellation to enable prompt exit when context is canceled.
		select {
		case <-ctx.Done():
			// Handle the current container that was not processed due to cancellation.
			c := containers[i]
			log.Info().
				Str("container", c.Name()).
				Str("image", c.ImageName()).
				Str("container_id", c.ID().ShortID()).
				Msg("Skipped container restart due to context cancellation")
			failed[c.ID()] = skipped(fmt.Errorf("restart skipped: %w", ctx.Err()))

			// Handle remaining containers that were not processed due to cancellation.
			for j := i + 1; j < len(containers); j++ {
				untouched := containers[j]
				log.Info().
					Str("container", untouched.Name()).
					Str("image", untouched.ImageName()).
					Str("container_id", untouched.ID().ShortID()).
					Msg("Skipped container restart due to context cancellation")
				failed[untouched.ID()] = skipped(fmt.Errorf("restart skipped: %w", ctx.Err()))
			}

			return failed, fmt.Errorf("rolling restart canceled: %w", ctx.Err())
		default:
		}

		c := containers[i]
		if !c.ToRestart() {
			continue
		}

		fields := map[string]any{
			"container": c.Name(),
			"image":     c.ImageName(),
		}

		log.Debug().
			Fields(fields).
			Msg("Processing container for rolling restart")

		// Mark for update if stale
		if c.IsStale() && progress != nil {
			progress.MarkForUpdate(log, c.ID())
		}

		// Stop the container, handling any errors. A Watchtower container is not
		// stopped here, and restartStaleContainer replaces it.
		_, err := stopStaleContainer(log, ctx, c, client, config)
		if err != nil {
			failed[c.ID()] = err
		} else {
			newContainerID, renamed, err := restartStaleContainer(log,
				ctx,
				c,
				client,
				config,
			)
			if err != nil {
				failed[c.ID()] = err
			} else {
				// Record the recreated container in progress.
				if progress != nil {
					progress.RecordRestart(log, c, newContainerID)
				}

				// Wait for the container to become healthy if it has a health check.
				// An ephemeral self-update returns no container ID, because the
				// orchestrator creates the new container after this process stops.
				if newContainerID != "" {
					waitErr := client.WaitForContainerHealthy(
						ctx,
						newContainerID,
						defaultHealthCheckTimeout,
					)
					if waitErr != nil {
						log.Warn().
							Err(waitErr).
							Fields(fields).
							Msg("Failed to wait for container to become healthy")

						// Don't fail the update, just log the warning
					}
				}

				if c.IsStale() && !renamed {
					// Only collect cleaned image info for stale containers that were not renamed. The new
					// Watchtower container removes a renamed predecessor and its image.
					cleanup.AddImage(
						cleanupImageInfos,
						c.ImageID(),
						c.ImageName(),
						c.Name(),
						c.ID(),
					)

					log.Debug().
						Fields(fields).
						Msg("Updated container")
				}
			}
		}
	}

	return failed, nil
}

// stopContainersInReversedOrder stops containers in reverse order.
//
// It stops each container, tracking stopped images and errors, to prepare for restarts while
// respecting dependency order.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - containers: List of containers to stop.
//   - client: Container client for Docker operations.
//   - config: Update options specifying stop timeout and other behaviors.
//
// Returns:
//   - map[container.ContainerID]error: Map of container IDs to errors for failed stops.
//   - []cleanup.RemovedImageInfo: Slice of cleaned image info for stopped containers.
func stopContainersInReversedOrder(
	log *zerolog.Logger,
	ctx context.Context,
	containers []*container.Container,
	client Client,
	config updateParams.Params,
) (map[container.ContainerID]error, []cleanup.RemovedImageInfo) {
	failed := make(map[container.ContainerID]error, len(containers))
	stopped := make([]cleanup.RemovedImageInfo, 0, len(containers))

	// Stop containers in reverse order to avoid breaking dependencies.
	for i, v := range slices.Backward(containers) {
		c := v

		// Check for context cancellation to avoid additional work when context is canceled.
		// First, log and track the current container, then iterate remaining containers.
		if ctx.Err() != nil {
			// Handle the current container that was not processed due to cancellation.
			log.Info().
				Str("container", c.Name()).
				Str("image", c.ImageName()).
				Str("container_id", c.ID().ShortID()).
				Msg("Skipped container stop due to context cancellation")
			failed[c.ID()] = skipped(fmt.Errorf("stop skipped: %w", ctx.Err()))

			// Handle remaining containers that were not processed due to cancellation.
			for j := i - 1; j >= 0; j-- {
				untouched := containers[j]
				log.Info().
					Str("container", untouched.Name()).
					Str("image", untouched.ImageName()).
					Str("container_id", untouched.ID().ShortID()).
					Msg("Skipped container stop due to context cancellation")
				failed[untouched.ID()] = skipped(fmt.Errorf("stop skipped: %w", ctx.Err()))
			}

			return failed, stopped
		}

		fields := map[string]any{
			"container": c.Name(),
			"image":     c.ImageName(),
		}

		stoppedNow, err := stopStaleContainer(log, ctx, c, client, config)
		if err != nil {
			failed[c.ID()] = err
		} else if stoppedNow {
			stopped = append(
				stopped,
				cleanup.RemovedImageInfo{
					ImageID:       c.ImageID(),
					ContainerID:   c.ID(),
					ImageName:     c.ImageName(),
					ContainerName: c.Name(),
				},
			)

			log.Debug().
				Fields(fields).
				Msg("Stopped container")
		}
	}

	return failed, stopped
}

// recordRestartOutcomes records each container whose stop or restart did not succeed.
//
// A container the update deliberately left untouched is recorded as skipped
// and any other as failed. A container that failed in an earlier phase stays
// failed when a later phase skips it. Each failure is logged once at error
// level as "Could not update container" with the container, its image and the
// error. That line is the failure's only report to notifications.
//
// Parameters:
//   - log: Process logger.
//   - progress: Session progress to update.
//   - outcomes: The error of each container that was not stopped or restarted.
func recordRestartOutcomes(log *zerolog.Logger, progress *state.Progress, outcomes map[container.ContainerID]error) {
	failed := make(map[container.ContainerID]error, len(outcomes))
	skips := make(map[container.ContainerID]error)

	for id, err := range outcomes {
		if isSkip(err) {
			skips[id] = err
		} else {
			failed[id] = err
		}
	}

	progress.UpdateSkipped(log, skips)
	progress.UpdateFailed(log, failed)

	// The restart steps log their own failures at debug. This line reports
	// each failure, in container ID order so notifications list them the same
	// way every time.
	for _, containerID := range slices.Sorted(maps.Keys(failed)) {
		status, ok := (*progress)[containerID]
		if !ok {
			continue
		}

		log.Error().
			Err(failed[containerID]).
			Str("container", status.Name()).
			Str("image", status.ImageName()).
			Msg(logging.ContainerUpdateFailed)
	}
}

// stopStaleContainer stops and removes a container that is marked for restart.
//
// Watchtower containers and containers not marked for restart are left running,
// and the restart phase replaces Watchtower containers itself. When lifecycle
// hooks are enabled, the pre-update hook runs first and can skip the container.
// A container that no longer exists is also skipped, so it is not recreated
// from the scan's inspect data.
//
// Parameters:
//   - log: Logger for stop diagnostics.
//   - ctx: Context for cancellation and timeouts.
//   - c: Container to stop.
//   - client: Container client for Docker operations.
//   - config: Update options specifying stop timeout and lifecycle hooks.
//
// Returns:
//   - bool: True when the container was stopped and removed.
//   - error: A skip when the pre-update hook skips the container or it no longer
//     exists, another error when a step fails, and nil otherwise.
func stopStaleContainer(
	log *zerolog.Logger,
	ctx context.Context,
	c *container.Container,
	client Client,
	config updateParams.Params,
) (bool, error) {
	fields := map[string]any{
		"container": c.Name(),
		"image":     c.ImageName(),
	}

	// Skip Watchtower containers to avoid self-interruption.
	if c.IsWatchtower() {
		log.Debug().
			Fields(fields).
			Msg("Skipping Watchtower container")

		return false, nil
	}

	// Skip containers not marked for restart (e.g., not stale or linked).
	if !c.ToRestart() {
		return false, nil
	}

	log.Debug().
		Fields(fields).
		Msg("Stopping container for restart")

	// Verify configuration for linked containers to ensure restart compatibility.
	if c.IsLinkedToRestarting() {
		err := c.VerifyConfiguration()
		if err != nil {
			log.Debug().
				Err(err).
				Fields(fields).
				Msg("Failed to verify container configuration")

			return false, fmt.Errorf("%w: %w", errVerifyConfigFailed, err)
		}
	}

	// Execute pre-update lifecycle hooks if enabled, checking for skip conditions.
	if config.LifecycleHooks {
		skipUpdate, err := hooks.ExecutePreUpdateCommand(log,
			ctx,
			client,
			c,
			config.LifecycleUID,
			config.LifecycleGID,
		)
		if err != nil {
			log.Debug().
				Err(err).
				Fields(fields).
				Msg("Pre-update command execution failed")

			return false, err //nolint:wrapcheck // The hook's error already names the failed pre-update command.
		}

		if skipUpdate {
			log.Debug().
				Fields(fields).
				Msg("Skipping container due to pre-update exit code 75")

			return false, skipped(errSkipUpdate)
		}
	}

	err := client.SnapshotCopyFiles(ctx, c)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to snapshot copy-file paths")

		return false, fmt.Errorf("snapshot copy-file paths: %w", err)
	}

	// Stop the container with the configured timeout.
	err = client.StopAndRemoveContainer(
		ctx,
		c,
		config.Timeout,
	)
	if err != nil {
		// Nothing recreates the container after a failed stop, so drop its snapshot.
		client.DiscardCopyFiles(c.ID())

		// The container was removed or replaced after the scan. Recreating it
		// from the scan's inspect data would bring back a removed container.
		if cerrdefs.IsNotFound(err) {
			log.Debug().
				Err(err).
				Fields(fields).
				Msg("Container no longer exists, skipping it")

			return false, skipped(fmt.Errorf("%w: %w", errContainerGone, err))
		}

		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to stop container")

		return false, err //nolint:wrapcheck // The client's error already names the failed stop or removal.
	}

	return true, nil
}

// restartContainersInSortedOrder restarts stopped containers.
//
// It restarts containers in dependency order, collecting cleaned image info
// for stale containers that were not renamed during a self-update, and
// tracking any restart failures.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - containers: List of containers to restart.
//   - client: Container client for Docker operations.
//   - config: Update options controlling restart behavior.
//   - stoppedImages: Slice of cleaned image info for previously stopped containers.
//   - cleanupImageInfos: Pointer to slice to collect cleaned image info for deferred cleanup.
//   - progress: Progress tracker to update with new container IDs.
//
// Returns:
//   - map[container.ContainerID]error: Map of container IDs to errors for failed restarts.
func restartContainersInSortedOrder(
	log *zerolog.Logger,
	ctx context.Context,
	containers []*container.Container,
	client Client,
	config updateParams.Params,
	stoppedImages []cleanup.RemovedImageInfo,
	cleanupImageInfos *[]cleanup.RemovedImageInfo,
	progress *state.Progress,
) map[container.ContainerID]error {
	failed := make(map[container.ContainerID]error, len(containers))
	// Track renamed containers to skip cleanup.
	renamedContainers := make(map[container.ContainerID]bool)

	// Restart containers in sorted order to respect dependency chains.
	for i := range containers {
		c := containers[i]

		if !c.ToRestart() {
			continue
		}

		fields := map[string]any{
			"container": c.Name(),
			"image":     c.ImageName(),
		}

		// Check if container was previously stopped by looking in stoppedImages slice.
		wasStopped := false

		for _, sc := range stoppedImages {
			if sc.ContainerID == c.ID() {
				wasStopped = true

				break
			}
		}

		// Skip other Watchtower containers, and every Watchtower container while
		// self-updates are disabled. A Watchtower container restarted with a
		// dependency would otherwise be recreated anyway.
		if c.IsWatchtower() && (config.SkipSelfUpdate ||
			config.CurrentContainerID != "" && c.ID() != config.CurrentContainerID) {
			continue
		}

		// Parent cancellation must not strand containers already removed in the
		// stop phase. restartStaleContainer uses a detached context for create/start,
		// so continue recreating stopped (and self-update) instances. Containers that
		// were never stopped can be skipped safely.
		if ctx.Err() != nil && !c.IsWatchtower() && !wasStopped {
			log.Info().
				Str("container", c.Name()).
				Str("image", c.ImageName()).
				Str("container_id", c.ID().ShortID()).
				Msg("Skipped container restart due to context cancellation")
			failed[c.ID()] = skipped(fmt.Errorf("restart skipped: %w", ctx.Err()))

			continue
		}

		if ctx.Err() != nil && (c.IsWatchtower() || wasStopped) {
			log.Info().
				Fields(fields).
				Str("container_id", c.ID().ShortID()).
				Msg("Parent context canceled. Continuing restart of stopped container")
		}

		// Restart Watchtower containers regardless of stoppedImages, as they are renamed.
		// Otherwise, restart only containers that were previously stopped.
		if c.IsWatchtower() || wasStopped {
			newContainerID, renamed, err := restartStaleContainer(log,
				ctx,
				c,
				client,
				config,
			)
			if err != nil {
				failed[c.ID()] = err
			} else {
				// Record the recreated container in progress.
				if progress != nil {
					progress.RecordRestart(log, c, newContainerID)
				}

				log.Debug().
					Fields(fields).
					Msg("Restarted container")

				if renamed {
					renamedContainers[c.ID()] = true
				}
				// Only collect cleaned image info for stale containers that were
				// not renamed. The new Watchtower container removes a renamed
				// predecessor and its image.
				if c.IsStale() && !renamedContainers[c.ID()] {
					cleanup.AddImage(
						cleanupImageInfos,
						c.ImageID(),
						c.ImageName(),
						c.Name(),
						c.ID(),
					)
				}
			}
		}
	}

	return failed
}

// restartStaleContainer restarts a stale container.
//
// It renames Watchtower containers if applicable, starts a new container,
// and runs post-update hooks.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - sourceContainer: Container to restart.
//   - client: Container client for Docker operations.
//   - config: Update options controlling restart and lifecycle hooks.
//
// Returns:
//   - container.ContainerID: ID of the new container, or empty when a step fails or the ephemeral
//     orchestrator takes over.
//   - bool: True if the Watchtower container is left under its "watchtower-old-" name, false otherwise.
//   - error: Non-nil if restart fails, nil on success.
func restartStaleContainer(
	log *zerolog.Logger,
	ctx context.Context,
	sourceContainer *container.Container,
	client Client,
	config updateParams.Params,
) (container.ContainerID, bool, error) {
	// Create a detached context to survive parent context cancellation.
	// This ensures container cleanup and update operations complete even if the
	// parent context is canceled during the restart process.
	// The create and start calls can be slow on busy hosts. They get the stop
	// timeout plus 30 seconds when that is longer than 5 minutes, and 5 minutes
	// otherwise.
	detachedTimeout := max(config.DetachedTimeout(), defaultCreateStartTimeout)

	detachedCtx, cancelDetached := context.WithTimeout(
		context.Background(),
		detachedTimeout,
	)

	defer cancelDetached()

	fields := map[string]any{
		"container": sourceContainer.Name(),
		"image":     sourceContainer.ImageName(),
	}

	var rename selfUpdateRename

	// Rename Watchtower containers regardless of NoRestart flag,
	// but skip in run-once mode as there's no need to avoid conflicts
	// with a continuously running instance.
	if sourceContainer.IsWatchtower() && !config.RunOnce {
		// Opt-in ephemeral self-update: use a short-lived orchestrator container
		// to perform the transition atomically. The orchestrator handles stopping
		// the old container, creating and starting the new one, and cleanup.
		// StartOrchestrator returns immediately after starting the orchestrator.
		// The orchestrator completes the replacement asynchronously. The current
		// Watchtower process will be stopped by the orchestrator shortly after.
		if config.EphemeralSelfUpdate {
			log.Debug().
				Fields(fields).
				Msg("Using ephemeral self-update")

			_, renamed, err := selfupdate.StartOrchestrator(log,
				ctx,
				client,
				sourceContainer,
				config,
			)
			if err != nil {
				return "", false, err //nolint:wrapcheck // The orchestrator's error already names the step that failed.
			}

			// Skip health check and post-update hooks: the new container's ID is
			// not known to this process, and the orchestrator will stop this process
			// shortly. The new Watchtower instance handles its own lifecycle.
			return "", renamed, nil
		}

		var err error

		rename, err = renameForSelfUpdate(log, ctx, client, sourceContainer, fields)
		if err != nil {
			return "", false, err
		}
	}

	// For Watchtower self-updates, accumulate container ID chain in labels.
	if sourceContainer.IsWatchtower() {
		newChain := sourceContainer.SuccessorChain()

		sourceContainer.SetLabel(labels.ContainerChain, newChain)
		log.Debug().
			Fields(fields).
			Str("container_chain", newChain).
			Msg("Updated container chain label for Watchtower self-update")
	}

	// Create the new container with updated configuration.
	//nolint:contextcheck // Using detached context intentionally to survive parent cancellation
	newContainerID, err := client.CreateContainer(detachedCtx, sourceContainer)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to create container")

		err = rename.restore(log, ctx, client, sourceContainer, config, err)

		return "", rename.renamed, err
	}

	// Start the new container based on restart settings:
	//   - Watchtower containers bypass the NoRestart check
	//   - All containers (including Watchtower) start only if they were running or ReviveStopped is enabled
	if (!config.NoRestart || sourceContainer.IsWatchtower()) && (sourceContainer.IsRunning() || config.ReviveStopped) {
		//nolint:contextcheck // Using detached context intentionally to survive parent cancellation
		err = startReplacement(log, detachedCtx, client, sourceContainer, newContainerID, config, fields)
		if err != nil {
			// The renamed Watchtower container still runs this process, so the
			// host keeps a Watchtower instance. When this restart renamed it, it
			// takes back the original name that the removed replacement held.
			if rename.renamed {
				removeFailedReplacement(log, ctx, client, sourceContainer, newContainerID, config, fields)

				err = rename.restore(log, ctx, client, sourceContainer, config, err)
			}

			return "", rename.renamed, err
		}
	}

	// For renamed Watchtower containers, update restart policy to "no" to prevent auto-restart.
	if rename.renamed {
		log.Debug().
			Fields(fields).
			Msg("Updating restart policy for old Watchtower container")

		//nolint:contextcheck // Using detached context intentionally to survive parent cancellation
		selfupdate.DisableOldRestarts(log, detachedCtx, sourceContainer, client)
	}

	return newContainerID, rename.renamed, nil
}

// renameForSelfUpdate renames the running Watchtower container to its
// "watchtower-old-" name, so its replacement can take the original name.
//
// A container that already has a "watchtower-old-" name, left by an earlier
// self-update, is not renamed again.
//
// Parameters:
//   - log: Process logger.
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker operations.
//   - sourceContainer: The running Watchtower container.
//   - fields: Log fields that name the container and image.
//
// Returns:
//   - selfUpdateRename: How the container was renamed.
//   - error: The client's error when the rename fails.
func renameForSelfUpdate(
	log *zerolog.Logger,
	ctx context.Context,
	client Client,
	sourceContainer *container.Container,
	fields map[string]any,
) (selfUpdateRename, error) {
	targetOldName := container.WatchtowerOldPrefix + sourceContainer.ID().ShortID()

	if container.IsOldContainer(sourceContainer.Name()) {
		log.Debug().
			Fields(fields).
			Str("target_name", targetOldName).
			Msg("Skipping rename of already-renamed Watchtower container")

		return selfUpdateRename{renamed: true}, nil
	}

	err := client.RenameContainer(ctx, sourceContainer, targetOldName)
	if err != nil {
		return selfUpdateRename{}, err //nolint:wrapcheck // The client's error already names the failed rename.
	}

	log.Debug().
		Fields(fields).
		Str("new_name", targetOldName).
		Msg("Renamed Watchtower container")

	return selfUpdateRename{
		originalName: sourceContainer.Name(),
		oldName:      targetOldName,
		renamed:      true,
	}, nil
}

// restore gives the running Watchtower container its original name back
// after a self-update failed to create or start a replacement under that name.
//
// It does nothing when this restart did not rename the container. An instance
// left under the "watchtower-old-" name stops at its next update check or
// restart. A failed rename is therefore added to the self-update's error,
// which the update reports once with the container. A restored name logs only
// at debug.
//
// Parameters:
//   - log: Process logger.
//   - ctx: The self-update's context. The rename keeps its values but not its
//     cancellation or deadline, since the self-update may have run out of time.
//   - client: Container client for Docker operations.
//   - sourceContainer: The running Watchtower container, renamed for the self-update.
//   - config: Update options. The rename gets the detached timeout.
//   - cause: The error that failed the self-update.
//
// Returns:
//   - error: cause, joined with the failed rename when the name was not restored.
func (r *selfUpdateRename) restore(
	log *zerolog.Logger,
	ctx context.Context,
	client Client,
	sourceContainer *container.Container,
	config updateParams.Params,
	cause error,
) error {
	if r.originalName == "" {
		return cause
	}

	renameCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.DetachedTimeout())
	defer cancel()

	err := client.RenameContainer(renameCtx, sourceContainer, r.originalName)
	if err != nil {
		return fmt.Errorf("%w, and %w %s from %s: %w", cause, errNameNotRestored, r.originalName, r.oldName, err)
	}

	r.renamed = false

	log.Debug().
		Str("container", r.originalName).
		Str("old_name", r.oldName).
		Msg("Restored Watchtower container name after a failed self-update")

	return cause
}

// startReplacement starts the replacement container and runs its post-update
// hook.
//
// Parameters:
//   - log: Process logger.
//   - detachedCtx: Context for the start and the post-update hook, which
//     survives the update's cancellation.
//   - client: Container client for Docker operations.
//   - sourceContainer: The container the replacement was created from.
//   - newContainerID: ID of the replacement.
//   - config: Update options controlling lifecycle hooks.
//   - fields: Log fields that name the container and image.
//
// Returns:
//   - error: The client's error when the start fails.
func startReplacement(
	log *zerolog.Logger,
	detachedCtx context.Context,
	client Client,
	sourceContainer *container.Container,
	newContainerID container.ContainerID,
	config updateParams.Params,
	fields map[string]any,
) error {
	log.Debug().
		Fields(fields).
		Msg("Starting container with updated configuration")

	err := client.StartContainerByID(detachedCtx, newContainerID)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to start container")

		return err //nolint:wrapcheck // The client's error already names the failed start.
	}

	log.Info().
		Fields(fields).
		Str("new_id", newContainerID.ShortID()).
		Msg(logging.StartedNewContainer)

	// Run post-update lifecycle hooks for restarting containers if enabled.
	if sourceContainer.ToRestart() && config.LifecycleHooks {
		log.Debug().
			Fields(fields).
			Msg("Executing post-update command")
		hooks.ExecutePostUpdateCommand(log,
			detachedCtx,
			client,
			newContainerID,
			config.LifecycleUID,
			config.LifecycleGID,
		)
	}

	return nil
}

// removeFailedReplacement stops and removes a Watchtower replacement that
// failed to start.
//
// A failure to remove it is logged at debug only.
//
// Parameters:
//   - log: Process logger.
//   - ctx: The update's context. The cleanup keeps its values but gets its own
//     deadline, since the start may have used up the detached context's time.
//   - client: Container client for Docker operations.
//   - sourceContainer: The running Watchtower container the replacement copies.
//   - newContainerID: ID of the failed replacement.
//   - config: Update options. The stop waits up to the stop timeout.
//   - fields: Log fields that name the container and image.
func removeFailedReplacement(
	log *zerolog.Logger,
	ctx context.Context,
	client Client,
	sourceContainer *container.Container,
	newContainerID container.ContainerID,
	config updateParams.Params,
	fields map[string]any,
) {
	log.Debug().
		Fields(fields).
		Str("new_id", newContainerID.ShortID()).
		Msg("Cleaning up failed Watchtower container")

	// The replacement copies the source's configuration, including
	// a stop timeout set on the container, so its stop waits as long.
	cleanupCtx, cleanupCancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		config.DetachedStopTimeout(sourceContainer),
	)
	defer cleanupCancel()

	failedNew, err := client.GetContainer(cleanupCtx, newContainerID)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Str("new_id", newContainerID.ShortID()).
			Msg("Failed to inspect failed Watchtower container for cleanup")

		return
	}

	err = client.StopAndRemoveContainer(cleanupCtx, failedNew, config.Timeout)
	if err != nil && !cerrdefs.IsNotFound(err) {
		log.Debug().
			Err(err).
			Fields(fields).
			Str("new_id", newContainerID.ShortID()).
			Msg("Failed to stop failed Watchtower container")
	}
}
