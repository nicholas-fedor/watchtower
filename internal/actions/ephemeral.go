package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// orchestratorTimeout defines the maximum duration for the orchestrator to complete.
// This covers: inspect old, stop old, create new, start new, remove old.
const orchestratorTimeout = 5 * time.Minute

// orchestratorStopTimeout defines the timeout for stopping the old container.
const orchestratorStopTimeout = 30 * time.Second

// orchestratorCreateTimeout is the timeout for the detached context used when
// creating the ephemeral orchestrator container.
const orchestratorCreateTimeout = 60 * time.Second

// Errors for ephemeral self-update operations in ephemeral.go.
var (
	// errEphemeralOrchestratorFailed indicates the ephemeral orchestrator failed.
	errEphemeralOrchestratorFailed = errors.New("ephemeral orchestrator failed")
	// errOrchestratorMissingEnv indicates a required environment variable is missing.
	errOrchestratorMissingEnv = errors.New("missing orchestrator environment variable")
	// errOrchestratorOldContainerNotFound indicates the old container was not found.
	errOrchestratorOldContainerNotFound = errors.New("old container not found")
	// errOrchestratorStopFailed indicates a failure to stop the old container.
	errOrchestratorStopFailed = errors.New("failed to stop old container")
	// errOrchestratorRemoveFailed indicates a failure to remove the old container.
	errOrchestratorRemoveFailed = errors.New("failed to remove old container")
	// errOrchestratorCreateFailed indicates a failure to create the new container.
	errOrchestratorCreateFailed = errors.New("failed to create new container")
	// errOrchestratorStartFailed indicates a failure to start the new container.
	errOrchestratorStartFailed = errors.New("failed to start new container")
	// errOrchestratorInspectFailed indicates a failure to inspect a container during orchestration.
	errOrchestratorInspectFailed = errors.New("failed to inspect container during orchestration")
	// errNewContainerNotRunning indicates the new container is not running after start.
	errNewContainerNotRunning = errors.New("new container is not running after start")
)

