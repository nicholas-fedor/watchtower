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
	ctx := context.Background()
	clog := logrus.WithFields(logrus.Fields{
		"include_stopped":    opts.IncludeStopped,
		"include_restarting": opts.IncludeRestarting,
	})

	clog.Debug("Retrieving container list")

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

	// Apply filters based on configured options.
	containers, err := api.ContainerList(ctx, dockerContainerType.ListOptions{Filters: filterArgs})
	if err != nil {
		clog.WithError(err).Debug("Failed to list containers")

		return nil, fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

	hostContainers := []types.Container{}

	for _, runningContainer := range containers {
		container, err := GetSourceContainer(api, types.ContainerID(runningContainer.ID))
		if err != nil {
			return nil, err // Logged in GetSourceContainer
		}

		if filter(container) {
			hostContainers = append(hostContainers, container)
		}
	}

	clog.WithField("count", len(hostContainers)).Debug("Filtered container list")

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
	clog := logrus.WithField("container_id", containerID)

	clog.Debug("Inspecting container")

	// Fetch basic container information.
	containerInfo, err := api.ContainerInspect(ctx, string(containerID))
	if err != nil {
		clog.WithError(err).Debug("Failed to inspect container")

		return nil, fmt.Errorf("%w: %w", errInspectContainerFailed, err)
	}

	// Resolve network container references.
	netType, netContainerID, found := strings.Cut(string(containerInfo.HostConfig.NetworkMode), ":")
	if found && netType == "container" {
		parentContainer, err := api.ContainerInspect(ctx, netContainerID)
		if err != nil {
			clog.WithError(err).WithFields(logrus.Fields{
				"container":         containerInfo.Name,
				"network_container": netContainerID,
			}).Warn("Unable to resolve network container")
		} else {
			containerInfo.HostConfig.NetworkMode = dockerContainerType.NetworkMode("container:" + parentContainer.Name)
			clog.WithFields(logrus.Fields{
				"container":         containerInfo.Name,
				"network_container": parentContainer.Name,
			}).Debug("Resolved network container name")
		}
	}

	// Fetch image info, tolerating failure.
	imageInfo, err := api.ImageInspect(ctx, containerInfo.Image)
	if err != nil {
		clog.WithError(err).Warn("Failed to retrieve image info")

		return NewContainer(&containerInfo, nil), nil
	}

	clog.WithField("image", containerInfo.Image).Debug("Retrieved container and image info")

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
	clog := logrus.WithFields(logrus.Fields{
		"container": sourceContainer.Name(),
		"id":        sourceContainer.ID().ShortID(),
	})

	// Stop the container if it’s running.
	signal := sourceContainer.StopSignal()
	if signal == "" {
		signal = defaultStopSignal
	}

	if sourceContainer.IsRunning() {
		clog.WithField("signal", signal).Info("Stopping container")

		if err := api.ContainerKill(ctx, string(sourceContainer.ID()), signal); err != nil {
			clog.WithError(err).Debug("Failed to stop container")

			return fmt.Errorf("%w: %w", errStopContainerFailed, err)
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
	clog := logrus.WithFields(logrus.Fields{
		"container": sourceContainer.Name(),
		"id":        sourceContainer.ID().ShortID(),
	})

	// Wait for the container to stop or timeout.
	stopped, err := waitForStopOrTimeout(api, sourceContainer, timeout)
	if err != nil {
		clog.WithError(err).Debug("Failed to wait for container stop")

		return err
	}

	if !stopped {
		clog.WithField("timeout", timeout).Warn("Container did not stop within timeout")
	}

	// If already gone and AutoRemove is enabled, no further action needed.
	if stopped && sourceContainer.ContainerInfo().HostConfig.AutoRemove {
		clog.Debug("Skipping removal due to AutoRemove")

		return nil
	}

	clog.Debug("Removing container")

	err = api.ContainerRemove(ctx, string(sourceContainer.ID()), dockerContainerType.RemoveOptions{
		Force:         true,
		RemoveVolumes: removeVolumes,
	})
	if err != nil && !dockerClient.IsErrNotFound(err) {
		clog.WithError(err).Debug("Failed to remove container")

		return fmt.Errorf("%w: %w", errRemoveContainerFailed, err)
	}

	if dockerClient.IsErrNotFound(err) {
		// Container was already gone after removal attempt; no need for second wait.
		return nil
	}

	// Confirm removal if it succeeded or container wasn’t gone before.
	stopped, err = waitForStopOrTimeout(api, sourceContainer, timeout)
	if err != nil {
		clog.WithError(err).Debug("Failed to confirm container removal")

		return err
	}

	if !stopped {
		clog.Debug("Container not removed within timeout")

		return fmt.Errorf(
			"%w: %s (%s)",
			errContainerNotRemoved,
			sourceContainer.Name(),
			sourceContainer.ID().ShortID(),
		)
	}

	clog.Debug("Confirmed container removal")

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

				logrus.WithError(err).WithFields(logrus.Fields{
					"container": container.Name(),
					"id":        container.ID().ShortID(),
				}).Debug("Failed to inspect container")

				return false, fmt.Errorf("%w: %w", errInspectContainerFailed, err)
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
	clog := logrus.WithFields(logrus.Fields{
		"container": sourceContainer.Name(),
		"version":   clientVersion,
	})

	networks := sourceContainer.ContainerInfo().NetworkSettings.Networks
	if networks == nil {
		clog.Warn("No network settings found")

		return &dockerNetworkType.NetworkingConfig{
			EndpointsConfig: make(map[string]*dockerNetworkType.EndpointSettings),
		}
	}

	config := &dockerNetworkType.NetworkingConfig{
		EndpointsConfig: make(map[string]*dockerNetworkType.EndpointSettings),
	}

	for networkName, endpoint := range networks {
		if endpoint == nil {
			clog.WithField("network", networkName).Warn("Nil endpoint in network settings")

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
			clog.WithFields(logrus.Fields{
				"network":     networkName,
				"mac_address": endpoint.MacAddress,
			}).Debug("Found MAC address in legacy config")
		}
	}

	// Warn if MAC preservation is desired but not possible
	if len(networks) > 0 {
		clog.Warn("MAC addresses not preserved in legacy config")
	}

	return config
}

// filterAliases removes the container’s short ID from the list of aliases.
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
	clog := logrus.WithField("container", sourceContainer.Name())

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
		clog.WithFields(logrus.Fields{
			"network":     networkName,
			"mac_address": endpoint.MacAddress,
			"ip_address":  endpoint.IPAddress,
		}).Debug("Preserved network config")
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
	clog := logrus.WithFields(logrus.Fields{
		"container":   containerID,
		"version":     clientVersion,
		"min_version": minSupportedVersion,
	})

	// Log any MAC addresses found in the config
	foundMac := false

	for networkName, endpoint := range networkConfig.EndpointsConfig {
		if endpoint.MacAddress != "" {
			clog.WithFields(logrus.Fields{
				"network":     networkName,
				"mac_address": endpoint.MacAddress,
			}).Debug("Found MAC address in config")

			foundMac = true
		}
	}

	// Determine logging behavior based on API version
	switch {
	case versions.LessThan(clientVersion, minSupportedVersion):
		if foundMac {
			clog.Warn("Unexpected MAC address in legacy config")
		} else {
			clog.Debug("No MAC address in legacy config, Docker will assign")
		}
	default: // API >= 1.44
		if foundMac {
			clog.Debug("Verified MAC address configuration")
		} else {
			clog.Warn("No MAC address found in config")
		}
	}
}
