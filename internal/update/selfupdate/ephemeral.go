package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/ephemeral"
	containerImage "github.com/nicholas-fedor/watchtower/internal/container/image"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
)

// OrchestratorCreator starts the container that carries out a self-update.
type OrchestratorCreator interface {
	// CreateEphemeralOrchestrator creates and starts a short-lived container
	// that replaces the source container with one running the new image.
	CreateEphemeralOrchestrator(
		ctx context.Context,
		sourceContainer *container.Container,
		newImage string,
		containerChain string,
		cleanup bool,
	) (container.ContainerID, error)
}

// HandoffClient replaces the old Watchtower container with its successor.
type HandoffClient interface {
	Renamer
	ReplacementRemover

	// StopContainer stops the container. The stop waits up to timeout.
	StopContainer(ctx context.Context, target *container.Container, timeout time.Duration) error
	// StartContainer creates and starts a container from the given container's
	// configuration and returns the new container's ID.
	StartContainer(ctx context.Context, target *container.Container) (container.ContainerID, error)
	// StartContainerByID starts an existing container.
	StartContainerByID(ctx context.Context, containerID container.ContainerID) error
	// RemoveContainer removes the container.
	RemoveContainer(ctx context.Context, target *container.Container) error
	// RemoveImageByID removes the image. The image name is used in logs.
	RemoveImageByID(ctx context.Context, imageID container.ImageID, imageName string) error
}

// orchestratorEnv holds what the old Watchtower passes to its orchestrator
// through the environment.
type orchestratorEnv struct {
	// oldID is the old container's full ID.
	oldID string
	// newImage is the image reference the replacement is created from.
	newImage string
	// originalName is the name the replacement takes over.
	originalName string
	// containerChain is the container chain label for lineage tracking.
	containerChain string
	// networkContainer is the name or ID of the container whose network
	// namespace the old container joins, empty when it has a network of its
	// own.
	networkContainer string
	// cleanup reports whether the old image is removed after the handoff.
	cleanup bool
}

// orchestratorTimeout defines the maximum duration for the orchestrator to complete.
// This covers: inspect old, stop old, create new, start new, remove old.
const orchestratorTimeout = 5 * time.Minute

// orchestratorStopTimeout defines the timeout for stopping the old container.
const orchestratorStopTimeout = 30 * time.Second

// orchestratorCreateTimeout is the timeout for the detached context used when
// creating the ephemeral orchestrator container.
const orchestratorCreateTimeout = 60 * time.Second

// predecessorImageRemovalTimeout bounds the removal of the old Watchtower image
// after a handoff, which runs detached from the session context.
const predecessorImageRemovalTimeout = 30 * time.Second

// orchestratorRecoveryTimeout bounds each recovery helper that renames or
// starts the predecessor after a failed handoff.
//
// Recovery runs detached from the orchestration context, so it still runs
// after that context's deadline. The 60 seconds match the shortest bound for
// removing a failed replacement.
const orchestratorRecoveryTimeout = 60 * time.Second

// Errors for ephemeral self-update operations in ephemeral.go.
var (
	// errOrchestratorMissingEnv indicates a required environment variable is missing.
	errOrchestratorMissingEnv = errors.New("missing orchestrator environment variable")
	// errOrchestratorInvalidEnv indicates an environment variable holds a value Watchtower never writes.
	errOrchestratorInvalidEnv = errors.New("invalid orchestrator environment variable")
	// errOrchestratorOldContainerNotFound indicates the old container was not found.
	errOrchestratorOldContainerNotFound = errors.New("old container not found")
	// errNewContainerNotRunning indicates the new container is not running after start.
	errNewContainerNotRunning = errors.New("new container is not running after start")
)

