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

	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// defaultStopSignal is the default signal for stopping containers ("SIGTERM").
const defaultStopSignal = "SIGTERM"

// ListSourceContainers retrieves a list of containers from the Docker host.
//
// It filters containers based on options and a provided filter function.
//
// Parameters:
//   - api: Docker API client.
//   - opts: Client options for filtering.
//   - filter: Function to filter containers.
//
// Returns:
//   - []types.Container: Filtered list of containers.
//   - error: Non-nil if listing fails, nil on success.
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

	// Build filter arguments for container states.
	filterArgs := dockerFiltersType.NewArgs()
	filterArgs.Add("status", "running")

	if opts.IncludeStopped {
		filterArgs.Add("status", "created")
		filterArgs.Add("status", "exited")
	}

	if opts.IncludeRestarting {
		filterArgs.Add("status", "restarting")
	}

	// Fetch containers with applied filters.
	containers, err := api.ContainerList(ctx, dockerContainerType.ListOptions{Filters: filterArgs})
	if err != nil {
		clog.WithError(err).Debug("Failed to list containers")

		return nil, fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

	// Convert and filter containers.
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
//
// It resolves network container references where possible.
//
// Parameters:
//   - api: Docker API client.
//   - containerID: ID of the container to inspect.
//
// Returns:
//   - types.Container: Container object if successful.
//   - error: Non-nil if inspection fails, nil on success.
func GetSourceContainer(
	api dockerClient.APIClient,
	containerID types.ContainerID,
) (types.Container, error) {
	ctx := context.Background()
	clog := logrus.WithField("container_id", containerID)

	clog.Debug("Inspecting container")

	// Inspect the container to get its details.
	containerInfo, err := api.ContainerInspect(ctx, string(containerID))
	if err != nil {
		clog.WithError(err).Debug("Failed to inspect container")

		return nil, fmt.Errorf("%w: %w", errInspectContainerFailed, err)
	}

	// Resolve network mode if it references another container.
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

	// Fetch image info, falling back if it fails.
	imageInfo, err := api.ImageInspect(ctx, containerInfo.Image)
	if err != nil {
		clog.WithError(err).Warn("Failed to retrieve image info")

		return NewContainer(&containerInfo, nil), nil
	}

	clog.WithField("image", containerInfo.Image).Debug("Retrieved container and image info")

	return NewContainer(&containerInfo, &imageInfo), nil
}

// StopSourceContainer stops and removes the specified container.
//
// Parameters:
//   - api: Docker API client.
//   - sourceContainer: Container to stop and remove.
//   - timeout: Duration to wait before forcing stop.
//   - removeVolumes: Whether to remove associated volumes.
//
// Returns:
//   - error: Non-nil if stop/removal fails, nil on success.
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

	// Determine stop signal, defaulting to SIGTERM.
	signal := sourceContainer.StopSignal()
	if signal == "" {
		signal = defaultStopSignal
	}

	// Stop the container if it’s running.
	if sourceContainer.IsRunning() {
		// Log detailed stop message
		clog.WithField("signal", signal).Info("Stopping container")

		if err := api.ContainerKill(ctx, string(sourceContainer.ID()), signal); err != nil {
			clog.WithError(err).Debug("Failed to stop container")

			return fmt.Errorf("%w: %w", errStopContainerFailed, err)
		}
	}

	// Proceed with removal process.
	return stopAndRemoveContainer(api, sourceContainer, timeout, removeVolumes)
}

// stopAndRemoveContainer waits for a container to stop and removes it if needed.
//
// Parameters:
//   - api: Docker API client.
//   - sourceContainer: Container to process.
//   - timeout: Duration to wait for stop/removal.
//   - removeVolumes: Whether to remove volumes.
//
// Returns:
//   - error: Non-nil if process fails, nil on success.
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

	// Wait for the container to stop.
	stopped, err := waitForStopOrTimeout(api, sourceContainer, timeout)
	if err != nil {
		clog.WithError(err).Debug("Failed to wait for container stop")

		return err
	}

	if !stopped {
		clog.WithField("timeout", timeout).Warn("Container did not stop within timeout")
	}

	// Skip removal if AutoRemove is enabled and container stopped.
	if stopped && sourceContainer.ContainerInfo().HostConfig.AutoRemove {
		clog.Debug("Skipping removal due to AutoRemove")

		return nil
	}

	// Remove the container with force and volume options.
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
		return nil // Container already gone.
	}

	// Confirm removal completed.
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
//
// Parameters:
//   - api: Docker API client.
//   - container: Container to monitor.
//   - waitTime: Duration to wait.
//
// Returns:
//   - bool: True if stopped or gone, false if still running.
//   - error: Non-nil if inspection fails, nil otherwise.
func waitForStopOrTimeout(
	api dockerClient.APIClient,
	container types.Container,
	waitTime time.Duration,
) (bool, error) {
	ctx := context.Background()
	timeout := time.After(waitTime)

	// Poll container state until stopped or timeout.
	for {
		select {
		case <-timeout:
			return false, nil // Timeout reached, still running.
		default:
			containerInfo, err := api.ContainerInspect(ctx, string(container.ID()))
			if err != nil {
				if dockerClient.IsErrNotFound(err) {
					return true, nil // Container gone, treat as stopped.
				}

				logrus.WithError(err).WithFields(logrus.Fields{
					"container": container.Name(),
					"id":        container.ID().ShortID(),
				}).Debug("Failed to inspect container")

				return false, fmt.Errorf("%w: %w", errInspectContainerFailed, err)
			}

			if !containerInfo.State.Running {
				return true, nil // Container stopped.
			}
		}
		time.Sleep(1 * time.Second)
	}
}

