package container

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types/versions"
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
		logrus.Debug(
			"Listing source containers: Retrieving running, stopped, restarting and exited containers",
		)
	case opts.IncludeStopped:
		logrus.Debug("Listing source containers: Retrieving running, stopped and exited containers")
	case opts.IncludeRestarting:
		logrus.Debug("Listing source containers: Retrieving running and restarting containers")
	default:
		logrus.Debug("Listing source containers: Retrieving running containers")
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
		return nil, fmt.Errorf(
			"failed to inspect container %s: %w",
			containerID,
			err,
		)
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
			}).Warnf("Getting source container info: Unable to resolve network container: %v", err)
		} else {
			// Update NetworkMode to use the parent container's name for stable references across recreations.
			containerInfo.HostConfig.NetworkMode = dockerContainerType.NetworkMode("container:" + parentContainer.Name)
		}
	}

	// Fetch associated image information.
	imageInfo, err := api.ImageInspect(ctx, containerInfo.Image)
	if err != nil {
		logrus.Warnf("Get source container info: Failed to retrieve container image info: %v", err)

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
		logrus.Infof(
			"Stopping source container: Stopping %s (%s) with %s",
			sourceContainer.Name(),
			shortID,
			signal,
		)

		if err := api.ContainerKill(ctx, idStr, signal); err != nil {
			return fmt.Errorf(
				"failed stopping container %s (%s): %w",
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
			"Stopping source container: %s (%s) did not stop within %v",
			sourceContainer.Name(),
			shortID,
			timeout,
		)
	}

	// If already gone and AutoRemove is enabled, no further action needed.
	if stopped && sourceContainer.ContainerInfo().HostConfig.AutoRemove {
		logrus.Debugf(
			"Removing source container: AutoRemove container %s, skipping ContainerRemove call.",
			shortID,
		)

		return nil
	}

	// Attempt removal.
	logrus.Debugf("Removing source container: Removing container %s", shortID)

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

// getLegacyNetworkConfig returns the network configuration of the source container.
// It duplicates Watchtower's original function to maintain compatibility with pre-version 1.44 API clients.
// See Docker's source code for implementation details: https://github.com/moby/moby/blob/master/client/container_create.go
func getLegacyNetworkConfig(
	sourceContainer types.Container,
	clientVersion string,
) *dockerNetworkType.NetworkingConfig {
	networks := sourceContainer.ContainerInfo().NetworkSettings.Networks
	if networks == nil {
		logrus.Warnf(
			"Getting (legacy) network config using API version %s: No network settings found for container %s",
			clientVersion,
			sourceContainer.ID(),
		)

		return &dockerNetworkType.NetworkingConfig{
			EndpointsConfig: make(map[string]*dockerNetworkType.EndpointSettings),
		}
	}

	config := &dockerNetworkType.NetworkingConfig{
		EndpointsConfig: make(map[string]*dockerNetworkType.EndpointSettings),
	}

	for networkName, endpoint := range networks {
		if endpoint == nil {
			logrus.Warnf(
				"Getting (legacy) network config using API version %s: Nil endpoint for network %s in container %s.",
				clientVersion,
				networkName,
				sourceContainer.ID(),
			)

			continue
		}
		// Create a minimal endpoint config without MAC address
		newEndpoint := &dockerNetworkType.EndpointSettings{
			NetworkID:  endpoint.NetworkID,
			EndpointID: endpoint.EndpointID,
			Gateway:    endpoint.Gateway,
			IPAddress:  endpoint.IPAddress,
			Aliases:    filterAliases(endpoint.Aliases, sourceContainer.ID().ShortID()),
		}
		config.EndpointsConfig[networkName] = newEndpoint
		// Log MAC address for visibility, but don’t include it
		if endpoint.MacAddress != "" {
			logrus.Debugf(
				"Getting (legacy) network config using API version %s: Found MAC address %s for container %s on network %s.",
				clientVersion,
				endpoint.MacAddress,
				sourceContainer.Name(),
				networkName,
			)
		}
	}

	// Warn if MAC preservation is desired but not possible
	if len(networks) > 0 {
		logrus.Warnf(
			"Getting (legacy) network config using API version %s: Container %s MAC addresses cannot be preserved. Docker will dynamically assign new ones.",
			clientVersion,
			sourceContainer.ID(),
		)
	}

	return config
}

func filterAliases(aliases []string, shortID string) []string {
	result := make([]string, 0, len(aliases))

	for _, alias := range aliases {
		if alias != shortID {
			result = append(result, alias)
		}
	}

	return result
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
			IPAMConfig:          originalEndpoint.IPAMConfig,          // Preserve full IPAM config
			Links:               originalEndpoint.Links,               // Preserve container links
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
			MacAddress:          originalEndpoint.MacAddress,          // Preserve endpoint MAC address if API Version > 1.43
		}

		// Only set Aliases and DNSNames for user-defined networks and not the default "bridge" network.
		if networkName != "bridge" {
			endpoint.Aliases = []string{sourceContainer.Name()[1:]}  // Reset to container name only
			endpoint.DNSNames = []string{sourceContainer.Name()[1:]} // Reset to container name only
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
			"Getting network config: Preserving network config for container %s on network %s: MAC=%s, IP=%s",
			sourceContainer.Name(),
			networkName,
			endpoint.MacAddress,
			endpoint.IPAddress,
		)
	}

	return config
}

// debugLogMacAddress logs MAC address information for a container's network configuration.
// It verifies that at least one MAC address exists for the container across its network
// endpoints, logging each found address at debug level.
// If no MAC addresses are found, then it emits a warning since containers typically require
// at least one MAC address for network communication.
// Multiple MAC addresses are supported as a container may be connected to multiple networks.
func debugLogMacAddress(
	networkConfig *dockerNetworkType.NetworkingConfig,
	containerID types.ContainerID,
	clientVersion string,
	minSupportedVersion string,
) {
	foundMac := false

	// Log any MAC addresses found in the config
	for networkName, endpoint := range networkConfig.EndpointsConfig {
		if endpoint.MacAddress != "" {
			logrus.Debugf(
				"Debugging MAC address: Found MAC address %s for container %s on network %s",
				endpoint.MacAddress,
				containerID,
				networkName,
			)

			foundMac = true
		}
	}

	// Determine logging behavior based on API version
	switch {
	case versions.LessThan(clientVersion, minSupportedVersion):
		switch foundMac {
		case true:
			logrus.Warnf(
				"Debugging MAC address: Unexpected MAC address in config for container %s with API version less than %s. This should not happen",
				containerID,
				minSupportedVersion,
			)
		case false:
			logrus.Debugf(
				"Debugging MAC address: No MAC address in config for container %s (expected for API versions less than %s. Docker will assign one)",
				containerID,
				minSupportedVersion,
			)
		}
	default: // API >= 1.44
		switch foundMac {
		case true:
			logrus.Debugf(
				"Debugging MAC address: MAC address configuration verified for container %s",
				containerID,
			)
		case false:
			logrus.Warnf(
				"Debugging MAC address: No MAC address found for container %s with API version greater than or equal to %s. This may indicate a configuration issue",
				containerID,
				minSupportedVersion,
			)
		}
	}
}