// EphemeralSelfUpdate performs a self-update using an ephemeral orchestrator container.
//
// Instead of the rename-based approach, this creates a short-lived container that:
//  1. Inspects the old container's configuration
//  2. Stops the old container
//  3. Creates a new container from the new image with the same config
//  4. Starts the new container
//  5. Removes the old container
//  6. Exits (AutoRemove cleans up the orchestrator)
//
// The ephemeral container uses the same Watchtower image (already pulled) and
// mounts the Docker socket for container management.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker operations.
//   - sourceContainer: Current Watchtower container being replaced.
//   - config: Update parameters.
//
// Returns:
//   - types.ContainerID: ID of the ephemeral orchestrator container.
//   - bool: True if orchestration was initiated.
//   - error: Non-nil if orchestration fails.
func EphemeralSelfUpdate(
	ctx context.Context,
	client container.Client,
	sourceContainer types.Container,
	config types.UpdateParams,
) (types.ContainerID, bool, error) {
	fields := logrus.Fields{
		"container": sourceContainer.Name(),
		"image":     sourceContainer.ImageName(),
	}

	clog := logrus.WithFields(fields)

	clog.Debug("Initiating ephemeral self-update for Watchtower")

	// Create a detached context for the orchestrator creation to survive parent cancellation.
	// Uses a 60-second timeout to prevent indefinite hangs during orchestrator creation.
	detachedCtx, cancelDetached := context.WithTimeout(
		context.Background(),
		orchestratorCreateTimeout,
	)
	defer cancelDetached()

	// The image name from the source container reflects the latest pulled image.
	// This is the same image the ephemeral container will use.
	newImage := sourceContainer.ImageName()

	// Compute the container chain for lineage tracking. The orchestrator will
	// set this on the new container's labels via the WT_ORCHESTRATOR_CONTAINER_CHAIN
	// environment variable. This preserves the cleanup behavior used in the rename path.
	existingChain := ""
	if c, ok := sourceContainer.(*container.Container); ok {
		existingChain, _ = c.GetContainerChain()
	}

	var newChain string
	if existingChain != "" {
		newChain = existingChain + "," + string(sourceContainer.ID())
	} else {
		newChain = string(sourceContainer.ID())
	}

	clog.WithField("container_chain", newChain).Debug("Computed container chain for ephemeral self-update")

	clog.Debug("Creating ephemeral orchestrator for self-update")

	// Log "Stopping container" for notification template compatibility.
	// The orchestrator will handle the actual stop/start/remove operations,
	// but we emit these Info entries so notifications match the normal update flow.
	logrus.WithFields(logrus.Fields{
		"container":    sourceContainer.Name(),
		"id":           sourceContainer.ID().ShortID(),
		"old_image_id": sourceContainer.ImageID().ShortID(),
	}).Info("Stopping container")

	// Create the ephemeral orchestrator container.
	//nolint:contextcheck // detached context is intentional for orchestrator lifecycle
	orchestratorID, err := client.CreateEphemeralOrchestrator(
		detachedCtx,
		sourceContainer,
		newImage,
		newChain,
	)
	if err != nil {
		clog.WithError(err).Error("Failed to create ephemeral orchestrator")

		return "", false, fmt.Errorf("%w: %w", errEphemeralOrchestratorFailed, err)
	}

	clog.WithField("orchestrator_id", orchestratorID.ShortID()).
		Debug("Ephemeral orchestrator started; Watchtower will be replaced")

	// Log that the orchestrator has been started. The orchestrator ID identifies
	// the ephemeral container that will perform the replacement; the actual new
	// container's ID is determined by the orchestrator and emitted in its own
	// "Started new container" log entry.
	logrus.WithFields(logrus.Fields{
		"container":       sourceContainer.Name(),
		"orchestrator_id": orchestratorID.ShortID(),
	}).Info("Started self-update orchestrator")

	// Return the orchestrator ID. The orchestrator will handle:
	// - Stopping the old container
	// - Creating and starting the new container
	// - Removing the old container
	// - Self-cleaning (AutoRemove: true)
	//
	// The current Watchtower process will be stopped by the orchestrator.
	//
	// Return false for "renamed" because in the ephemeral flow the old container
	// IS removed (not renamed). This allows cleanup image info to be collected
	// for the old image, unlike the rename path where the old container persists.
	return orchestratorID, false, nil
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
//  3. STOP OLD: Stop the old Watchtower container
//  4. CREATE NEW: Create a new container from the new image with the same config
//  5. START NEW: Start the new Watchtower container
//  6. VERIFY: Confirm the new container is running
//  7. CLEANUP OLD: Remove the old container
//
// If the new container fails to start or is not running, the old container is
// preserved for manual recovery. Automatic rollback is not supported because
// client.StartContainer creates a new container rather than restarting the old one.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker operations.
func RunOrchestrator(ctx context.Context, client container.Client) {
	clog := logrus.WithField("mode", "orchestrator")

	clog.Debug("Starting Watchtower self-update orchestrator")

	// Read environment variables.
	oldID, newImage, originalName, containerChain, err := readOrchestratorEnv()
	if err != nil {
		clog.WithError(err).Fatal("Failed to read orchestrator environment variables")
	}

	clog.WithFields(logrus.Fields{
		"old_id":          oldID,
		"new_image":       newImage,
		"original_name":   originalName,
		"container_chain": containerChain,
	}).Debug("Read orchestrator environment variables")

	// Create a timeout context for the entire orchestration.
	orchCtx, orchCancel := context.WithTimeout(
		ctx,
		orchestratorTimeout,
	)

	// Execute the orchestration sequence.
	err = orchestrateSelfUpdate(
		orchCtx,
		client,
		oldID,
		newImage,
		originalName,
		containerChain,
	)
	if err != nil {
		// Explicitly cancel before exit since deferred calls do not run on os.Exit.
		orchCancel()
		clog.WithError(err).Error("Orchestration failed")
		os.Exit(1)
	}

	// Explicitly cancel before exit since deferred calls do not run on os.Exit.
	orchCancel()
	clog.Debug("Self-update orchestration completed successfully")
	os.Exit(0)
}

// readOrchestratorEnv reads and validates the environment variables required
// by the orchestrator.
//
// Returns:
//   - string: Old container ID.
//   - string: New image reference.
//   - string: Original container name.
//   - string: Container chain for lineage tracking.
//   - error: Non-nil if any required variable is missing.
func readOrchestratorEnv() (string, string, string, string, error) {
	oldID := os.Getenv("WT_ORCHESTRATOR_OLD_ID")
	if oldID == "" {
		return "", "", "", "", fmt.Errorf(
			"%w: WT_ORCHESTRATOR_OLD_ID",
			errOrchestratorMissingEnv,
		)
	}

	newImage := os.Getenv("WT_ORCHESTRATOR_NEW_IMAGE")
	if newImage == "" {
		return "", "", "", "", fmt.Errorf(
			"%w: WT_ORCHESTRATOR_NEW_IMAGE",
			errOrchestratorMissingEnv,
		)
	}

	originalName := os.Getenv("WT_ORCHESTRATOR_ORIGINAL_NAME")
	if originalName == "" {
		return "", "", "", "", fmt.Errorf(
			"%w: WT_ORCHESTRATOR_ORIGINAL_NAME",
			errOrchestratorMissingEnv,
		)
	}

	containerChain := os.Getenv("WT_ORCHESTRATOR_CONTAINER_CHAIN")

	return oldID, newImage, originalName, containerChain, nil
}

// orchestrateSelfUpdate performs the container replacement sequence for a
// Watchtower self-update.
//
// It follows a deterministic state machine that inspects the old container,
// stops and removes it, creates and starts a new container from the updated
// image, and verifies the new container is running before returning.
//
// If the new container fails to start or is not running after the update,
// manual recovery is required because the old container has already been
// removed. Automatic rollback is not supported since the new container
// replaces the old one rather than renaming it.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker operations.
//   - oldID: ID of the old Watchtower container to replace.
//   - newImage: Image reference for the new container.
//   - originalName: Original container name to preserve on the new container.
//   - containerChain: Container chain label for lineage tracking.
//
// Returns:
//   - error: Non-nil if any step in the orchestration fails.
func orchestrateSelfUpdate(
	ctx context.Context,
	client container.Client,
	oldID string,
	newImage string,
	originalName string,
	containerChain string,
) error {
	clog := logrus.WithFields(logrus.Fields{
		"old_id":        oldID,
		"new_image":     newImage,
		"original_name": originalName,
	})

	// Inspect the old container and propagate the chain label.
	oldContainer, err := inspectOldContainer(
		ctx,
		client,
		clog,
		oldID,
		containerChain,
	)
	if err != nil {
		return err
	}

	// Stop and remove the old container to free its name.
	skipRemoval, err := stopAndRemoveOldContainer(
		ctx,
		client,
		clog,
		oldContainer,
	)
	if err != nil {
		return err
	}

	_ = skipRemoval // Removal is skipped only when StopContainer returns NotFound.

	// Create and start the new container.
	newContainerID, err := createAndStartNewContainer(
		ctx,
		client,
		clog,
		oldContainer,
	)
	if err != nil {
		return err
	}

	// Verify the new container is running, starting it explicitly if needed.
	err = ensureContainerRunning(ctx, client, clog, newContainerID)
	if err != nil {
		return err
	}

	// Emit the actual new container ID at Info level so notification templates
	// can consume the correct "new_id" field. The orchestrator container runs
	// after the source Watchtower process is stopped by the orchestrator.
	clog.WithField("new_id", newContainerID.ShortID()).
		Info("Started new container")

	return nil
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
//   - types.Container: The inspected old container.
//   - error: Non-nil if inspection fails.
func inspectOldContainer(
	ctx context.Context,
	client container.Client,
	clog *logrus.Entry,
	oldID string,
	containerChain string,
) (types.Container, error) {
	clog.Debug("Inspecting old container")

	oldContainer, err := client.GetContainer(
		ctx,
		types.ContainerID(oldID),
	)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			clog.Error("Old container not found")

			return nil, fmt.Errorf("%w: %s", errOrchestratorOldContainerNotFound, oldID)
		}

		clog.WithError(err).Error("Failed to inspect old container")

		return nil, fmt.Errorf("%w: %w", errOrchestratorInspectFailed, err)
	}

	if !oldContainer.IsRunning() {
		clog.Warn("Old container is not running, proceeding with creation only")
	}

	clog.WithField("old_name", oldContainer.Name()).
		Debug("Inspected old container successfully")

	// Propagate the container chain label to the old container's config.
	// This intentionally mutates the cached container config in-place so that
	// StartContainer's GetCreateConfig() will include the label on the new container.
	//
	// Note: This mutates the container object retrieved from GetContainer, which
	// could affect other code paths holding a reference to the same object. This
	// is safe here because the old container is about to be stopped and removed.
	if containerChain != "" {
		if c, ok := oldContainer.(*container.Container); ok {
			containerInfo := c.ContainerInfo()
			if containerInfo != nil && containerInfo.Config != nil {
				if containerInfo.Config.Labels == nil {
					containerInfo.Config.Labels = make(map[string]string)
				}

				// In-place mutation required: StartContainer reads labels from this
				// config to build the new container. Any other references to this
				// container object will also see this label after this assignment.
				containerInfo.Config.Labels[container.ContainerChainLabel] = containerChain
				clog.WithField("container_chain", containerChain).
					Debug("Set container chain label on source container config")
			}
		}
	}

	return oldContainer, nil
}

