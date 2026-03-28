package container

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	dockerContainer "github.com/docker/docker/api/types/container"
	dockerNetwork "github.com/docker/docker/api/types/network"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Environment variable keys used by the ephemeral orchestrator.
const (
	// orchestratorOldIDEnv is the environment variable key for the old container ID.
	orchestratorOldIDEnv = "WT_ORCHESTRATOR_OLD_ID"
	// orchestratorNewImageEnv is the environment variable key for the new image reference.
	// Note: StartContainer resolves the image from the source container's config, not this var.
	// This env var is retained for debugging and future extensibility.
	orchestratorNewImageEnv = "WT_ORCHESTRATOR_NEW_IMAGE"
	// orchestratorOriginalNameEnv is the environment variable key for the original container name.
	orchestratorOriginalNameEnv = "WT_ORCHESTRATOR_ORIGINAL_NAME"
	// orchestratorContainerChainEnv is the environment variable key for the container chain label.
	orchestratorContainerChainEnv = "WT_ORCHESTRATOR_CONTAINER_CHAIN"
)

// CreateEphemeralOrchestrator creates a short-lived container that orchestrates
// the Watchtower self-update transition.
//
// The ephemeral container uses the same Watchtower image (already pulled) with
// the --self-update-orchestrator flag. It is configured with AutoRemove for
// automatic cleanup and mounts the Docker socket for container management.
//
// The ephemeral container does not set the watchtower label
// (com.centurylinklabs.watchtower = "true") to avoid being detected as an
// excess Watchtower instance by the scope and filter systems.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - sourceContainer: Current Watchtower container being replaced.
//   - newImage: Image reference for the new Watchtower container.
//   - containerChain: Container chain label for lineage tracking.
//
// Returns:
//   - types.ContainerID: ID of the ephemeral orchestrator container.
//   - error: Non-nil if creation or start fails, nil on success.
func (c *client) CreateEphemeralOrchestrator(
	ctx context.Context,
	sourceContainer types.Container,
	newImage string,
	containerChain string,
) (types.ContainerID, error) {
	clog := logrus.WithFields(logrus.Fields{
		"source_container": sourceContainer.Name(),
		"source_id":        sourceContainer.ID().ShortID(),
		"new_image":        newImage,
	})

	clog.Debug("Creating ephemeral orchestrator for self-update")

	// Build the orchestrator container configuration.
	config := buildOrchestratorConfig(sourceContainer, newImage, containerChain)
	hostConfig := buildOrchestratorHostConfig()

	// Generate a deterministic container name based on the source container ID.
	orchestratorName := "watchtower-orchestrator-" + sourceContainer.ID().ShortID()

	clog.WithField("orchestrator_name", orchestratorName).
		Debug("Creating ephemeral orchestrator container")

	// Create the container without specifying a platform.
	resp, err := c.api.ContainerCreate(
		ctx,
		config,
		hostConfig,
		&dockerNetwork.NetworkingConfig{},
		nil,
		orchestratorName,
	)
	if err != nil {
		clog.WithError(err).Error("Failed to create ephemeral orchestrator container")

		return "", fmt.Errorf("%w: %w", ErrEphemeralCreateFailed, err)
	}

	orchestratorID := types.ContainerID(resp.ID)

	clog.WithField("orchestrator_id", orchestratorID.ShortID()).
		Debug("Created ephemeral orchestrator container")

	// Start the orchestrator container.
	err = c.api.ContainerStart(
		ctx,
		resp.ID,
		dockerContainer.StartOptions{},
	)
	if err != nil {
		clog.WithError(err).Error("Failed to start ephemeral orchestrator container")

		// Attempt cleanup of the created but not-started container.
		cleanupErr := c.api.ContainerRemove(
			ctx,
			resp.ID,
			dockerContainer.RemoveOptions{Force: true},
		)
		if cleanupErr != nil {
			clog.WithError(cleanupErr).
				Warn("Failed to clean up ephemeral orchestrator after start failure")
		}

		return "", fmt.Errorf("%w: %w", ErrEphemeralStartFailed, err)
	}

	clog.WithField("orchestrator_id", orchestratorID.ShortID()).
		Debug("Started ephemeral orchestrator for self-update")

	return orchestratorID, nil
}

