package container

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	dockerContainerType "github.com/docker/docker/api/types/container"
	dockerNetworkType "github.com/docker/docker/api/types/network"
	dockerClient "github.com/docker/docker/client"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// StartTargetContainer creates and starts a new container using the source container’s configuration.
//
// It applies the provided network configuration and respects the reviveStopped option.
//
// Parameters:
//   - api: Docker API client.
//   - sourceContainer: Container to replicate.
//   - networkConfig: Network settings to apply.
//   - reviveStopped: Whether to start stopped containers.
//   - clientVersion: API version of the client.
//   - minSupportedVersion: Minimum API version for full features.
//
// Returns:
//   - types.ContainerID: ID of the new container.
//   - error: Non-nil if creation or start fails, nil on success.
func StartTargetContainer(
	api dockerClient.APIClient,
	sourceContainer types.Container,
	networkConfig *dockerNetworkType.NetworkingConfig,
	reviveStopped bool,
	clientVersion string,
	minSupportedVersion string,
) (types.ContainerID, error) {
	ctx := context.Background()
	clog := logrus.WithFields(logrus.Fields{
		"container": sourceContainer.Name(),
		"id":        sourceContainer.ID().ShortID(),
	})

	// Extract configuration from the source container.
	config := sourceContainer.GetCreateConfig()
	hostConfig := sourceContainer.GetCreateHostConfig()

	// Log network details for debugging.
	isHostNetwork := sourceContainer.ContainerInfo().HostConfig.NetworkMode.IsHost()
	debugLogMacAddress(
		networkConfig,
		sourceContainer.ID(),
		clientVersion,
		minSupportedVersion,
		isHostNetwork,
	)

	clog.Debug("Creating new container")

	// Create the new container with source config and network settings.
	createdContainer, err := api.ContainerCreate(
		ctx,
		config,
		hostConfig,
		networkConfig,
		nil,
		sourceContainer.Name(),
	)
	if err != nil {
		clog.WithError(err).Debug("Failed to create new container")

		return "", fmt.Errorf("%w: %w", errCreateContainerFailed, err)
	}

	createdContainerID := types.ContainerID(createdContainer.ID)

	// Skip starting if source isn’t running and revive isn’t enabled.
	if !sourceContainer.IsRunning() && !reviveStopped {
		clog.WithField("new_id", createdContainerID.ShortID()).
			Debug("Created container, not starting due to stopped state")

		return createdContainerID, nil
	}

	// Start the newly created container.
	clog.WithField("new_id", createdContainerID.ShortID()).Debug("Starting new container")

	if err := api.ContainerStart(ctx, createdContainer.ID, dockerContainerType.StartOptions{}); err != nil {
		clog.WithError(err).
			WithField("new_id", createdContainerID.ShortID()).
			Debug("Failed to start new container")

		return createdContainerID, fmt.Errorf("%w: %w", errStartContainerFailed, err)
	}

	// Log detailed start message
	clog.WithField("new_id", createdContainerID.ShortID()).Info("Started new container")

	return createdContainerID, nil
}

// RenameTargetContainer renames an existing container to the specified new name.
//
// Parameters:
//   - api: Docker API client.
//   - targetContainer: Container to rename.
//   - newName: New name for the container.
//
// Returns:
//   - error: Non-nil if rename fails, nil on success.
func RenameTargetContainer(
	api dockerClient.APIClient,
	targetContainer types.Container,
	newName string,
) error {
	ctx := context.Background()
	clog := logrus.WithFields(logrus.Fields{
		"container": targetContainer.Name(),
		"id":        targetContainer.ID().ShortID(),
		"new_name":  newName,
	})

	// Attempt to rename the container.
	clog.Debug("Renaming container")

	if err := api.ContainerRename(ctx, string(targetContainer.ID()), newName); err != nil {
		clog.WithError(err).Debug("Failed to rename container")

		return fmt.Errorf("%w: %w", errRenameContainerFailed, err)
	}

	clog.Debug("Renamed container successfully")

	return nil
}