// Patterns for the container references the orchestrator reads.
var (
	// fullContainerIDPattern matches a full container ID, the form the old
	// instance writes for its own ID and the form Docker stores for a network
	// container.
	fullContainerIDPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	// containerNamePattern matches a container name without its leading
	// slash. It follows Podman's rule, which also accepts the single-character
	// names Docker rejects. A full container ID matches too.
	containerNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
)

// StartOrchestrator performs a self-update using an ephemeral orchestrator container.
//
// Instead of the rename-based approach, this creates a short-lived container that:
//  1. Inspects the old container's configuration
//  2. Stops the old container
//  3. Creates a new container from the new image with the same config
//  4. Starts the new container
//  5. Removes the old container
//  6. When cleanup is enabled, removes the unused predecessor image
//  7. Exits (AutoRemove cleans up the orchestrator)
//
// This function returns immediately after starting the orchestrator. The
// orchestrator handles the full replacement sequence asynchronously. The
// current Watchtower process will be stopped by the orchestrator shortly
// after this function returns.
//
// The ephemeral container uses the same Watchtower image (already pulled) and
// mounts the Docker socket for container management.
//
// Parameters:
//   - log: Process logger. Required and must be non-nil. A nil logger panics on the first log call.
//   - ctx: Parent context. Its values carry over to the orchestrator creation, but its
//     cancellation does not.
//   - client: Container client for Docker operations.
//   - sourceContainer: Current Watchtower container being replaced.
//   - config: Update parameters.
//
// Returns:
//   - container.ContainerID: Empty string (the new container's ID is not known to the caller).
//   - bool: True so the dying process skips image cleanup. The orchestrator
//     removes the old container and, when cleanup is enabled, the old image.
//   - error: Non-nil if orchestrator creation fails.
func StartOrchestrator(log *zerolog.Logger, ctx context.Context,
	client OrchestratorCreator,
	sourceContainer *container.Container,
	config updateParams.Params,
) (container.ContainerID, bool, error) {
	clogVal := log.With().
		Str("container", sourceContainer.Name()).
		Str("image", sourceContainer.ImageName()).
		Logger()
	clog := &clogVal

	clog.Debug().Msg("Initiating ephemeral self-update for Watchtower")

	// Detach the orchestrator creation from the parent's cancellation so it survives a
	// canceled update. The timeout prevents indefinite hangs during orchestrator creation.
	detachedCtx, cancelDetached := context.WithTimeout(
		context.WithoutCancel(ctx),
		orchestratorCreateTimeout,
	)
	defer cancelDetached()

	// The image name from the source container reflects the latest pulled image.
	// This is the same image the ephemeral container will use.
	newImage := sourceContainer.ImageName()

	// Compute the container chain for lineage tracking. The orchestrator will
	// set this on the new container's labels via the WT_ORCHESTRATOR_CONTAINER_CHAIN
	// environment variable. This preserves the cleanup behavior used in the rename path.
	newChain := sourceContainer.SuccessorChain()

	clog.Debug().
		Str("container_chain", newChain).
		Msg("Computed container chain for ephemeral self-update")

	orchestratorID, err := client.CreateEphemeralOrchestrator(
		detachedCtx,
		sourceContainer,
		newImage,
		newChain,
		config.Cleanup,
	)
	if err != nil {
		return "", false, err //nolint:wrapcheck // The orchestrator's error already names the step that failed.
	}

	// The orchestrator stops and replaces this container after this process
	// returns. The line comes only once the orchestrator has started, so a
	// failed handoff does not report it.
	clog.Info().
		Str("orchestrator_id", orchestratorID.ShortID()).
		Msg(logging.EphemeralSelfUpdateStarting)

	// Return immediately. The orchestrator handles the full replacement sequence
	// asynchronously: stopping the old container, creating and starting the new one,
	// and cleaning up. The current Watchtower process will be stopped by the
	// orchestrator shortly after this function returns.
	//
	// Return an empty container ID because the new container's ID is not known
	// to this process — it is created by the orchestrator after this process is
	// stopped. Callers must handle the ephemeral case specially (skip health checks
	// and post-update hooks since the old process exits before the new container exists).
	//
	// Return true for "renamed" so the dying process does not collect the old
	// image for cleanup. This process is about to be stopped and still uses that
	// image. The orchestrator removes the old container and, when cleanup is
	// enabled, the old image after the replacement is running.
	return "", true, nil
}