// buildOrchestratorConfig builds the Docker container configuration for the
// ephemeral orchestrator.
//
// The configuration:
//   - Uses the same Watchtower image (no separate image pull needed)
//   - Runs with --self-update-orchestrator flag
//   - Passes old container ID, new image, original name, and container chain via environment
//   - Sets the orchestrator label for identification
//   - Omits the watchtower label and scope label to avoid excess instance detection
//
// Parameters:
//   - sourceContainer: Current Watchtower container.
//   - newImage: Image reference for the new container.
//   - containerChain: Container chain label for lineage tracking.
//
// Returns:
//   - *dockerContainer.Config: The container configuration.
func buildOrchestratorConfig(
	sourceContainer types.Container,
	newImage string,
	containerChain string,
) *dockerContainer.Config {
	return &dockerContainer.Config{
		Image: newImage,
		Cmd:   []string{"--self-update-orchestrator"},
		Env: []string{
			fmt.Sprintf("%s=%s", orchestratorOldIDEnv, sourceContainer.ID()),
			fmt.Sprintf("%s=%s", orchestratorNewImageEnv, newImage),
			fmt.Sprintf("%s=%s", orchestratorOriginalNameEnv, sourceContainer.Name()),
			fmt.Sprintf("%s=%s", orchestratorContainerChainEnv, containerChain),
		},
		Labels: map[string]string{
			// Orchator label only — watchtower label omitted to avoid excess instance detection.
			OrchestratorLabel: "true",
		},
	}
}

// buildOrchestratorHostConfig builds the Docker host configuration for the
// ephemeral orchestrator.
//
// The configuration ensures:
//   - AutoRemove for automatic cleanup on exit
//   - Docker socket mount for container management
//   - No port bindings to avoid conflicts
//   - No restart policy (one-shot container)
//
// Returns:
//   - *dockerContainer.HostConfig: The host configuration.
func buildOrchestratorHostConfig() *dockerContainer.HostConfig {
	return &dockerContainer.HostConfig{
		AutoRemove: true,
		Binds:      []string{"/var/run/docker.sock:/var/run/docker.sock"},
		// No port bindings — avoids conflicts with the new Watchtower container
		// No restart policy — one-shot container that exits after orchestration
	}
}

// RemoveOrphanedOrchestrators removes any ephemeral orchestrator containers
// that may have persisted due to crashes or unexpected termination.
//
// This is called during startup alongside RemoveExcessWatchtowerInstances to
// ensure a clean state.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - client: Container client for Docker operations.
//
// Returns:
//   - int: Number of orphaned orchestrators removed.
//   - error: Non-nil if listing or removal fails, nil on success.
func RemoveOrphanedOrchestrators(
	ctx context.Context,
	client Client,
) (int, error) {
	clog := logrus.WithField("function", "RemoveOrphanedOrchestrators")

	clog.Debug("Checking for orphaned ephemeral orchestrator containers")

	// List all containers to find orphaned orchestrators.
	allContainers, err := client.ListContainers(ctx)
	if err != nil {
		clog.WithError(err).Error("Failed to list containers for orchestrator cleanup")

		return 0, fmt.Errorf("failed to list containers: %w", err)
	}

	removed := 0

	for _, c := range allContainers {
		containerInfo := c.ContainerInfo()
		if containerInfo == nil || containerInfo.Config == nil {
			continue
		}

		// Check for the orchestrator label.
		if containerInfo.Config.Labels[OrchestratorLabel] != "true" {
			continue
		}

		clog.WithFields(logrus.Fields{
			"container": c.Name(),
			"id":        c.ID().ShortID(),
		}).Info("Removing orphaned ephemeral orchestrator container")

		err := client.StopAndRemoveContainer(ctx, c, 0)
		if err != nil {
			clog.WithError(err).WithFields(logrus.Fields{
				"container": c.Name(),
				"id":        c.ID().ShortID(),
			}).Warn("Failed to remove orphaned orchestrator container")

			continue
		}

		removed++
	}

	if removed > 0 {
		clog.WithField("count", removed).
			Info("Removed orphaned ephemeral orchestrator containers")
	} else {
		clog.Debug("No orphaned ephemeral orchestrator containers found")
	}

	return removed, nil
}
