package update

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/rs/zerolog"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container"
	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	"github.com/nicholas-fedor/watchtower/internal/container/hooks"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/update/selfupdate"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for restarting containers.
var (
	// errVerifyConfigFailed indicates a failure to verify container configuration for recreation.
	errVerifyConfigFailed = errors.New("failed to verify container configuration")
	// errPreUpdateFailed indicates a failure in the pre-update lifecycle command execution.
	errPreUpdateFailed = errors.New("pre-update command failed")
	// errSkipUpdate signals that a container update should be skipped due to a specific condition (e.g., EX_TEMPFAIL).
	errSkipUpdate = errors.New(
		"skipping container due to pre-update command exit code 75 (EX_TEMPFAIL)",
	)
	// errStopContainerFailed indicates a failure to stop a container during the update process.
	errStopContainerFailed = errors.New("failed to stop container")
	// errContainerGone indicates a container no longer existed when the update tried to stop it.
	errContainerGone = errors.New("container no longer exists")
	// errStartContainerFailed indicates a failure to start a container after an update.
	errStartContainerFailed = errors.New("failed to start container")
	// errCreateContainerFailed indicates a failure to create a container during the update process.
	errCreateContainerFailed = errors.New("failed to create container")
	// errRenameWatchtowerFailed indicates a failure to rename the Watchtower container before restarting.
	errRenameWatchtowerFailed = errors.New("failed to rename Watchtower container")
)