// RunOrchestrator executes the orchestrator mode for self-update.
//
// This is the entry point when Watchtower is started with --self-update-orchestrator.
// It reads environment variables to determine the old container ID, new image, and
// original container name, then performs the container replacement sequence.
//
// The orchestrator follows a deterministic state machine:
//  1. VALIDATE: Read and validate environment variables
//  2. INSPECT: Get the old container's full configuration
//  3. STOP OLD: Stop the old container (frees host ports)
//  4. RENAME OLD: Move the stopped predecessor off the original name
//  5. CREATE NEW: Create a new container from the new image with the same config
//  6. START NEW: Start the new Watchtower container
//  7. VERIFY: Confirm the new container is running
//  8. REMOVE OLD: Delete the renamed predecessor
//  9. REMOVE OLD IMAGE: When cleanup is enabled, delete the unused predecessor image
//
// Stop before create releases published host ports. Rename keeps the stopped
// predecessor for recovery if create or start fails (restore name and start).
//
// RunOrchestrator logs each failure before returning it, at error level, or at
// debug level when a stop signal that canceled ctx caused it.
//
// Parameters:
//   - log: Process logger. Required and must be non-nil. A nil logger panics on the first log call.
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker operations.
//
// Returns:
//   - error: Non-nil when the environment variables are invalid or the handoff fails.
func RunOrchestrator(log *zerolog.Logger, ctx context.Context, client HandoffClient) error {
	clogVal := log.With().
		Str("mode", "orchestrator").
		Logger()
	clog := &clogVal

	clog.Debug().Msg("Starting Watchtower self-update orchestrator")

	// Read environment variables.
	env, err := readOrchestratorEnv()
	if err != nil {
		clog.Error().
			Err(err).
			Msg("Failed to read orchestrator environment variables")

		return err
	}

	clog.Debug().
		Str("old_id", env.oldID).
		Str("new_image", env.newImage).
		Str("original_name", env.originalName).
		Str("container_chain", env.containerChain).
		Str("network_container", env.networkContainer).
		Bool("cleanup", env.cleanup).
		Msg("Read orchestrator environment variables")

	// Create a timeout context for the entire orchestration.
	orchCtx, orchCancel := context.WithTimeout(
		ctx,
		orchestratorTimeout,
	)
	defer orchCancel()

	// Execute the orchestration sequence.
	err = orchestrateSelfUpdate(clog, orchCtx, client, env)
	if err != nil {
		// The steps log their failures at debug. This line reports the failure,
		// unless a stop signal ended the handoff on purpose. The handoff's own
		// deadline ends it with a different error, which is still reported.
		level, msg := zerolog.ErrorLevel, "Orchestration failed"
		if ctx.Err() != nil && errors.Is(err, context.Canceled) {
			level, msg = zerolog.DebugLevel, "Orchestration stopped by a signal"
		}

		clog.WithLevel(level).
			Err(err).
			Str("old_id", env.oldID).
			Str("new_image", env.newImage).
			Str("original_name", env.originalName).
			Msg(msg)

		return err
	}

	clog.Debug().Msg("Self-update orchestration completed successfully")

	return nil
}