// stopAndRemoveOldContainer stops the old container and removes it to free its
// name for the new one. If StopContainer returns NotFound, removal is skipped
// because the Docker daemon already removed the container.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker operations.
//   - clog: Logger with container context fields.
//   - oldContainer: The old Watchtower container to stop and remove.
//
// Returns:
//   - skipRemoval: True if removal was skipped (container already gone).
//   - error: Non-nil if stopping or removing fails fatally.
func stopAndRemoveOldContainer(
	ctx context.Context,
	client container.Client,
	clog *logrus.Entry,
	oldContainer types.Container,
) (bool, error) {
	clog.Debug("Stopping old Watchtower container")

	skipRemoval := false

	err := client.StopContainer(
		ctx,
		oldContainer,
		orchestratorStopTimeout,
	)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			clog.Debug("Old container already removed")

			// Container was already removed during the stop attempt;
			// skip removal and proceed to creating the new one.
			skipRemoval = true
		} else {
			// StopContainer failed for a reason other than NotFound.
			// Re-inspect the container to get its current state, since the
			// old container object may be stale after the failed stop attempt.
			freshContainer, inspectErr := client.GetContainer(
				ctx,
				oldContainer.ID(),
			)
			if inspectErr != nil {
				clog.WithError(inspectErr).Error("Failed to re-inspect old container after stop failure")
				clog.WithError(err).Error("Failed to stop old container")

				return false, fmt.Errorf("%w: %w", errOrchestratorStopFailed, err)
			}

			if !freshContainer.IsRunning() {
				// Container stopped despite the error; proceed with removal.
				clog.Debug("Old container is not running after stop attempt, proceeding with removal")
			} else {
				// Container is still running and StopContainer failed — this is fatal.
				clog.WithError(err).Error("Failed to stop old container")

				return false, fmt.Errorf("%w: %w", errOrchestratorStopFailed, err)
			}
		}
	} else {
		// StopContainer succeeded.
		clog.Debug("Old container stopped")
	}

	// Remove the old container to free its name for the new one.
	// StartTargetContainer renames the new container to the source
	// container's name, which fails if a stopped container with the same
	// name still exists.
	//
	// This step is skipped if skipRemoval is true, which occurs when
	// StopContainer returns NotFound (container already removed).
	if !skipRemoval {
		clog.Debug("Removing old Watchtower container to free the name for the new container")

		err = client.RemoveContainer(ctx, oldContainer)
		if err != nil {
			if cerrdefs.IsNotFound(err) {
				clog.Debug("Old container already removed")
			} else {
				clog.WithError(err).Error("Failed to remove old container")

				return false, fmt.Errorf("%w: %w", errOrchestratorRemoveFailed, err)
			}
		} else {
			clog.Debug("Old container removed")
		}
	}

	return skipRemoval, nil
}

