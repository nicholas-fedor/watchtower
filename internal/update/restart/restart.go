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
	selfupdate.RenameClient

	// StopAndRemoveContainer stops the container and removes it. The stop
	// waits up to timeout.
	StopAndRemoveContainer(ctx context.Context, target *container.Container, timeout time.Duration) error
	// CreateContainer creates a container from the given container's
	// configuration without starting it. It returns the new container's ID.
	CreateContainer(ctx context.Context, target *container.Container) (container.ContainerID, error)
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
)

const (
	// defaultHealthCheckTimeout defines the default timeout for waiting for container health checks.
	defaultHealthCheckTimeout = 5 * time.Minute
)

// Run stops and recreates the containers an update session restarts, and
// records in progress each container that was not replaced.
//
// The containers must be in dependency order. With rolling restarts, each
// container is stopped and recreated before the next one. Otherwise every
// stale container is marked for update, then every container stops in reverse
// order before any is recreated in order. A container that others join is
// replaced before they are removed, so a failed replacement leaves them
// joined to the restored container. Without rolling restarts, this needs a
// host config that names no container earlier in the list. A container the
// session left untouched on purpose is recorded as skipped. One example is a container whose
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
		recordRestartOutcomes(log, progress, containers, failed, config)

		return err
	}

	for _, c := range containers {
		if c.IsStale() {
			progress.MarkForUpdate(log, c.ID())
		}
	}

	failedStop, stopped := stopContainersInReversedOrder(
		log,
		ctx,
		containers,
		client,
		config,
	)
	recordRestartOutcomes(log, progress, containers, failedStop, config)

	failedStart := restartContainersInSortedOrder(
		log,
		ctx,
		containers,
		client,
		config,
		stopped,
		cleanupImageInfos,
		progress,
	)
	recordRestartOutcomes(log, progress, containers, failedStart, config)

	return nil
}

// performRollingRestart updates containers with rolling restarts.
//
// It processes containers sequentially in forward order, stopping and restarting each as needed,
// collecting cleaned image info for stale containers only to ensure proper cleanup.
// A container is stopped and recreated with the rest of its group, the rebound
// containers that join its network namespace and the containers placed
// between them, so a cancellation never leaves one of them removed.
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

	groups := networkGroups(containers)

	// Process containers in forward order to respect dependency chains.
	for g, group := range groups {
		// Check for context cancellation to enable prompt exit when context is canceled.
		if ctx.Err() != nil {
			for _, remaining := range groups[g:] {
				for _, untouched := range remaining {
					log.Info().
						Str("container", untouched.Name()).
						Str("image", untouched.ImageName()).
						Str("container_id", untouched.ID().ShortID()).
						Msg("Skipped container restart due to context cancellation")
					failed[untouched.ID()] = skipped(fmt.Errorf("restart skipped: %w", ctx.Err()))
				}
			}

			return failed, fmt.Errorf("rolling restart canceled: %w", ctx.Err())
		}

		if !group[0].ToRestart() {
			continue
		}

		for _, c := range group {
			if c.IsStale() && progress != nil {
				progress.MarkForUpdate(log, c.ID())
			}
		}

		// The groups before this one are recreated already, so none can hold
		// a container this group's create config names.
		stopped := make(map[container.ContainerID]container.ContainerID, len(group))
		stopGroup(log, ctx, group, nil, client, config, failed, stopped)

		for _, c := range group {
			created, wasStopped := stopped[c.ID()]
			if !recreates(c, wasStopped) {
				continue
			}

			err := replaceContainer(log, ctx, c, created, client, config, cleanupImageInfos, progress)
			if err != nil {
				failed[c.ID()] = err
			}
		}
	}

	return failed, nil
}