// readOrchestratorEnv reads and validates the environment variables required
// by the orchestrator.
//
// The container chain and the network container are optional. An empty
// cleanup value means false.
//
// Returns:
//   - orchestratorEnv: The values the old instance passed.
//   - error: Non-nil if a required variable is missing, the old ID is not a
//     full container ID, the network container is not a container name or ID,
//     or the cleanup value is not a boolean.
func readOrchestratorEnv() (orchestratorEnv, error) {
	oldID := os.Getenv(ephemeral.OrchestratorOldIDEnv)
	if oldID == "" {
		return orchestratorEnv{}, fmt.Errorf("%w: %s", errOrchestratorMissingEnv, ephemeral.OrchestratorOldIDEnv)
	}

	if !fullContainerIDPattern.MatchString(oldID) {
		return orchestratorEnv{}, fmt.Errorf(
			"%w: %s %q is not a full container ID",
			errOrchestratorInvalidEnv,
			ephemeral.OrchestratorOldIDEnv,
			oldID,
		)
	}

	newImage := os.Getenv(ephemeral.OrchestratorNewImageEnv)
	if newImage == "" {
		return orchestratorEnv{}, fmt.Errorf("%w: %s", errOrchestratorMissingEnv, ephemeral.OrchestratorNewImageEnv)
	}

	originalName := os.Getenv(ephemeral.OrchestratorOriginalNameEnv)
	if originalName == "" {
		return orchestratorEnv{}, fmt.Errorf("%w: %s", errOrchestratorMissingEnv, ephemeral.OrchestratorOriginalNameEnv)
	}

	networkContainer := os.Getenv(ephemeral.OrchestratorNetworkContainerEnv)
	if networkContainer != "" && !containerNamePattern.MatchString(networkContainer) {
		return orchestratorEnv{}, fmt.Errorf(
			"%w: %s %q is not a container name or ID",
			errOrchestratorInvalidEnv,
			ephemeral.OrchestratorNetworkContainerEnv,
			networkContainer,
		)
	}

	cleanup := false

	if rawCleanup := os.Getenv(ephemeral.OrchestratorCleanupEnv); rawCleanup != "" {
		parsed, err := strconv.ParseBool(rawCleanup)
		if err != nil {
			return orchestratorEnv{}, fmt.Errorf("%w: %s: %w", errOrchestratorInvalidEnv, ephemeral.OrchestratorCleanupEnv, err)
		}

		cleanup = parsed
	}

	return orchestratorEnv{
		oldID:            oldID,
		newImage:         newImage,
		originalName:     originalName,
		containerChain:   os.Getenv(ephemeral.OrchestratorContainerChainEnv),
		networkContainer: networkContainer,
		cleanup:          cleanup,
	}, nil
}

