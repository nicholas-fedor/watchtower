package container

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	dockerContainerType "github.com/docker/docker/api/types/container"
	dockerFiltersType "github.com/docker/docker/api/types/filters"
	dockerNetworkType "github.com/docker/docker/api/types/network"
	dockerClient "github.com/docker/docker/client"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// defaultStopSignal defines the default signal used to stop containers when no custom signal is specified.
// It is set to "SIGTERM" to allow containers to terminate gracefully by default.
const defaultStopSignal = "SIGTERM"

// ListSourceContainers retrieves a list of containers from the Docker host, filtered by the provided function.
// It respects the IncludeStopped and IncludeRestarting options to determine which container states to include.
// Returns a slice of containers or an error if the listing fails.
func ListSourceContainers(
	api dockerClient.APIClient,
	opts ClientOptions,
	filter types.Filter,
) ([]types.Container, error) {
	hostContainers := []types.Container{}
	ctx := context.Background()

	// Log the scope of containers being retrieved based on configuration.
	switch {
	case opts.IncludeStopped && opts.IncludeRestarting:
		logrus.Debug("Retrieving running, stopped, restarting and exited containers")
	case opts.IncludeStopped:
		logrus.Debug("Retrieving running, stopped and exited containers")
	case opts.IncludeRestarting:
		logrus.Debug("Retrieving running and restarting containers")
	default:
		logrus.Debug("Retrieving running containers")
	}

	// Apply filters based on configured options.
	filterArgs := dockerFiltersType.NewArgs()
	filterArgs.Add("status", "running")

	if opts.IncludeStopped {
		filterArgs.Add("status", "created")
		filterArgs.Add("status", "exited")
	}

	if opts.IncludeRestarting {
		filterArgs.Add("status", "restarting")
	}

	containers, err := api.ContainerList(ctx, dockerContainerType.ListOptions{
		Filters: filterArgs,
		Size:    false,
		All:     false,
		Latest:  false,
		Since:   "",
		Before:  "",
		Limit:   0,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	// Fetch detailed info for each container and apply the user-provided filter.
	for _, runningContainer := range containers {
		container, err := GetSourceContainer(api, types.ContainerID(runningContainer.ID))
		if err != nil {
			return nil, err
		}

		if filter(container) {
			hostContainers = append(hostContainers, container)
		}
	}

	return hostContainers, nil
}

// GetSourceContainer retrieves detailed information about a container by its ID.
// It resolves network container references by replacing IDs with names when possible.
// Returns a Container object or an error if inspection fails.
func GetSourceContainer(
	api dockerClient.APIClient,
	containerID types.ContainerID,
) (types.Container, error) {
	ctx := context.Background()

	// Fetch basic container information.
	containerInfo, err := api.ContainerInspect(ctx, string(containerID))
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container %s: %w", containerID, err)
	}

	// Handle container network mode dependencies.
	netType, netContainerID, found := strings.Cut(string(containerInfo.HostConfig.NetworkMode), ":")
	if found && netType == "container" {
		parentContainer, err := api.ContainerInspect(ctx, netContainerID)
		if err != nil {
			logrus.WithFields(map[string]any{
				"container":         containerInfo.Name,
				"error":             err,
				"network-container": netContainerID,
			}).Warnf("Unable to resolve network container: %v", err)
		} else {
			// Update NetworkMode to use the parent container's name for stable references across recreations.
			containerInfo.HostConfig.NetworkMode = dockerContainerType.NetworkMode("container:" + parentContainer.Name)
		}
	}

	// Fetch associated image information.
	imageInfo, err := api.ImageInspect(ctx, containerInfo.Image)
	if err != nil {
		logrus.Warnf("Failed to retrieve container image info: %v", err)

		return NewContainer(&containerInfo, nil), nil
	}

	return NewContainer(&containerInfo, &imageInfo), nil
}

// StopSourceContainer stops and removes the specified container within the given timeout.
// It first attempts to stop the container gracefully, then removes it unless AutoRemove is enabled.
// Returns an error if stopping or removal fails.
func StopSourceContainer(
	api dockerClient.APIClient,
	sourceContainer types.Container,
	timeout time.Duration,
	removeVolumes bool,
) error {
	ctx := context.Background()
	idStr := string(sourceContainer.ID())
	shortID := sourceContainer.ID().ShortID()

	// Stop the container if it’s running.
	signal := sourceContainer.StopSignal()
	if signal == "" {
		signal = defaultStopSignal
	}

	if sourceContainer.IsRunning() {
		logrus.Infof("Stopping %s (%s) with %s", sourceContainer.Name(), shortID, signal)

		if err := api.ContainerKill(ctx, idStr, signal); err != nil {
			return fmt.Errorf(
				"failed to stop container %s (%s): %w",
				sourceContainer.Name(),
				shortID,
				err,
			)
		}
	}

	return stopAndRemoveContainer(api, sourceContainer, timeout, removeVolumes)
}

// stopAndRemoveContainer waits for a container to stop and removes it if needed.
// It respects AutoRemove and logs progress, returning an error if the process fails.
func stopAndRemoveContainer(
	api dockerClient.APIClient,
	sourceContainer types.Container,
	timeout time.Duration,
	removeVolumes bool,
) error {
	ctx := context.Background()
	idStr := string(sourceContainer.ID())
	shortID := sourceContainer.ID().ShortID()

	// Wait for the container to stop or timeout.
	stopped, err := waitForStopOrTimeout(api, sourceContainer, timeout)
	if err != nil {
		return fmt.Errorf(
			"failed to wait for container %s (%s) to stop: %w",
			sourceContainer.Name(),
			shortID,
			err,
		)
	}

	if !stopped {
		logrus.Warnf(
			"Container %s (%s) did not stop within %v",
			sourceContainer.Name(),
			shortID,
			timeout,
		)
	}

	// If already gone and AutoRemove is enabled, no further action needed.
	if stopped && sourceContainer.ContainerInfo().HostConfig.AutoRemove {
		logrus.Debugf("AutoRemove container %s, skipping ContainerRemove call.", shortID)

		return nil
	}

	// Attempt removal.
	logrus.Debugf("Removing container %s", shortID)

	err = api.ContainerRemove(ctx, idStr, dockerContainerType.RemoveOptions{
		Force:         true,
		RemoveVolumes: removeVolumes,
	})
	if err != nil && !dockerClient.IsErrNotFound(err) {
		return fmt.Errorf(
			"failed to remove container %s (%s): %w",
			sourceContainer.Name(),
			shortID,
			err,
		)
	}

	if dockerClient.IsErrNotFound(err) {
		// Container was already gone after removal attempt; no need for second wait.
		return nil
	}

	// Confirm removal if it succeeded or container wasn’t gone before.
	stopped, err = waitForStopOrTimeout(api, sourceContainer, timeout)
	if err != nil {
		return fmt.Errorf(
			"failed to confirm removal of container %s (%s): %w",
			sourceContainer.Name(),
			shortID,
			err,
		)
	}

	if !stopped {
		return fmt.Errorf("%w: %s (%s)", errContainerNotRemoved, sourceContainer.Name(), shortID)
	}

	return nil
}

// waitForStopOrTimeout waits for a container to stop or times out.
// Returns true if stopped (or gone), false if still running after timeout, and any error.
// Treats a 404 (not found) as stopped, indicating successful removal or prior stop.
func waitForStopOrTimeout(
	api dockerClient.APIClient,
	container types.Container,
	waitTime time.Duration,
) (bool, error) {
	ctx := context.Background()
	timeout := time.After(waitTime)

	for {
		select {
		case <-timeout:
			return false, nil // Timed out, container still running
		default:
			containerInfo, err := api.ContainerInspect(ctx, string(container.ID()))
			if err != nil {
				if dockerClient.IsErrNotFound(err) {
					return true, nil // Container gone, treat as stopped
				}

				return false, fmt.Errorf(
					"failed to inspect container %s: %w",
					container.ID(),
					err,
				) // Other errors propagate
			}

			if !containerInfo.State.Running {
				return true, nil // Stopped successfully
			}
		}
		time.Sleep(1 * time.Second)
	}
}

// getNetworkConfig extracts the network configuration from the source container.
// It preserves essential settings (e.g., IP, MAC) while resetting DNSNames and Aliases to minimal values.
// Returns a sanitized network configuration for use in creating the target container.
func getNetworkConfig(sourceContainer types.Container) *dockerNetworkType.NetworkingConfig {
	config := &dockerNetworkType.NetworkingConfig{
		EndpointsConfig: make(map[string]*dockerNetworkType.EndpointSettings),
	}

	for networkName, originalEndpoint := range sourceContainer.ContainerInfo().NetworkSettings.Networks {
		// Copy all fields from the original endpoint
		endpoint := &dockerNetworkType.EndpointSettings{
			IPAMConfig: originalEndpoint.IPAMConfig, // Preserve full IPAM config
			Links:      originalEndpoint.Links,      // Preserve container links
			Aliases: []string{
				sourceContainer.Name()[1:],
			}, // Reset to container name only
			DriverOpts:          originalEndpoint.DriverOpts,          // Preserve driver options
			GwPriority:          originalEndpoint.GwPriority,          // Preserve gateway priority
			NetworkID:           originalEndpoint.NetworkID,           // Preserve network ID
			EndpointID:          originalEndpoint.EndpointID,          // Preserve endpoint ID
			Gateway:             originalEndpoint.Gateway,             // Preserve gateway
			IPAddress:           originalEndpoint.IPAddress,           // Preserve IP address
			IPPrefixLen:         originalEndpoint.IPPrefixLen,         // Preserve IP prefix length
			IPv6Gateway:         originalEndpoint.IPv6Gateway,         // Preserve IPv6 gateway
			GlobalIPv6Address:   originalEndpoint.GlobalIPv6Address,   // Preserve global IPv6 address
			GlobalIPv6PrefixLen: originalEndpoint.GlobalIPv6PrefixLen, // Preserve IPv6 prefix length
			MacAddress:          originalEndpoint.MacAddress,          // Preserve MAC address
			DNSNames: []string{
				sourceContainer.Name()[1:],
			}, // Reset to container name only
		}

		// Preserve IPAMConfig if present
		if originalEndpoint.IPAMConfig != nil {
			endpoint.IPAMConfig = &dockerNetworkType.EndpointIPAMConfig{
				IPv4Address:  originalEndpoint.IPAMConfig.IPv4Address,
				IPv6Address:  originalEndpoint.IPAMConfig.IPv6Address,
				LinkLocalIPs: originalEndpoint.IPAMConfig.LinkLocalIPs,
			}
		}

		config.EndpointsConfig[networkName] = endpoint
		logrus.Debugf(
			"Preserving network config for %s on %s: MAC=%s, IP=%s",
			sourceContainer.Name(),
			networkName,
			endpoint.MacAddress,
			endpoint.IPAddress,
		)
	}

	return config
}

// getDesiredMacAddress extracts the first MAC address from the network config for logging.
// It logs the MAC address being preserved and returns it for debugging purposes.
// Returns an empty string if no MAC address is found.
func getDesiredMacAddress(
	networkConfig *dockerNetworkType.NetworkingConfig,
	containerID types.ContainerID,
) string {
	for networkName, endpoint := range networkConfig.EndpointsConfig {
		if endpoint.MacAddress != "" {
			logrus.Debugf(
				"Preserving MAC address %s for container %s on network %s",
				endpoint.MacAddress,
				containerID,
				networkName,
			)

			return endpoint.MacAddress
		}
	}

	logrus.Warnf("No MAC address found for container %s", containerID)

	return ""
}