// replaceContainer recreates and starts one container of a rolling restart,
// then waits for it to become healthy.
//
// Parameters:
//   - log: Process logger.
//   - ctx: Context for cancellation and timeouts.
//   - c: Container to replace, already stopped unless it is Watchtower's own.
//   - created: The replacement when it was created before c was stopped, or
//     empty to create it.
//   - client: Container client for Docker operations.
//   - config: Update options controlling restart behavior.
//   - cleanupImageInfos: Collects the image of an updated container for cleanup.
//   - progress: Progress tracker to update with new container IDs.
//
// Returns:
//   - error: Non-nil when the container could not be recreated or started.
func replaceContainer(
	log *zerolog.Logger,
	ctx context.Context,
	c *container.Container,
	created container.ContainerID,
	client Client,
	config updateParams.Params,
	cleanupImageInfos *[]cleanup.RemovedImageInfo,
	progress *state.Progress,
) error {
	fields := map[string]any{
		"container": c.Name(),
		"image":     c.ImageName(),
	}

	log.Debug().
		Fields(fields).
		Msg("Processing container for rolling restart")

	newContainerID, renamed, err := restartStaleContainer(log,
		ctx,
		c,
		created,
		client,
		config,
	)
	if err != nil {
		return err
	}

	recordReplacement(log, c, newContainerID, renamed, cleanupImageInfos, progress)

	// Wait for the container to become healthy if it has a health check.
	// An ephemeral self-update returns no container ID, because the
	// orchestrator creates the new container after this process stops. A
	// replacement left stopped runs no health check.
	if newContainerID != "" && replacementStarts(c, config) {
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

	return nil
}

// recordReplacement records a container's replacement in progress and adds
// the old image of an updated container to the images to remove.
//
// A Watchtower container left under its old name keeps its image, because the
// new instance removes that container and its image itself.
//
// Parameters:
//   - log: Session logger.
//   - c: The container that was replaced.
//   - newContainerID: ID of the replacement, empty after an ephemeral
//     self-update handoff.
//   - renamed: Whether c is a Watchtower container left under its old name.
//   - cleanupImageInfos: Images to remove after the session.
//   - progress: Session progress, nil to record nothing.
func recordReplacement(
	log *zerolog.Logger,
	c *container.Container,
	newContainerID container.ContainerID,
	renamed bool,
	cleanupImageInfos *[]cleanup.RemovedImageInfo,
	progress *state.Progress,
) {
	if progress != nil {
		progress.RecordRestart(log, c, newContainerID)
	}

	log.Debug().
		Str("container", c.Name()).
		Str("image", c.ImageName()).
		Msg("Restarted container")

	if c.IsStale() && !renamed {
		cleanup.AddImage(cleanupImageInfos, c.ImageID(), c.ImageName(), c.Name(), c.ID())
	}
}

// stopContainersInReversedOrder stops containers in reverse order.
//
// It stops each container, tracking the stopped containers and errors, to prepare for restarts while
// respecting dependency order. A container followed by the rebound containers
// that join its network namespace is replaced first, so a failed replacement
// leaves them in place or recreates them against the restored container. A
// container whose host config names a container earlier in the list is
// stopped after its rebound containers instead. A group that a container
// which could not be stopped still joins is left as it is. Its first
// container fails and the rest are skipped.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - containers: List of containers to stop.
//   - client: Container client for Docker operations.
//   - config: Update options specifying stop timeout and other behaviors.
//
// Returns:
//   - map[container.ContainerID]error: Map of container IDs to errors for failed stops.
//   - map[container.ContainerID]container.ContainerID: Each stopped and removed
//     container with the replacement already created for it, empty when
//     none was created.
func stopContainersInReversedOrder(
	log *zerolog.Logger,
	ctx context.Context,
	containers []*container.Container,
	client Client,
	config updateParams.Params,
) (map[container.ContainerID]error, map[container.ContainerID]container.ContainerID) {
	failed := make(map[container.ContainerID]error, len(containers))
	stopped := make(map[container.ContainerID]container.ContainerID, len(containers))

	groups := networkGroups(containers)

	// Stop containers in reverse order to avoid breaking dependencies. The
	// groups are contiguous, so the containers before a group end where it
	// starts.
	start := len(containers)

	for _, group := range slices.Backward(groups) {
		start -= len(group)

		// Check for context cancellation to avoid additional work when context is canceled.
		// The current group and the ones before it are skipped.
		if ctx.Err() != nil {
			for _, untouched := range slices.Backward(containers[:start+len(group)]) {
				log.Info().
					Str("container", untouched.Name()).
					Str("image", untouched.ImageName()).
					Str("container_id", untouched.ID().ShortID()).
					Msg("Skipped container stop due to context cancellation")
				failed[untouched.ID()] = skipped(fmt.Errorf("stop skipped: %w", ctx.Err()))
			}

			return failed, stopped
		}

		// A container after the group that could not be stopped still joins
		// a member, so removing that member would cut its network. The group
		// stays as it is, and its head then holds the namespace it joins in
		// turn.
		later := containers[start+len(group):]

		held := slices.IndexFunc(later, func(c *container.Container) bool {
			return holdsMember(group, c, failed[c.ID()])
		})
		if held >= 0 {
			heldBy(group, later[held], failed[later[held].ID()], failed, stopped)

			continue
		}

		stopGroup(log, ctx, group, containers[:start], client, config, failed, stopped)
	}

	return failed, stopped
}

// recordRestartOutcomes records each container whose stop or restart did not succeed.
//
// A container the update deliberately left untouched is recorded as skipped
// and any other as failed. A container that failed in an earlier phase stays
// failed when a later phase skips it. Each failure is logged once at error
// level with the container, its image and the error. That line is the
// failure's only report to notifications. It is "Could not update container"
// for a container the update recreates. A rebound container, which may be one
// the session does not monitor, gets "Could not reconnect container to the
// network of a recreated container".
//
// Parameters:
//   - log: Process logger.
//   - progress: Session progress to update.
//   - containers: The containers being restarted, which name the failures.
//   - outcomes: The error of each container that was not stopped or restarted.
//   - config: Update options, read for the status of a rebound container the
//     session does not hold.
func recordRestartOutcomes(
	log *zerolog.Logger,
	progress *state.Progress,
	containers []*container.Container,
	outcomes map[container.ContainerID]error,
	config updateParams.Params,
) {
	byID := make(map[container.ContainerID]*container.Container, len(containers))
	for _, c := range containers {
		byID[c.ID()] = c
	}

	failed := make(map[container.ContainerID]error, len(outcomes))
	skips := make(map[container.ContainerID]error)
	rebound := make(map[container.ContainerID]*container.Container)

	for containerID, err := range outcomes {
		if isSkip(err) {
			skips[containerID] = err

			continue
		}

		failed[containerID] = err

		c, ok := byID[containerID]
		if !ok || !c.IsRebinding() {
			continue
		}

		rebound[containerID] = c

		// A failed rebound container that the session does not hold, such
		// as an unmonitored one, is added so that the report, the metrics
		// and every notification count it.
		if _, held := (*progress)[containerID]; !held {
			progress.AddFailed(log, c, err, config)
		}
	}

	progress.UpdateSkipped(log, skips)
	progress.UpdateFailed(log, failed)

	// The restart steps log their own failures at debug. This line reports
	// each failure, in container ID order so notifications list them the same
	// way every time.
	for _, containerID := range slices.Sorted(maps.Keys(failed)) {
		if c, ok := rebound[containerID]; ok {
			log.Error().
				Err(failed[containerID]).
				Str("container", c.Name()).
				Str("image", c.ImageName()).
				Str("network_container", c.NetworkContainer()).
				Msg(logging.ContainerRebindFailed)

			continue
		}

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

	err := prepareStop(log, ctx, c, client, config)
	if err != nil {
		return false, err
	}

	err = stopAndRemove(log, ctx, c, client, config)
	if err != nil {
		return false, err
	}

	return true, nil
}

// prepareStop readies a container to be stopped and recreated.
//
// It checks that the container can be recreated and runs its pre-update hook.
// It then stores the container's copy files.
//
// Parameters:
//   - log: Session logger.
//   - ctx: Context for cancellation and timeouts.
//   - c: Container to stop.
//   - client: Container client for Docker operations.
//   - config: Update options specifying lifecycle hooks.
//
// Returns:
//   - error: A skip when the pre-update hook skips the container, another
//     error when a step fails, and nil otherwise.
func prepareStop(
	log *zerolog.Logger,
	ctx context.Context,
	c *container.Container,
	client Client,
	config updateParams.Params,
) error {
	fields := map[string]any{
		"container": c.Name(),
		"image":     c.ImageName(),
	}

	// Verify configuration for linked and rebound containers to ensure restart compatibility.
	if c.RestartsForAnother() {
		err := c.VerifyConfiguration()
		if err != nil {
			log.Debug().
				Err(err).
				Fields(fields).
				Msg("Failed to verify container configuration")

			return fmt.Errorf("%w: %w", errVerifyConfigFailed, err)
		}
	}

	// Execute pre-update lifecycle hooks if enabled, checking for skip conditions.
	if runsUpdateHooks(c, config) {
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

			return err //nolint:wrapcheck // The hook's error already names the failed pre-update command.
		}

		if skipUpdate {
			log.Debug().
				Fields(fields).
				Msg("Skipping container due to pre-update exit code 75")

			return skipped(errSkipUpdate)
		}
	}

	err := client.SnapshotCopyFiles(ctx, c)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to snapshot copy-file paths")

		return fmt.Errorf("snapshot copy-file paths: %w", err)
	}

	return nil
}