// orchestrateSelfUpdate performs the container replacement sequence for a
// Watchtower self-update.
//
// Sequence:
//  1. Inspect the old container and propagate the chain label.
//  2. Pin Config.Image to newImage for create and point the replacement at
//     the network container by name when its ID names a removed container.
//  3. Stop the old container (frees host ports).
//  4. Rename the stopped old container off the original name.
//  5. Create and start the new container under the original name.
//  6. Verify the new container is running.
//  7. Remove the renamed predecessor.
//  8. When cleanup is enabled, remove the unused predecessor image.
//
// Stop before create releases published host ports. Rename keeps the stopped
// predecessor available for recovery. When the rename fails, the old container
// is started again. On create/start/verify failure after rename, the original
// name is restored and the old container is started again.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker operations.
//   - env: The values the old instance passed.
//
// Returns:
//   - error: Non-nil if any step in the orchestration fails.
func orchestrateSelfUpdate(log *zerolog.Logger, ctx context.Context,
	client HandoffClient,
	env orchestratorEnv,
) error {
	clogVal := log.With().
		Str("old_id", env.oldID).
		Str("new_image", env.newImage).
		Str("original_name", env.originalName).
		Logger()
	clog := &clogVal

	oldContainer, err := inspectOldContainer(
		ctx,
		client,
		clog,
		env.oldID,
		env.containerChain,
	)
	if err != nil {
		return err
	}

	// Capture before pinContainerCreateImage overwrites the cached image name.
	oldImageID := oldContainer.ImageID()
	oldImageName := oldContainer.ImageName()

	pinContainerCreateImage(oldContainer, env.newImage, clog)

	// Inspecting the old container replaces the network container's ID with
	// its name when that container still exists, and the replacement joins
	// it. A full ID left in place names a removed container, so the
	// replacement joins the container the old instance named.
	if env.networkContainer != "" && fullContainerIDPattern.MatchString(oldContainer.NetworkContainer()) {
		oldContainer.SetNetworkContainer(env.networkContainer)
	}

	// Free host ports before create. Keep the container so rename can preserve
	// it for recovery if the replacement fails.
	oldGone, err := stopOldContainer(ctx, client, clog, oldContainer)
	if err != nil {
		return err
	}

	if !oldGone {
		// A predecessor that already has its old name from an earlier attempt
		// is not renamed again. Recovery depends only on the predecessor still
		// existing, so it restores and restarts that one too.
		_, err = renameToOldName(clog, ctx, client, oldContainer)
		if err != nil {
			// The old container keeps its name, so starting it again leaves
			// a Watchtower instance running.
			restartOldContainer(ctx, client, clog, oldContainer)

			return err
		}
	}

	newContainerID, err := createAndStartNewContainer(
		ctx,
		client,
		clog,
		oldContainer,
	)
	if err != nil {
		// Recover any existing predecessor, including one already renamed to
		// watchtower-old-* from an earlier attempt (renamed may be false).
		if !oldGone {
			restoreAndStartOldContainer(ctx, client, clog, oldContainer, env.originalName)
		}

		return err
	}

	err = ensureContainerRunning(ctx, client, clog, newContainerID)
	if err != nil {
		// A stop timeout set on the container takes precedence over the 30
		// seconds passed to the stop, so the removal's bound follows the longer.
		RemoveFailedReplacement(clog, ctx, client, oldContainer, newContainerID,
			updateParams.Params{Timeout: orchestratorStopTimeout})

		if !oldGone {
			restoreAndStartOldContainer(ctx, client, clog, oldContainer, env.originalName)
		}

		return err
	}

	if !oldGone {
		removeErr := removeOldContainer(ctx, client, clog, oldContainer)
		if removeErr != nil {
			clog.Warn().
				Err(removeErr).
				Msg("New Watchtower is running but cleanup of the old container failed")
		}
	}

	if env.cleanup {
		removeOldImage(ctx, client, clog, oldImageID, oldImageName)
	}

	// Emit the actual new container ID at Info level so notification templates
	// can consume the correct "new_id" field.
	clog.Info().
		Str("new_id", newContainerID.ShortID()).
		Msg(logging.StartedNewContainer)

	return nil
}

// pinContainerCreateImage sets Config.Image on the old container so
// GetCreateConfig uses newImage for the replacement instance.
//
// SetImageName also updates the cached ImageName used after the pin.
//
// Parameters:
//   - oldContainer: Source container whose create config is pinned.
//   - newImage: Image reference to use when creating the replacement.
//   - clog: Logger with container context fields.
func pinContainerCreateImage(oldContainer *container.Container, newImage string, clog *zerolog.Logger) {
	if newImage == "" {
		return
	}

	info := oldContainer.ContainerInfo()
	if info == nil || info.Config == nil {
		return
	}

	oldContainer.SetImageName(newImage)
	clog.Debug().
		Str("pinned_image", newImage).
		Msg("Pinned create image for ephemeral self-update")
}

