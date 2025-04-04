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
// It applies the provided network configuration to ensure settings are preserved.
// Returns the new container’s ID or an error if creation or startup fails.
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

	config := sourceContainer.GetCreateConfig()
	hostConfig := sourceContainer.GetCreateHostConfig()

	// Log network config details with client version context
	debugLogMacAddress(networkConfig, sourceContainer.ID(), clientVersion, minSupportedVersion)

	clog.Debug("Creating new container")

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
	if !sourceContainer.IsRunning() && !reviveStopped {
		clog.WithField("new_id", createdContainerID.ShortID()).
			Debug("Created container, not starting due to stopped state")

		return createdContainerID, nil
	}

	clog.WithField("new_id", createdContainerID.ShortID()).Debug("Starting new container")

	if err := api.ContainerStart(ctx, createdContainer.ID, dockerContainerType.StartOptions{}); err != nil {
		clog.WithError(err).
			WithField("new_id", createdContainerID.ShortID()).
			Debug("Failed to start new container")

		return createdContainerID, fmt.Errorf("%w: %w", errStartContainerFailed, err)
	}

	clog.WithField("new_id", createdContainerID.ShortID()).Info("Started new container")

	return createdContainerID, nil
}

// RenameTargetContainer renames an existing container to the specified new name.
// It logs the action and returns an error if the rename fails.
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

	clog.Debug("Renaming container")

	if err := api.ContainerRename(ctx, string(targetContainer.ID()), newName); err != nil {
		clog.WithError(err).Debug("Failed to rename container")

		return fmt.Errorf("%w: %w", errRenameContainerFailed, err)
	}

	clog.Debug("Renamed container successfully")

	return nil
}