// stopAndRemove stops and removes a container whose copy files are stored.
//
// A container that no longer exists is skipped, so it is not recreated from
// the scan's inspect data.
//
// Parameters:
//   - log: Session logger.
//   - ctx: Context for cancellation and timeouts.
//   - c: Container to stop.
//   - client: Container client for Docker operations.
//   - config: Update options specifying the stop timeout.
//
// Returns:
//   - error: A skip when the container no longer exists, the client's error
//     when the stop or removal fails, and nil otherwise.
func stopAndRemove(
	log *zerolog.Logger,
	ctx context.Context,
	c *container.Container,
	client Client,
	config updateParams.Params,
) error {
	err := client.StopAndRemoveContainer(ctx, c, config.Timeout)
	if err == nil {
		return nil
	}

	return abandonStop(log, client, c, err, "Failed to stop container")
}

// abandonStop handles a step that failed before a container's removal.
//
// Nothing recreates the container, so its stored copy files are dropped. A
// container the daemon no longer has was removed after the scan and is
// skipped, since recreating it from the scan's inspect data would bring back
// a removed container.
//
// Parameters:
//   - log: Session logger.
//   - client: Container client for Docker operations.
//   - c: The container whose step failed.
//   - err: The client's error.
//   - failure: The debug message for any failure other than a missing
//     container.
//
// Returns:
//   - error: A skip when c no longer exists, err otherwise.
func abandonStop(log *zerolog.Logger, client Client, c *container.Container, err error, failure string) error {
	client.DiscardCopyFiles(c.ID())

	event := log.Debug().
		Err(err).
		Str("container", c.Name()).
		Str("image", c.ImageName())

	if cerrdefs.IsNotFound(err) {
		event.Msg("Container no longer exists, skipping it")

		return skipped(fmt.Errorf("%w: %w", errContainerGone, err))
	}

	event.Msg(failure)

	return err
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
//   - stopped: Each stopped container with the replacement already created
//     for it, empty when none was created.
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
	stopped map[container.ContainerID]container.ContainerID,
	cleanupImageInfos *[]cleanup.RemovedImageInfo,
	progress *state.Progress,
) map[container.ContainerID]error {
	failed := make(map[container.ContainerID]error, len(containers))

	// Restart containers in sorted order to respect dependency chains.
	for i := range containers {
		c := containers[i]

		if !c.ToRestart() {
			continue
		}

		created, wasStopped := stopped[c.ID()]
		recreate := recreates(c, wasStopped)

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
		if ctx.Err() != nil {
			clog := log.With().
				Str("container", c.Name()).
				Str("image", c.ImageName()).
				Str("container_id", c.ID().ShortID()).
				Logger()

			if !recreate {
				clog.Info().Msg("Skipped container restart due to context cancellation")

				failed[c.ID()] = skipped(fmt.Errorf("restart skipped: %w", ctx.Err()))

				continue
			}

			clog.Info().Msg("Parent context canceled. Continuing restart of stopped container")
		}

		if !recreate {
			continue
		}

		newContainerID, renamed, err := restartStaleContainer(log, ctx, c, created, client, config)
		if err != nil {
			failed[c.ID()] = err

			continue
		}

		recordReplacement(log, c, newContainerID, renamed, cleanupImageInfos, progress)
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
//   - created: The replacement when it was created before sourceContainer
//     was stopped, empty to create it here.
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
	created container.ContainerID,
	client Client,
	config updateParams.Params,
) (container.ContainerID, bool, error) {
	// Create a detached context to survive parent context cancellation.
	// This ensures container cleanup and update operations complete even if the
	// parent context is canceled during the restart process.
	detachedCtx, cancelDetached := context.WithTimeout(context.WithoutCancel(ctx), config.CreateStartTimeout())

	defer cancelDetached()

	fields := map[string]any{
		"container": sourceContainer.Name(),
		"image":     sourceContainer.ImageName(),
	}

	var rename selfupdate.Rename

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

		rename, err = selfupdate.BeginRename(log, ctx, client, sourceContainer)
		if err != nil {
			return "", false, err //nolint:wrapcheck // The client's error already names the failed rename.
		}
	}

	// Create the new container with updated configuration, unless it was
	// created before the source container was stopped.
	newContainerID := created
	if newContainerID == "" {
		var err error

		newContainerID, err = client.CreateContainer(detachedCtx, sourceContainer)
		if err != nil {
			log.Debug().
				Err(err).
				Fields(fields).
				Msg("Failed to create container")

			err = rename.Abort(ctx, client, "", config, err)

			return "", rename.Renamed(), err
		}
	}

	if replacementStarts(sourceContainer, config) {
		err := startReplacement(log, detachedCtx, client, sourceContainer, newContainerID, config, fields)
		if err != nil {
			// A Watchtower container renamed for this self-update still runs
			// this process, and it takes its original name back.
			err = rename.Abort(ctx, client, newContainerID, config, err)

			return "", rename.Renamed(), err
		}
	}

	rename.Retire(detachedCtx, client)

	return newContainerID, rename.Renamed(), nil
}

