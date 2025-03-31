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
	config := sourceContainer.GetCreateConfig()
	hostConfig := sourceContainer.GetCreateHostConfig()

	// Log network config details with client version context
	debugLogMacAddress(networkConfig, sourceContainer.ID(), clientVersion, minSupportedVersion)

	name := sourceContainer.Name()
	logrus.Debugf("Starting target container: Creating %s", name)

	createdContainer, err := api.ContainerCreate(ctx, config, hostConfig, networkConfig, nil, name)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	createdContainerID := types.ContainerID(createdContainer.ID)
	if !sourceContainer.IsRunning() && !reviveStopped {
		return createdContainerID, nil
	}

	logrus.Debugf("Starting target container: Starting %s (%s)", name, createdContainerID.ShortID())

	if err := api.ContainerStart(ctx, createdContainer.ID, dockerContainerType.StartOptions{
		CheckpointID:  "",
		CheckpointDir: "",
	}); err != nil {
		return createdContainerID, fmt.Errorf(
			"failed to start container: %w",
			err,
		)
	}

	logrus.Infof("Started new container %s (%s)", name, createdContainerID.ShortID())

	return createdContainerID, nil
}

// RenameTargetContainer renames an existing container to the specified new name.
// It logs the action and returns an error if the rename fails.
func RenameTargetContainer(
	api dockerClient.APIClient,
	target types.Container,
	newName string,
) error {
	ctx := context.Background()

	logrus.Debugf(
		"Renaming target container: %s (%s) to %s",
		target.Name(),
		target.ID().ShortID(),
		newName,
	)

	if err := api.ContainerRename(ctx, string(target.ID()), newName); err != nil {
		return fmt.Errorf(
			"failed to rename container %s to %s: %w",
			target.ID(),
			newName,
			err,
		)
	}

	return nil
}