const (
	// defaultHealthCheckTimeout defines the default timeout for waiting for container health checks.
	defaultHealthCheckTimeout = 5 * time.Minute

	// defaultRestartPolicyTimeout defines the fallback timeout for restart policy
	// updates when config.Timeout is non-positive.
	defaultRestartPolicyTimeout = 30 * time.Second

	// defaultCreateStartTimeout defines the minimum timeout for container create
	// and start operations during restart. It guards against slow hosts where
	// Docker API calls can exceed the default 30s restart policy timeout.
	defaultCreateStartTimeout = 5 * time.Minute
)

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
//   - map[types.ContainerID]error: Map of container IDs to errors for failed updates.
//   - error: Non-nil if context was canceled, nil otherwise.
func performRollingRestart(
	log *zerolog.Logger,
	ctx context.Context,
	containers []types.Container,
	client containerClient.Client,
	config types.UpdateParams,
	cleanupImageInfos *[]types.RemovedImageInfo,
	progress *state.Progress,
	gitSession *gitSession,
) (map[types.ContainerID]error, error) {
	failed := make(map[types.ContainerID]error, len(containers))

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

		if gitSession.skipRecreate(c, config) {
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
				// Set the new container ID in progress
				if progress != nil {
					status, exists := (*progress)[c.ID()]
					if exists {
						status.SetNewContainerID(newContainerID)
						// Mark as restarted if not stale (not updated)
						if !c.IsStale() {
							progress.MarkRestarted(log, c.ID())
						}
					}
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
					// Only collect cleaned image info for stale containers that were not renamed, as renamed
					// containers (Watchtower self-updates) are cleaned up by RemoveExcessWatchtowerInstances
					// in the new container.
					addCleanupImageInfo(
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
//   - map[types.ContainerID]error: Map of container IDs to errors for failed stops.
//   - []types.RemovedImageInfo: Slice of cleaned image info for stopped containers.
func stopContainersInReversedOrder(
	log *zerolog.Logger,
	ctx context.Context,
	containers []types.Container,
	client containerClient.Client,
	config types.UpdateParams,
) (map[types.ContainerID]error, []types.RemovedImageInfo) {
	failed := make(map[types.ContainerID]error, len(containers))
	stopped := make([]types.RemovedImageInfo, 0, len(containers))

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
				types.RemovedImageInfo{
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

// recordRestartOutcomes records the containers whose stop or restart did not
// succeed: as skipped when the update deliberately left them untouched, and as
// failed otherwise. A container that already failed in an earlier phase stays
// failed, even when a later phase skips it.
//
// Parameters:
//   - log: Process logger.
//   - progress: Session progress to update.
//   - outcomes: The error of each container that was not stopped or restarted.
func recordRestartOutcomes(log *zerolog.Logger, progress *state.Progress, outcomes map[types.ContainerID]error) {
	failed := make(map[types.ContainerID]error, len(outcomes))
	skips := make(map[types.ContainerID]error)

	for id, err := range outcomes {
		if isSkip(err) {
			skips[id] = err
		} else {
			failed[id] = err
		}
	}

	progress.UpdateSkipped(log, skips)
	progress.UpdateFailed(log, failed)
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
//   - container: Container to stop.
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
	container types.Container,
	client containerClient.Client,
	config types.UpdateParams,
) (bool, error) {
	fields := map[string]any{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	// Skip Watchtower containers to avoid self-interruption.
	if container.IsWatchtower() {
		log.Debug().
			Fields(fields).
			Msg("Skipping Watchtower container")

		return false, nil
	}

	// Skip containers not marked for restart (e.g., not stale or linked).
	if !container.ToRestart() {
		return false, nil
	}

	log.Debug().
		Fields(fields).
		Msg("Stopping container for restart")

	// Verify configuration for linked containers to ensure restart compatibility.
	if container.IsLinkedToRestarting() {
		err := container.VerifyConfiguration()
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
			container,
			config.LifecycleUID,
			config.LifecycleGID,
		)
		if err != nil {
			log.Debug().
				Err(err).
				Fields(fields).
				Msg("Pre-update command execution failed")

			return false, fmt.Errorf("%w: %w", errPreUpdateFailed, err)
		}

		if skipUpdate {
			log.Debug().
				Fields(fields).
				Msg("Skipping container due to pre-update exit code 75")

			return false, skipped(errSkipUpdate)
		}
	}

	err := snapshotCopyFilesForRecreate(ctx, client, container)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to snapshot copy-file paths")

		return false, err
	}

	// Stop the container with the configured timeout.
	err = client.StopAndRemoveContainer(
		ctx,
		container,
		config.Timeout,
	)
	if err != nil {
		// Nothing recreates the container after a failed stop, so drop its snapshot.
		discardCopyFilesForRecreate(client, container.ID())

		// The container was removed or replaced after the scan. Recreating it
		// from the scan's inspect data would bring back a removed container.
		if cerrdefs.IsNotFound(err) {
			log.Debug().
				Err(err).
				Fields(fields).
				Msg("Container no longer exists, skipping it")

			return false, skipped(fmt.Errorf("%w: %w", errContainerGone, err))
		}

		log.Error().
			Err(err).
			Fields(fields).
			Msg("Failed to stop container")

		return false, fmt.Errorf("%w: %w", errStopContainerFailed, err)
	}

	return true, nil
}

// snapshotCopyFilesForRecreate stores labeled files before the source is removed.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: Docker client. No-op unless it implements container.CopyFileStore.
//   - source: Container about to be stopped and removed.
//
// Returns:
//   - error: Non-nil if a labeled path cannot be snapshotted.
func snapshotCopyFilesForRecreate(
	ctx context.Context,
	client containerClient.Client,
	source types.Container,
) error {
	store, ok := client.(containerClient.CopyFileStore)
	if !ok {
		return nil
	}

	err := store.SnapshotCopyFiles(ctx, source)
	if err != nil {
		return fmt.Errorf("snapshot copy-file paths: %w", err)
	}

	return nil
}

// discardCopyFilesForRecreate drops a leftover snapshot after a failed stop.
//
// Parameters:
//   - client: Docker client. No-op unless it implements container.CopyFileStore.
//   - containerID: Source container ID whose snapshot should be discarded.
func discardCopyFilesForRecreate(client containerClient.Client, containerID types.ContainerID) {
	store, ok := client.(containerClient.CopyFileStore)
	if !ok {
		return
	}

	store.DiscardCopyFiles(containerID)
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
//   - map[types.ContainerID]error: Map of container IDs to errors for failed restarts.
func restartContainersInSortedOrder(
	log *zerolog.Logger,
	ctx context.Context,
	containers []types.Container,
	client containerClient.Client,
	config types.UpdateParams,
	stoppedImages []types.RemovedImageInfo,
	cleanupImageInfos *[]types.RemovedImageInfo,
	progress *state.Progress,
	gitSession *gitSession,
) map[types.ContainerID]error {
	failed := make(map[types.ContainerID]error, len(containers))
	// Track renamed containers to skip cleanup.
	renamedContainers := make(map[types.ContainerID]bool)

	// Restart containers in sorted order to respect dependency chains.
	for i := range containers {
		c := containers[i]

		if !c.ToRestart() {
			continue
		}

		if gitSession.skipRecreate(c, config) {
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
				// Set the new container ID in progress
				if progress != nil {
					status, exists := (*progress)[c.ID()]
					if exists {
						status.SetNewContainerID(newContainerID)
						// Mark as restarted if not stale (not updated)
						if !c.IsStale() {
							progress.MarkRestarted(log, c.ID())
						}
					}
				}

				log.Debug().
					Fields(fields).
					Msg("Restarted container")

				if renamed {
					renamedContainers[c.ID()] = true
				}
				// Only collect cleaned image info for stale containers that were
				// not renamed, as renamed containers (Watchtower self-updates)
				// are cleaned up by RemoveExcessWatchtowerInstances
				// in the new container.
				if c.IsStale() && !renamedContainers[c.ID()] {
					addCleanupImageInfo(
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

// addCleanupImageInfo adds cleanup info if not already present.
//
// Deduplication is based on the (ImageID, ContainerName) pair so that when
// notifications are split by container, each container that used the old image
// receives its own "Removing image" entry.
//
// Parameters:
//   - cleanupImageInfos: Pointer to slice to collect cleaned image info.
//   - imageID: ID of the image to clean up.
//   - imageName: Name of the image.
//   - containerName: Name of the container.
//   - containerID: ID of the container (optional, pass empty string if not available).
func addCleanupImageInfo(
	cleanupImageInfos *[]types.RemovedImageInfo,
	imageID types.ImageID,
	imageName, containerName string,
	containerID types.ContainerID,
) {
	for _, existing := range *cleanupImageInfos {
		if existing.ImageID == imageID && existing.ContainerName == containerName {
			return
		}
	}

	*cleanupImageInfos = append(*cleanupImageInfos, types.RemovedImageInfo{
		ImageID:       imageID,
		ContainerID:   containerID,
		ImageName:     imageName,
		ContainerName: containerName,
	})
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
//   - types.ContainerID: ID of the new container if started, original ID if renamed only, empty otherwise.
//   - bool: True if the container was renamed, false otherwise.
//   - error: Non-nil if restart fails, nil on success.
func restartStaleContainer(
	log *zerolog.Logger,
	ctx context.Context,
	sourceContainer types.Container,
	client containerClient.Client,
	config types.UpdateParams,
) (types.ContainerID, bool, error) {
	// Create a detached context to survive parent context cancellation.
	// This ensures container cleanup and update operations complete even if the
	// parent context is canceled during the restart process.
	// A finite fallback is applied when config.Timeout is non-positive, and the
	// create/start phase uses at least defaultCreateStartTimeout to accommodate
	// slow hosts.
	detachedTimeout := max(restartPolicyTimeout(config.Timeout), defaultCreateStartTimeout)

	detachedCtx, cancelDetached := context.WithTimeout(
		context.Background(),
		detachedTimeout,
	)

	defer cancelDetached()

	fields := map[string]any{
		"container": sourceContainer.Name(),
		"image":     sourceContainer.ImageName(),
	}

	var (
		renamed                bool
		originalWatchtowerName string
	)

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
				return "", false, fmt.Errorf("start self-update orchestrator: %w", err)
			}

			// Skip health check and post-update hooks: the new container's ID is
			// not known to this process, and the orchestrator will stop this process
			// shortly. The new Watchtower instance handles its own lifecycle.
			return "", renamed, nil
		}

		targetOldName := types.WatchtowerOldPrefix + sourceContainer.ID().ShortID()

		// Redundant rename guard: the lingering old instance already has the
		// target name from a prior rename. Skip to avoid a same-name error.
		if container.IsOldContainer(sourceContainer.Name()) {
			log.Debug().
				Fields(fields).
				Str("target_name", targetOldName).
				Msg("Skipping rename of already-renamed Watchtower container")

			renamed = true
		} else {
			originalWatchtowerName = sourceContainer.Name()
			newName := targetOldName

			err := client.RenameContainer(
				ctx,
				sourceContainer,
				newName,
			)
			if err != nil {
				log.Debug().
					Err(err).
					Str("container", sourceContainer.Name()).
					Str("new_name", newName).
					Msg("Failed to rename Watchtower container")

				return "",
					false,
					fmt.Errorf(
						"%w: %w",
						errRenameWatchtowerFailed,
						err,
					)
			}

			log.Debug().
				Fields(fields).
				Str("new_name", newName).
				Msg("Renamed Watchtower container")

			renamed = true
		}
	}

	// For Watchtower self-updates, accumulate container ID chain in labels.
	if sourceContainer.IsWatchtower() {
		c, ok := sourceContainer.(*container.Container)
		if ok {
			containerInfo := c.ContainerInfo()
			if containerInfo != nil && containerInfo.Config != nil {
				existingChain, _ := c.GetContainerChain()

				var newChain string
				if existingChain != "" {
					newChain = existingChain + "," + string(c.ID())
				} else {
					newChain = string(c.ID())
				}

				c.SetLabel(labels.ContainerChain, newChain)
				log.Debug().
					Fields(fields).
					Str("container_chain", newChain).
					Msg("Updated container chain label for Watchtower self-update")
			}
		}
	}

	// Create the new container with updated configuration.
	//nolint:contextcheck // Using detached context intentionally to survive parent cancellation
	newContainerID, err := client.CreateContainer(detachedCtx, sourceContainer)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to create container")

		// Restore the original name so the running instance is not left only as
		// watchtower-old-* with the canonical name free and unused.
		// Use a fresh context here: detachedCtx may already be expired if the
		// create call hit the timeout, and we do not want recovery to fail for
		// the same reason.
		if renamed && originalWatchtowerName != "" && sourceContainer.IsWatchtower() {
			renameBackCtx, renameBackCancel := context.WithTimeout(
				context.Background(),
				restartPolicyTimeout(config.Timeout),
			)
			defer renameBackCancel()

			//nolint:contextcheck // fresh context is intentional for recovery after timeout
			renameBackErr := client.RenameContainer(
				renameBackCtx,
				sourceContainer,
				originalWatchtowerName,
			)
			if renameBackErr != nil {
				log.Debug().
					Err(renameBackErr).
					Fields(fields).
					Str("original_name", originalWatchtowerName).
					Msg("Failed to rename Watchtower container back after create failure")
			} else {
				renamed = false

				log.Debug().
					Fields(fields).
					Str("original_name", originalWatchtowerName).
					Msg("Restored Watchtower container name after create failure")
			}
		}

		return "",
			renamed,
			fmt.Errorf(
				"%w: %w",
				errCreateContainerFailed,
				err,
			)
	}

	// Start the new container based on restart settings:
	//   - Watchtower containers bypass the NoRestart check
	//   - All containers (including Watchtower) start only if they were running or ReviveStopped is enabled
	if (!config.NoRestart || sourceContainer.IsWatchtower()) && (sourceContainer.IsRunning() || config.ReviveStopped) {
		log.Debug().
			Fields(fields).
			Msg("Starting container with updated configuration")

		//nolint:contextcheck // Using detached context intentionally to survive parent cancellation
		err = client.StartContainerByID(detachedCtx, newContainerID)
		if err != nil {
			log.Debug().
				Err(err).
				Fields(fields).
				Msg("Failed to start container")

			// On start failure after a Watchtower rename, remove the failed replacement
			// container only. The renamed source still holds the running process and must
			// remain so the host keeps a Watchtower instance.
			// Use a fresh context because detachedCtx may already be expired if the start
			// call hit the timeout and we do not want cleanup to fail for the same reason.
			if renamed && sourceContainer.IsWatchtower() {
				log.Debug().
					Fields(fields).
					Str("new_id", newContainerID.ShortID()).
					Msg("Cleaning up failed Watchtower container")

				cleanupCtx, cleanupCancel := context.WithTimeout(
					context.Background(),
					restartPolicyTimeout(config.Timeout),
				)
				defer cleanupCancel()

				//nolint:contextcheck // fresh context is intentional for cleanup after timeout
				failedNew, getErr := client.GetContainer(cleanupCtx, newContainerID)
				if getErr != nil {
					log.Debug().
						Err(getErr).
						Fields(fields).
						Str("new_id", newContainerID.ShortID()).
						Msg("Failed to inspect failed Watchtower container for cleanup")
				} else {
					//nolint:contextcheck // fresh context is intentional for cleanup after timeout
					cleanupErr := client.StopAndRemoveContainer(
						cleanupCtx,
						failedNew,
						config.Timeout,
					)
					if cleanupErr != nil && !cerrdefs.IsNotFound(cleanupErr) {
						log.Debug().
							Err(cleanupErr).
							Fields(fields).
							Str("new_id", newContainerID.ShortID()).
							Msg("Failed to stop failed Watchtower container")
					}
				}
			}

			return "",
				renamed,
				fmt.Errorf(
					"%w: %w",
					errStartContainerFailed,
					err,
				)
		}

		log.Info().
			Fields(fields).
			Str("new_id", newContainerID.ShortID()).
			Msg("Started new container")

		// Run post-update lifecycle hooks for restarting containers if enabled.
		if sourceContainer.ToRestart() && config.LifecycleHooks {
			log.Debug().
				Fields(fields).
				Msg("Executing post-update command")
			//nolint:contextcheck // Using detached context intentionally to survive parent cancellation
			hooks.ExecutePostUpdateCommand(log,
				detachedCtx,
				client,
				newContainerID,
				config.LifecycleUID,
				config.LifecycleGID,
			)
		}
	}

	// For renamed Watchtower containers, update restart policy to "no" to prevent auto-restart.
	if renamed && sourceContainer.IsWatchtower() {
		log.Debug().
			Fields(fields).
			Msg("Updating restart policy for old Watchtower container")

		//nolint:contextcheck // Using detached context intentionally to survive parent cancellation
		disableOldWatchtowerRestarts(log, detachedCtx, sourceContainer, client)
	}

	return newContainerID, renamed, nil
}

// restartPolicyTimeout returns the timeout to use for restart policy operations.
// If the provided timeout is positive, it is returned as-is.
// Otherwise, a finite fallback is used to prevent indefinite blocking.
func restartPolicyTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}

	return defaultRestartPolicyTimeout
}

// disableOldWatchtowerRestarts sets the restart policy of an old Watchtower
// container to "no", so the runtime does not start it again.
//
// A failure is logged at debug only. An old Watchtower container that starts
// again detects that it is old and exits, and the instance that replaced it
// removes it.
//
// Parameters:
//   - log: Logger for the failure.
//   - ctx: Context for the restart policy update.
//   - oldContainer: The old Watchtower container.
//   - client: Container client for Docker operations.
func disableOldWatchtowerRestarts(
	log *zerolog.Logger,
	ctx context.Context,
	oldContainer types.Container,
	client containerClient.Client,
) {
	err := client.SetRestartPolicy(
		ctx,
		oldContainer.ID(),
		dockerContainer.RestartPolicy{Name: dockerContainer.RestartPolicyDisabled},
	)
	if err != nil {
		log.Debug().
			Err(err).
			Str("container", oldContainer.Name()).
			Msg("Failed to disable the restart policy of the old Watchtower container")
	}
}