// restoreAndStartOldContainer renames the predecessor back to originalName and
// starts it after a failed create/start so a Watchtower instance remains running.
//
// Parameters:
//   - ctx: Orchestration context. Its values carry over to recovery, but its
//     cancellation and deadline do not.
//   - client: Container client for Docker operations.
//   - clog: Logger with container context fields.
//   - oldContainer: Stopped predecessor to restore.
//   - originalName: Canonical name to restore on the predecessor.
func restoreAndStartOldContainer(
	ctx context.Context,
	client HandoffClient,
	clog *zerolog.Logger,
	oldContainer *container.Container,
	originalName string,
) {
	if originalName == "" {
		return
	}

	clog.Warn().
		Str("original_name", originalName).
		Msg("Restoring old Watchtower container after handoff failure")

	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), orchestratorRecoveryTimeout)
	defer cancel()

	err := client.RenameContainer(recoveryCtx, oldContainer, originalName)
	if err != nil {
		clog.Error().
			Err(err).
			Str("original_name", originalName).
			Msg("Failed to restore old Watchtower container name. Manual intervention required")

		return
	}

	err = client.StartContainerByID(recoveryCtx, oldContainer.ID())
	if err != nil {
		clog.Error().
			Err(err).
			Str("original_name", originalName).
			Msg("Failed to restart old Watchtower container after name restore. Manual intervention required")

		return
	}

	clog.Info().
		Str("original_name", originalName).
		Msg("Restored and restarted old Watchtower container after handoff failure")
}

// restartOldContainer starts the predecessor again after a failed rename, so
// a Watchtower instance remains running under its original name.
//
// Parameters:
//   - ctx: Orchestration context. Its values carry over to recovery, but its
//     cancellation and deadline do not.
//   - client: Container client for Docker operations.
//   - clog: Logger with container context fields.
//   - oldContainer: Stopped predecessor to start.
func restartOldContainer(
	ctx context.Context,
	client HandoffClient,
	clog *zerolog.Logger,
	oldContainer *container.Container,
) {
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), orchestratorRecoveryTimeout)
	defer cancel()

	err := client.StartContainerByID(recoveryCtx, oldContainer.ID())
	if err != nil {
		clog.Error().
			Err(err).
			Msg("Failed to restart old Watchtower container after rename failure. Manual intervention required")

		return
	}

	clog.Info().Msg("Restarted old Watchtower container after rename failure")
}

// inspectOldContainer retrieves the old container's configuration and propagates
// the container chain label to it. The chain label is set on the old container's
// config so that StartContainer's GetCreateConfig() includes it on the new container.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker operations.
//   - clog: Logger with container context fields.
//   - oldID: ID of the old Watchtower container to inspect.
//   - containerChain: Container chain label for lineage tracking.
//
// Returns:
//   - *container.Container: The inspected old container.
//   - error: Non-nil if inspection fails.
func inspectOldContainer(
	ctx context.Context,
	client HandoffClient,
	clog *zerolog.Logger,
	oldID string,
	containerChain string,
) (*container.Container, error) {
	clog.Debug().Msg("Inspecting old container")

	oldContainer, err := client.GetContainer(
		ctx,
		container.ContainerID(oldID),
	)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			clog.Debug().Msg("Old container not found")

			return nil, fmt.Errorf("%w: %s", errOrchestratorOldContainerNotFound, oldID)
		}

		return nil, err //nolint:wrapcheck // The client's error already names the failed inspect.
	}

	if !oldContainer.IsRunning() {
		clog.Warn().Msg("Old container is not running - proceeding with creation only")
	}

	clog.Debug().
		Str("old_name", oldContainer.Name()).
		Msg("Inspected old container successfully")

	// Propagate the container chain label to the old container's config, so
	// StartContainer's GetCreateConfig() includes the label on the new container.
	if containerChain != "" {
		oldContainer.SetLabel(labels.ContainerChain, containerChain)
		clog.Debug().
			Str("container_chain", containerChain).
			Msg("Set container chain label on source container config")
	}

	return oldContainer, nil
}

