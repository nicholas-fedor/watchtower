package container

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/versions"
	"github.com/sirupsen/logrus"

	dockerContainerType "github.com/docker/docker/api/types/container"
	dockerNetworkType "github.com/docker/docker/api/types/network"
	dockerClient "github.com/docker/docker/client"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// StartTargetContainer creates and starts a new container using the source container’s configuration.
//
// It applies the provided network configuration and respects the reviveStopped option.
// For legacy Docker API versions (< 1.44) with multiple networks, it creates the container with a single
// network and attaches others sequentially to avoid issues with multiple network endpoints in ContainerCreate.
// For modern API versions (>= 1.44) or single networks, it attaches all networks at creation.
//
// Parameters:
//   - api: Docker API client.
//   - sourceContainer: Container to replicate.
//   - networkConfig: Network settings to apply.
//   - reviveStopped: Whether to start stopped containers.
//   - clientVersion: API version of the client.
//   - minSupportedVersion: Minimum API version for full features.
//   - disableMemorySwappiness: Whether to disable memory swappiness for Podman compatibility.
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
	disableMemorySwappiness bool,
) (types.ContainerID, error) {
	ctx := context.Background()
	clog := logrus.WithFields(logrus.Fields{
		"container": sourceContainer.Name(),
		"id":        sourceContainer.ID().ShortID(),
	})

	// Extract configuration from the source container.
	config := sourceContainer.GetCreateConfig()
	hostConfig := sourceContainer.GetCreateHostConfig()

	// Set MemorySwappiness to nil for Podman compatibility if flag is enabled.
	if disableMemorySwappiness {
		hostConfig.MemorySwappiness = nil

		clog.Debug("Disabled memory swappiness for Podman compatibility")
	}

	// Log network details for debugging, including MAC address validation.
	isHostNetwork := sourceContainer.ContainerInfo().HostConfig.NetworkMode.IsHost()
	debugLogMacAddress(
		networkConfig,
		sourceContainer.ID(),
		clientVersion,
		minSupportedVersion,
		isHostNetwork,
	)

	// Determine network config for container creation based on API version.
	createNetworkConfig := networkConfig

	if versions.LessThan(clientVersion, "1.44") && len(networkConfig.EndpointsConfig) > 1 {
		// Legacy API (< 1.44) with multiple networks: use first network for creation.
		var firstNetworkName string

		createNetworkConfig = newEmptyNetworkConfig()

		for name, endpoint := range networkConfig.EndpointsConfig {
			firstNetworkName = name
			createNetworkConfig.EndpointsConfig[name] = endpoint

			clog.WithField("network", firstNetworkName).
				Debug("Selected first network for container creation")

			break // Use only the first network initially.
		}
	} else {
		clog.Debug("Using full network config for API version >= 1.44 or single network")
	}

	// Create container with the selected network config.
	clog.Debug("Creating new container")

	createdContainer, err := api.ContainerCreate(
		ctx,
		config,
		hostConfig,
		createNetworkConfig,
		nil,
		sourceContainer.Name(),
	)
	if err != nil {
		clog.WithError(err).Debug("Failed to create new container")

		return "", fmt.Errorf("%w: %w", errCreateContainerFailed, err)
	}

	createdContainerID := types.ContainerID(createdContainer.ID)
	clog.WithField("new_id", createdContainerID.ShortID()).Debug("Created container successfully")

	// Attach additional networks for legacy API if needed.
	if versions.LessThan(clientVersion, "1.44") && len(networkConfig.EndpointsConfig) > 1 {
		if err := attachNetworks(ctx, api, createdContainer.ID, networkConfig, createNetworkConfig, clog); err != nil {
			// Clean up the created container to avoid orphaned resources.
			if rmErr := api.ContainerRemove(ctx, createdContainer.ID, dockerContainerType.RemoveOptions{Force: true}); rmErr != nil {
				clog.WithError(rmErr).
					Warn("Failed to clean up container after network attachment error")
			}

			return "", err
		}
	}

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

// attachNetworks connects a container to additional networks for legacy API versions.
//
// It iterates through the provided network config, attaching all networks not included in the initial
// creation config, ensuring compatibility with Docker API < 1.44 where multiple network endpoints may fail.
//
// Parameters:
//   - ctx: Context for API operations.
//   - api: Docker API client.
//   - containerID: ID of the container to attach networks to.
//   - networkConfig: Full network configuration with all desired endpoints.
//   - initialNetworkConfig: Network config used during container creation.
//   - clog: Logger with container context for consistent logging.
//
// Returns:
//   - error: Non-nil if attaching any network fails, nil on success.
func attachNetworks(
	ctx context.Context,
	api dockerClient.APIClient,
	containerID string,
	networkConfig *dockerNetworkType.NetworkingConfig,
	initialNetworkConfig *dockerNetworkType.NetworkingConfig,
	clog *logrus.Entry,
) error {
	// Identify the initial network used during creation to skip it.
	var initialNetworkName string
	for name := range initialNetworkConfig.EndpointsConfig {
		initialNetworkName = name

		break
	}

	// Attach each additional network sequentially.
	for name, endpoint := range networkConfig.EndpointsConfig {
		if name != initialNetworkName && name != "" {
			clog.WithField("network", name).Debug("Attaching additional network to container")

			if err := api.NetworkConnect(ctx, name, containerID, endpoint); err != nil {
				clog.WithError(err).
					WithField("network", name).
					Error("Failed to attach additional network")

				return fmt.Errorf("failed to attach network %s: %w", name, err)
			}

			clog.WithField("network", name).Debug("Successfully attached additional network")
		}
	}

	return nil
}

// RenameTargetContainer renames an existing container to the specified target name.
//
// Parameters:
//   - api: Docker API client.
//   - targetContainer: Container to rename.
//   - targetName: New name for the container.
//
// Returns:
//   - error: Non-nil if rename fails, nil on success.
func RenameTargetContainer(
	api dockerClient.APIClient,
	targetContainer types.Container,
	targetName string,
) error {
	ctx := context.Background()
	clog := logrus.WithFields(logrus.Fields{
		"container":   targetContainer.Name(),
		"id":          targetContainer.ID().ShortID(),
		"target_name": targetName,
	})

	// Attempt to rename the container.
	clog.Debug("Renaming container")

	if err := api.ContainerRename(ctx, string(targetContainer.ID()), targetName); err != nil {
		clog.WithError(err).Debug("Failed to rename container")

		return fmt.Errorf("%w: %w", errRenameContainerFailed, err)
	}

	clog.Debug("Renamed container successfully")

	return nil
}