// getNetworkConfig extracts and sanitizes the network configuration from a container.
//
// It handles all network modes, including host, and supports both legacy and modern API versions.
//
// Parameters:
//   - sourceContainer: Container to extract config from.
//   - clientVersion: API version of the client.
//
// Returns:
//   - *dockerNetworkType.NetworkingConfig: Sanitized network configuration.
func getNetworkConfig(
	sourceContainer types.Container,
	clientVersion string,
) *dockerNetworkType.NetworkingConfig {
	clog := logrus.WithFields(logrus.Fields{
		"container": sourceContainer.Name(),
		"id":        sourceContainer.ID().ShortID(),
		"version":   clientVersion,
	})

	// Initialize default network config.
	config := newEmptyNetworkConfig()

	clog.Debug("Initialized empty network configuration")

	// Get network settings and mode from container info.
	containerInfo := sourceContainer.ContainerInfo()
	if containerInfo == nil || containerInfo.NetworkSettings == nil {
		clog.Warn("No network settings available")

		return config
	}

	networkMode := containerInfo.HostConfig.NetworkMode
	isHostNetwork := string(networkMode) == "host"
	clog.WithFields(logrus.Fields{
		"network_mode": networkMode,
		"is_host":      isHostNetwork,
	}).Debug("Evaluated network mode")

	// Process each network endpoint, including host mode.
	for networkName, sourceEndpoint := range containerInfo.NetworkSettings.Networks {
		if sourceEndpoint == nil {
			clog.WithField("network", networkName).Warn("Skipping nil endpoint")

			continue
		}

		targetEndpoint := processEndpoint(
			sourceEndpoint,
			sourceContainer.ID(),
			clientVersion,
			isHostNetwork,
		)
		config.EndpointsConfig[networkName] = targetEndpoint

		clog.WithField("network", networkName).Debug("Added endpoint to network config")
	}

	// Validate and log MAC address presence.
	if err := validateMacAddresses(config, sourceContainer.ID(), clientVersion, isHostNetwork); err != nil {
		clog.WithError(err).Warn("MAC address validation failed")
		// Log warning but don’t fail, as MAC issues are non-critical.
	}

	return config
}

// newEmptyNetworkConfig creates an empty network configuration.
//
// Returns:
//   - *dockerNetworkType.NetworkingConfig: Empty configuration with initialized EndpointsConfig.
func newEmptyNetworkConfig() *dockerNetworkType.NetworkingConfig {
	return &dockerNetworkType.NetworkingConfig{
		EndpointsConfig: make(map[string]*dockerNetworkType.EndpointSettings),
	}
}