// createAndStartNewContainer creates and starts a new container using the old
// container's configuration. StartContainer handles config extraction, container
// creation, renaming, network attachment, and starting.
//
// StartContainer resolves the image from the source container's config
// (GetCreateConfig().Image), not from the WT_ORCHESTRATOR_NEW_IMAGE env var.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker operations.
//   - clog: Logger with container context fields.
//   - oldContainer: The old container whose config is used for the new one.
//
// Returns:
//   - types.ContainerID: ID of the newly created container.
//   - error: Non-nil if creation or starting fails.
func createAndStartNewContainer(
	ctx context.Context,
	client container.Client,
	clog *logrus.Entry,
	oldContainer types.Container,
) (types.ContainerID, error) {
	clog.Debug("Creating and starting new Watchtower container")

	newContainerID, err := client.StartContainer(
		ctx,
		oldContainer,
	)
	if err != nil {
		clog.WithError(err).Error("Failed to create and start new container")
		clog.Warn("Rollback unavailable: the old container has been removed. Manual intervention required.")

		return "", fmt.Errorf("%w: %w", errOrchestratorCreateFailed, err)
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
	client container.Client,
	clog *logrus.Entry,
	containerID types.ContainerID,
) error {
	clog.WithField("new_id", containerID.ShortID()).
		Debug("Verifying new container is running")

	ctr, err := client.GetContainer(ctx, containerID)
	if err != nil {
		clog.WithError(err).Error("Failed to inspect new container")
		clog.Warn("Cannot verify new container is running. Old container was removed; manual recovery requires recreating the container.")

		return fmt.Errorf("%w: %w", errOrchestratorInspectFailed, err)
	}

	if ctr.IsRunning() {
		clog.Debug("New container verified as running")

		return nil
	}

	// Container was created but not started; start it explicitly.
	clog.Debug("New container was created but not started, starting it now")

	err = client.StartContainerByID(ctx, containerID)
	if err != nil {
		clog.WithError(err).Error("Failed to start new container")
		clog.Warn("Rollback unavailable: the old container has been removed. Manual intervention required.")

		return fmt.Errorf("%w: %w", errOrchestratorStartFailed, err)
	}

	// Re-verify the container is running after the explicit start call.
	ctr, err = client.GetContainer(ctx, containerID)
	if err != nil {
		clog.WithError(err).Error("Failed to inspect new container after start")
		clog.Warn("Cannot verify new container is running. Old container was removed; manual recovery requires recreating the container.")

		return fmt.Errorf("%w: %w", errOrchestratorInspectFailed, err)
	}

	if !ctr.IsRunning() {
		clog.Error("New container is not running after explicit start. Old container was removed; manual recovery requires recreating the container.")

		return fmt.Errorf("%w: %s", errNewContainerNotRunning, containerID.ShortID())
	}

	clog.Debug("New container verified as running")

	return nil
}