// stopOldContainer stops the old container so published host ports are released
// before the replacement is created. The container is left in place for rename
// and optional recovery.
//
// A stop that has begun runs to its end with a context of its own, bounded by
// the longer of 30 seconds and the container's stop timeout plus 30 seconds.
// A stop signal that cancels ctx during it therefore leaves the container
// stopped, and the rename that follows fails on ctx and starts the container
// again. When ctx has ended before the stop begins, the container is left
// running. A failed stop is checked against the container's state with a
// context of its own, bounded like a recovery step.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker operations.
//   - clog: Logger with container context fields.
//   - oldContainer: The old Watchtower container to stop.
//
// Returns:
//   - oldGone: True when the container is already absent (NotFound on stop).
//   - error: Non-nil if stopping fails fatally while the container still runs.
func stopOldContainer(
	ctx context.Context,
	client HandoffClient,
	clog *zerolog.Logger,
	oldContainer *container.Container,
) (bool, error) {
	if ctx.Err() != nil {
		return false, fmt.Errorf("stop old container: %w", context.Cause(ctx))
	}

	clog.Debug().Msg("Stopping old Watchtower container")

	// A stop cut short would leave the container stopping with no step left
	// to start it again, so it is not canceled with ctx.
	stopCtx, cancelStop := context.WithTimeout(
		context.WithoutCancel(ctx),
		updateParams.Params{Timeout: orchestratorStopTimeout}.DetachedStopTimeout(oldContainer),
	)
	defer cancelStop()

	err := client.StopContainer(
		stopCtx,
		oldContainer,
		orchestratorStopTimeout,
	)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			clog.Debug().Msg("Old container already removed")

			return true, nil
		}

		// Re-inspect after a non-NotFound stop error. The cached view may be
		// stale.
		inspectCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), orchestratorRecoveryTimeout)
		defer cancel()

		freshContainer, inspectErr := client.GetContainer(
			inspectCtx,
			oldContainer.ID(),
		)
		if inspectErr != nil {
			if cerrdefs.IsNotFound(inspectErr) {
				clog.Debug().Msg("Old container already removed")

				return true, nil
			}

			clog.Debug().
				Err(inspectErr).
				Msg("Failed to re-inspect old container after stop failure")

			return false, err //nolint:wrapcheck // The client's error already names the failed stop.
		}

		if !freshContainer.IsRunning() {
			clog.Debug().Msg("Old container is not running after stop attempt")

			return false, nil
		}

		return false, err //nolint:wrapcheck // The client's error already names the failed stop.
	}

	clog.Debug().Msg("Old container stopped")

	return false, nil
}

// removeOldContainer removes the renamed predecessor after a successful handoff.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker operations.
//   - clog: Logger with container context fields.
//   - oldContainer: The old Watchtower container to remove.
//
// Returns:
//   - error: Non-nil if removal fails for a reason other than NotFound.
func removeOldContainer(
	ctx context.Context,
	client HandoffClient,
	clog *zerolog.Logger,
	oldContainer *container.Container,
) error {
	clog.Debug().Msg("Removing old Watchtower container after successful handoff")

	err := client.RemoveContainer(ctx, oldContainer)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			clog.Debug().Msg("Old container already removed")

			return nil
		}

		return err //nolint:wrapcheck // The client's error already names the failed removal.
	}

	clog.Debug().Msg("Old container removed")

	return nil
}