// processEndpoint sanitizes a single network endpoint for the target container.
//
// It filters aliases, copies IPAM config, and handles MAC addresses based on API version and network mode.
//
// Parameters:
//   - sourceEndpoint: Source endpoint to process.
//   - containerID: ID of the source container.
//   - clientVersion: API version of the client.
//   - isHostNetwork: Whether the container uses host network mode.
//
// Returns:
//   - *dockerNetworkType.EndpointSettings: Sanitized endpoint settings.
func processEndpoint(
	sourceEndpoint *dockerNetworkType.EndpointSettings,
	containerID types.ContainerID,
	clientVersion string,
	isHostNetwork bool,
) *dockerNetworkType.EndpointSettings {
	clog := logrus.WithFields(logrus.Fields{
		"container": containerID.ShortID(),
		"version":   clientVersion,
	})

	// Copy endpoint to preserve all fields.
	targetEndpoint := sourceEndpoint.Copy()

	clog.Debug("Copied endpoint settings")

	// Handle aliases: clear for host mode, filter for others.
	if isHostNetwork {
		targetEndpoint.Aliases = nil

		clog.Debug("Cleared aliases for host network mode")
	} else if len(targetEndpoint.Aliases) > 0 {
		targetEndpoint.Aliases = filterAliases(targetEndpoint.Aliases, containerID.ShortID())
		clog.WithFields(logrus.Fields{
			"source_aliases": sourceEndpoint.Aliases,
			"target_aliases": targetEndpoint.Aliases,
		}).Debug("Filtered aliases")
	}

	// Copy IPAM config if present and not in host mode.
	if sourceEndpoint.IPAMConfig != nil && !isHostNetwork {
		targetEndpoint.IPAMConfig = &dockerNetworkType.EndpointIPAMConfig{
			IPv4Address:  sourceEndpoint.IPAMConfig.IPv4Address,
			IPv6Address:  sourceEndpoint.IPAMConfig.IPv6Address,
			LinkLocalIPs: sourceEndpoint.IPAMConfig.LinkLocalIPs,
		}

		clog.Debug("Copied IPAM configuration")
	} else {
		targetEndpoint.IPAMConfig = nil

		if isHostNetwork {
			clog.Debug("Cleared IPAM config for host network mode")
		}
	}

	// Handle MAC address, IP address, and DNS names based on API version and network mode.
	if versions.LessThan(clientVersion, flags.DockerAPIMinVersion) || isHostNetwork {
		targetEndpoint.MacAddress = ""
		targetEndpoint.IPAddress = ""
		targetEndpoint.DNSNames = nil

		if isHostNetwork {
			clog.Debug("Cleared MAC address, IP address, and DNS names for host network mode")
		} else {
			clog.Debug("Cleared MAC address and DNS names for legacy API")
		}
	}

	return targetEndpoint
}

// validateMacAddresses checks MAC address presence and logs appropriate messages.
//
// Parameters:
//   - config: Network configuration to validate.
//   - containerID: ID of the container.
//   - clientVersion: API version of the client.
//   - isHostNetwork: Whether the container uses host network mode.
//
// Returns:
//   - error: Non-nil if validation detects an issue requiring attention.
func validateMacAddresses(
	config *dockerNetworkType.NetworkingConfig,
	containerID types.ContainerID,
	clientVersion string,
	isHostNetwork bool,
) error {
	clog := logrus.WithFields(logrus.Fields{
		"container": containerID.ShortID(),
		"version":   clientVersion,
	})

	foundMac := false

	for networkName, endpoint := range config.EndpointsConfig {
		if endpoint.MacAddress != "" {
			foundMac = true

			clog.WithFields(logrus.Fields{
				"network":     networkName,
				"mac_address": endpoint.MacAddress,
			}).Debug("Found MAC address in config")
		}
	}

	switch {
	case versions.LessThan(clientVersion, flags.DockerAPIMinVersion):
		if foundMac {
			return fmt.Errorf("%w: API version %s", errUnexpectedMacInLegacy, clientVersion)
		}

		clog.Debug("No MAC address in legacy config, as expected")
	case foundMac && isHostNetwork:
		return errUnexpectedMacInHost
	case foundMac:
		clog.Debug("Verified MAC address presence")
	case !isHostNetwork:
		return errNoMacInNonHost
	default:
		clog.Debug("No MAC address in host network mode, as expected")
	}

	return nil
}

// filterAliases removes the container’s short ID from the list of aliases.
//
// Parameters:
//   - aliases: List of aliases to filter.
//   - shortID: Short ID to remove.
//
// Returns:
//   - []string: Filtered list of aliases.
func filterAliases(aliases []string, shortID string) []string {
	result := make([]string, 0, len(aliases))

	for _, alias := range aliases {
		if alias != shortID {
			result = append(result, alias)
		}
	}

	return result
}

// debugLogMacAddress logs MAC address info for a container’s network config.
//
// Parameters:
//   - networkConfig: Network configuration to check.
//   - containerID: ID of the container.
//   - clientVersion: API version of the client.
//   - minSupportedVersion: Minimum API version for MAC preservation.
//   - isHostNetwork: Whether the container uses host network mode.
func debugLogMacAddress(
	networkConfig *dockerNetworkType.NetworkingConfig,
	containerID types.ContainerID,
	clientVersion string,
	minSupportedVersion string,
	isHostNetwork bool,
) {
	clog := logrus.WithFields(logrus.Fields{
		"container":   containerID,
		"version":     clientVersion,
		"min_version": minSupportedVersion,
	})

	// Check for MAC addresses in the config.
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

	// Log based on API version, MAC presence, and network mode.
	switch {
	case versions.LessThan(clientVersion, minSupportedVersion): // API < v1.44
		if foundMac {
			clog.Warn("Unexpected MAC address in legacy config")

			return
		}

		clog.Debug("No MAC address in legacy config, Docker will handle")
	case foundMac: // API >= v1.44, MAC present
		clog.Debug("Verified MAC address configuration")
	case !isHostNetwork: // API >= v1.44, no MAC, non-host network
		clog.Warn("No MAC address found in config")
	default: // API >= v1.44, no MAC, host network
		clog.Debug("No MAC address in host network mode, as expected")
	}
}