// recreates reports whether the restart phase recreates a container.
//
// A container stopped and removed by the stop phase is recreated. Watchtower's
// own container is not stopped there because its self-update replaces it.
//
// Parameters:
//   - c: A container marked for restart.
//   - wasStopped: Whether the stop phase stopped and removed c.
//
// Returns:
//   - bool: True when the restart phase recreates c.
func recreates(c *container.Container, wasStopped bool) bool {
	return wasStopped || c.IsWatchtower()
}

// runsUpdateHooks reports whether a container's pre- and post-update hooks
// run.
//
// A rebound container is not updated, so its hooks do not run.
//
// Parameters:
//   - c: The container being restarted.
//   - config: Update options controlling lifecycle hooks.
//
// Returns:
//   - bool: True when lifecycle hooks are enabled and c is not rebound.
func runsUpdateHooks(c *container.Container, config updateParams.Params) bool {
	return config.LifecycleHooks && !c.IsRebinding()
}

// replacementStarts reports whether a container's replacement is started.
//
// A replacement starts when its container was running or with the
// revive-stopped configuration option. A rebound container keeps its state,
// so revive-stopped does not start it. The no-restart configuration option
// leaves every replacement stopped except Watchtower's own.
//
// Parameters:
//   - c: The container the replacement is created from.
//   - config: Update options controlling restart behavior.
//
// Returns:
//   - bool: True when the replacement is started.
func replacementStarts(c *container.Container, config updateParams.Params) bool {
	revive := config.ReviveStopped && !c.IsRebinding()

	return (!config.NoRestart || c.IsWatchtower()) && (c.IsRunning() || revive)
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
	if sourceContainer.ToRestart() && runsUpdateHooks(sourceContainer, config) {
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
