package container

import (
	"context"
	"fmt"

	dockerContainerType "github.com/docker/docker/api/types/container"
	dockerNetworkType "github.com/docker/docker/api/types/network"
	dockerClient "github.com/docker/docker/client"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// StartTargetContainer creates and starts a new container using the source container’s configuration.
// It applies the provided network configuration to ensure settings are preserved.
// Returns the new container’s ID or an error if creation or startup fails.
func StartTargetContainer(api dockerClient.APIClient, sourceContainer types.Container, networkConfig *dockerNetworkType.NetworkingConfig, reviveStopped bool) (types.ContainerID, error) {
	ctx := context.Background()
	config := sourceContainer.GetCreateConfig()
	hostConfig := sourceContainer.GetCreateHostConfig()

	// Log the MAC address being preserved for debugging
	_ = getDesiredMacAddress(networkConfig, sourceContainer.ID())

	name := sourceContainer.Name()
	logrus.Infof("Creating %s", name)

	// Create the new container with the exact network configuration from the source
	createdContainer, err := api.ContainerCreate(ctx, config, hostConfig, networkConfig, nil, name)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	createdContainerID := types.ContainerID(createdContainer.ID)
	if !sourceContainer.IsRunning() && !reviveStopped {
		return createdContainerID, nil
	}

	// Start the new container
	logrus.Debugf("Starting container %s (%s)", name, createdContainerID.ShortID())

	if err := api.ContainerStart(ctx, createdContainer.ID, dockerContainerType.StartOptions{
		CheckpointID:  "",
		CheckpointDir: "",
	}); err != nil {
		return createdContainerID, fmt.Errorf("failed to start container: %w", err)
	}

	return createdContainerID, nil
}

// RenameTargetContainer renames an existing container to the specified new name.
// It logs the action and returns an error if the rename fails.
func RenameTargetContainer(api dockerClient.APIClient, target types.Container, newName string) error {
	ctx := context.Background()

	logrus.Debugf("Renaming container %s (%s) to %s", target.Name(), target.ID().ShortID(), newName)

	if err := api.ContainerRename(ctx, string(target.ID()), newName); err != nil {
		return fmt.Errorf("failed to rename container %s to %s: %w", target.ID(), newName, err)
	}

	return nil
}