// removeOldImage removes the predecessor Watchtower image after a successful
// handoff. NotFound and in-use errors are skipped. Other failures are logged
// and do not fail orchestration because the replacement is already running.
// Docker calls use a detached timeout so SIGTERM cannot abort removal.
//
// Parameters:
//   - ctx: Parent session context. Cancellation is detached for Docker calls.
//   - client: Container client for Docker operations.
//   - clog: Logger with container context fields.
//   - imageID: ID of the predecessor image captured before create-image pinning.
//   - imageName: Name of the predecessor image captured before create-image pinning.
func removeOldImage(
	ctx context.Context,
	client HandoffClient,
	clog *zerolog.Logger,
	imageID container.ImageID,
	imageName string,
) {
	if imageID == "" {
		return
	}

	clog.Debug().
		Str("image_id", imageID.ShortID()).
		Str("image_name", imageName).
		Msg("Removing old Watchtower image after ephemeral handoff")

	// Detach from the session context so SIGTERM cannot abort predecessor
	// image removal after the replacement is already running.
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		predecessorImageRemovalTimeout,
	)
	defer cancel()

	err := client.RemoveImageByID(cleanupCtx, imageID, imageName)
	if err == nil {
		clog.Info().
			Str("image_id", imageID.ShortID()).
			Str("image_name", imageName).
			Msg(logging.RemovingImage)

		return
	}

	switch {
	case cerrdefs.IsNotFound(err),
		cerrdefs.IsConflict(err),
		errors.Is(err, containerImage.ErrImageInUse),
		errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded),
		cleanupCtx.Err() != nil:
		clog.Debug().
			Err(err).
			Str("image_id", imageID.ShortID()).
			Str("image_name", imageName).
			Msg("Skipped old Watchtower image removal")
	default:
		clog.Warn().
			Err(err).
			Str("image_id", imageID.ShortID()).
			Str("image_name", imageName).
			Msg("New Watchtower is running but cleanup of the old image failed")
	}
}

// createAndStartNewContainer creates and starts a new container using the old
// container's configuration. StartContainer handles config extraction, container
// creation, renaming, network attachment, and starting.
//
// StartContainer resolves the image from the source container's config
// (GetCreateConfig().Image), which orchestrateSelfUpdate pins to the
// WT_ORCHESTRATOR_NEW_IMAGE reference beforehand.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker operations.
//   - clog: Logger with container context fields.
//   - oldContainer: The old container whose config is used for the new one.
//
// Returns:
//   - container.ContainerID: ID of the newly created container.
//   - error: Non-nil if creation or starting fails.
func createAndStartNewContainer(
	ctx context.Context,
	client HandoffClient,
	clog *zerolog.Logger,
	oldContainer *container.Container,
) (container.ContainerID, error) {
	clog.Debug().Msg("Creating and starting new Watchtower container")

	newContainerID, err := client.StartContainer(
		ctx,
		oldContainer,
	)
	if err != nil {
		return "", err //nolint:wrapcheck // The client's error already names the failed create or start.
	}

	return newContainerID, nil
}

// ensureContainerRunning verifies the container is running and starts it
// explicitly if needed. StartContainer may create the container without
// starting it if the source container was stopped and the reviveStopped
// option is not enabled.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker operations.
//   - clog: Logger with container context fields.
//   - containerID: ID of the container to verify and start.
//
// Returns:
//   - error: Non-nil if the container cannot be verified or started.
func ensureContainerRunning(
	ctx context.Context,
	client HandoffClient,
	clog *zerolog.Logger,
	containerID container.ContainerID,
) error {
	clog.Debug().
		Str("new_id", containerID.ShortID()).
		Msg("Verifying new container is running")

	ctr, err := client.GetContainer(ctx, containerID)
	if err != nil {
		return err //nolint:wrapcheck // The client's error already names the failed inspect.
	}

	if ctr.IsRunning() {
		clog.Debug().Msg("New container verified as running")

		return nil
	}

	// Container was created but not started. Start it explicitly.
	clog.Debug().Msg("New container was created but not started, starting it now")

	err = client.StartContainerByID(ctx, containerID)
	if err != nil {
		return err //nolint:wrapcheck // The client's error already names the failed start.
	}

	// Re-verify the container is running after the explicit start call.
	ctr, err = client.GetContainer(ctx, containerID)
	if err != nil {
		return err //nolint:wrapcheck // The client's error already names the failed inspect.
	}

	if !ctr.IsRunning() {
		return fmt.Errorf("%w: %s", errNewContainerNotRunning, containerID.ShortID())
	}

	clog.Debug().Msg("New container verified as running")

	return nil
}
