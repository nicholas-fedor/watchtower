package container

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/docker/docker/api/types/container"
	dockerFilters "github.com/docker/docker/api/types/filters"
	dockerNetwork "github.com/docker/docker/api/types/network"
	dockerAPIVersion "github.com/docker/docker/api/types/versions"
	dockerClient "github.com/docker/docker/client"

	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// defaultStopSignal is the default signal for stopping containers ("SIGTERM").
const defaultStopSignal = "SIGTERM"

// ListSourceContainers retrieves a list of containers from the container runtime host.
//
// It filters containers based on options and a provided filter function.
//
// Parameters:
//   - api: Docker API client.
//   - opts: Client options for filtering.
//   - filter: Function to filter containers.
//   - isPodmanOptional: Optional variadic flag indicating Podman runtime (defaults to false).
//
// Returns:
//   - []types.Container: Filtered list of containers.
//   - error: Non-nil if listing fails, nil on success.
func ListSourceContainers(
	api dockerClient.APIClient,
	opts ClientOptions,
	filter types.Filter,
	isPodmanOptional ...bool,
) ([]types.Container, error) {
	ctx := context.Background()
	clog := logrus.WithFields(logrus.Fields{
		"include_stopped":    opts.IncludeStopped,
		"include_restarting": opts.IncludeRestarting,
	})

	clog.Debug("Retrieving container list")

	// Determine if the container runtime is Podman; default to false (Docker) if not specified.
	isPodman := false
	if len(isPodmanOptional) > 0 {
		isPodman = isPodmanOptional[0]
	}

	// Build filter arguments for container states.
	filterArgs := buildListFilterArgs(opts, isPodman)
	clog.WithFields(logrus.Fields{
		"custom_filter_provided": filter != nil,
		"filter_args":            filterArgs,
	}).Debug("Built filter arguments")

	// Fetch containers with status filters always applied based on ClientOptions.
	listOptions := dockerContainer.ListOptions{Filters: filterArgs}

	clog.Debug("API status filters applied based on ClientOptions")

	containers, err := api.ContainerList(ctx, listOptions)
	if err != nil {
		// Log detailed error information for debugging
		clog.WithFields(logrus.Fields{
			"error":        err,
			"error_type":   fmt.Sprintf("%T", err),
			"endpoint":     "/containers/json",
			"api_version":  strings.Trim(api.ClientVersion(), "\""),
			"docker_host":  os.Getenv("DOCKER_HOST"),
			"list_options": fmt.Sprintf("%+v", listOptions),
		}).Debug("ContainerList API call failed")

		// Handle 404 responses from Docker API
		if strings.Contains(err.Error(), "page not found") {
			clog.WithFields(logrus.Fields{
				"error":       err,
				"endpoint":    "/containers/json",
				"api_version": strings.Trim(api.ClientVersion(), "\""),
				"docker_host": os.Getenv("DOCKER_HOST"),
			}).Warn("Docker API returned 404 for container list; treating as empty list")

			return []types.Container{}, nil
		}

		clog.WithError(err).Debug("Failed to list containers")

		return nil, fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

	// Convert and filter containers.
	hostContainers := []types.Container{}

	for _, runningContainer := range containers {
		container, err := GetSourceContainer(api, types.ContainerID(runningContainer.ID))
		if err != nil {
			// Log detailed error information for debugging container inspect failures
			logrus.WithFields(logrus.Fields{
				"container_id": runningContainer.ID,
				"error":        err,
				"error_type":   fmt.Sprintf("%T", err),
				"api_version":  strings.Trim(api.ClientVersion(), "\""),
				"docker_host":  os.Getenv("DOCKER_HOST"),
			}).Debug("Failed to inspect individual container during list")

			// Handle race condition where containers disappear between API calls
			if cerrdefs.IsNotFound(err) {
				logrus.WithField("container_id", runningContainer.ID).
					Debug("Container no longer exists")

				continue
			}

			return nil, err // Logged in GetSourceContainer
		}

		if filter == nil || filter(container) {
			hostContainers = append(hostContainers, container)
		}
	}

	clog.WithField("count", len(hostContainers)).Debug("Filtered container list")

	return hostContainers, nil
}

// GetSourceContainer retrieves detailed information about a container by its ID.
//
// It resolves network mode if it references another container.
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
		// Log detailed error information for debugging
		clog.WithFields(logrus.Fields{
			"error":       err,
			"error_type":  fmt.Sprintf("%T", err),
			"api_version": strings.Trim(api.ClientVersion(), "\""),
			"docker_host": os.Getenv("DOCKER_HOST"),
		}).Debug("ContainerInspect API call failed")

		clog.WithError(err).Debug("Failed to inspect container")

		return nil, fmt.Errorf("%w: %w", errInspectContainerFailed, err)
	}

	// Resolve network mode if it references another container.
	netType, netContainerID, found := strings.Cut(string(containerInfo.HostConfig.NetworkMode), ":")
	if found && netType == "container" {
		parentContainer, err := api.ContainerInspect(ctx, netContainerID)
		if err != nil {
			clog.WithError(err).WithFields(logrus.Fields{
				"container":         util.NormalizeContainerName(containerInfo.Name),
				"network_container": netContainerID,
			}).Warn("Unable to resolve network container")
		} else {
			containerInfo.HostConfig.NetworkMode = dockerContainer.NetworkMode(
				"container:" + parentContainer.Name,
			)
			clog.WithFields(logrus.Fields{
				"container":         util.NormalizeContainerName(containerInfo.Name),
				"network_container": util.NormalizeContainerName(parentContainer.Name),
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

// StopSourceContainer stops the specified container using the Docker API's StopContainer method.
//
// It checks if the container is running, sends the configured stop signal (defaulting to SIGTERM if unset),
// and waits for the specified timeout for graceful shutdown, forcing termination with SIGKILL if necessary.
//
// Parameters:
//   - api: Docker API client for interacting with the Docker daemon.
//   - sourceContainer: Container object to stop, containing metadata like name and ID.
//   - timeout: Duration to wait for graceful shutdown before forcing termination with SIGKILL.
//
// Returns:
//   - error: Non-nil if stopping the container fails, nil on successful completion.
func StopSourceContainer(
	api dockerClient.APIClient,
	sourceContainer types.Container,
	timeout time.Duration,
) error {
	ctx := context.Background()
	clog := logrus.WithFields(logrus.Fields{
		"container": sourceContainer.Name(),
		"id":        sourceContainer.ID().ShortID(),
	})

	// Check if the container is running to determine if a stop operation is needed.
	if !sourceContainer.IsRunning() {
		// Log that the container is not running, so no stop attempt is required.
		clog.Debug("Container is not running, no stop operation needed")

		return nil
	}

	// Use container's configured timeout if available and valid, otherwise use passed parameter.
	// A timeout of 0 is valid in Docker (means no grace period - immediate SIGKILL after stop signal).
	effectiveTimeout := timeout
	if containerTimeout := sourceContainer.StopTimeout(); containerTimeout != nil &&
		*containerTimeout >= 0 {
		effectiveTimeout = time.Duration(*containerTimeout) * time.Second
		clog.WithFields(logrus.Fields{
			"container_timeout": effectiveTimeout,
			"default_timeout":   timeout,
		}).Debug("Using container-specific stop timeout")
	}

	// Retrieve the container's configured stop signal, falling back to SIGTERM if not specified.
	signal := sourceContainer.StopSignal()
	if signal == "" {
		// Use default SIGTERM signal if no custom signal is provided.
		signal = defaultStopSignal
	}

	// Log the stop attempt with the signal and configured timeout for clarity.
	message := "Stopping container"
	if sourceContainer.IsLinkedToRestarting() {
		message = "Stopping linked container"
	}

	clog.WithFields(logrus.Fields{
		"signal":  signal,
		"timeout": effectiveTimeout,
	}).Info(message)

	// Record the start time to measure elapsed duration for the stop operation.
	startTime := time.Now()

	// Convert timeout from time.Duration to seconds for Docker API's StopContainer.
	timeoutSeconds := int(effectiveTimeout / time.Second)

	// Call the Docker API's StopContainer with the stop signal and timeout in seconds.
	// The API sends the signal (SIGTERM or custom), waits for the timeout, and sends SIGKILL if needed.
	err := api.ContainerStop(ctx, string(sourceContainer.ID()), dockerContainer.StopOptions{
		Signal:  signal,
		Timeout: &timeoutSeconds,
	})
	if err != nil {
		// Log the failure with elapsed time and error details for debugging.
		clog.WithError(err).
			WithField("elapsed", time.Since(startTime)).
			Error("Failed to stop container")
		// Wrap the error with a specific Watchtower error type for consistent error handling.
		return fmt.Errorf("%w: %w", errStopContainerFailed, err)
	}

	// Log successful stop with elapsed time to confirm the operation's duration.
	clog.WithField("elapsed", time.Since(startTime)).Debug("Container stopped successfully")

	return nil
}

// StopAndRemoveSourceContainer stops and removes the specified container using the Docker API.
//
// It first stops the container if running, then removes it with optional volume cleanup.
//
// Parameters:
//   - api: Docker API client for interacting with the Docker daemon.
//   - sourceContainer: Container object to stop and remove, containing metadata like name and ID.
//   - timeout: Duration to wait for graceful shutdown before forcing termination with SIGKILL.
//   - removeVolumes: Boolean indicating whether to remove associated volumes during container removal.
//
// Returns:
//   - error: Non-nil if stopping or removing the container fails, nil on successful completion.
func StopAndRemoveSourceContainer(
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

	// Stop the container first
	err := StopSourceContainer(api, sourceContainer, timeout)
	if err != nil {
		return err
	}

	// Check if the container has AutoRemove enabled in its HostConfig, which automatically removes
	// the container after stopping, eliminating the need for an explicit removal call.
	if sourceContainer.ContainerInfo().HostConfig.AutoRemove {
		// Log that the container is skipped due to AutoRemove configuration.
		clog.Debug("Skipping container removal due to AutoRemove configuration")

		return nil
	}

	// Proceed with explicit container removal, including volumes if specified.
	// Log the start of the removal process for tracking.
	clog.Debug("Initiating container removal")

	// Record start time for the removal operation to track its duration.
	startTime := time.Now()

	// Call the Docker API's ContainerRemove to delete the container, forcing termination of any
	// lingering processes (via SIGKILL if needed) and removing volumes if specified.
	err = api.ContainerRemove(ctx, string(sourceContainer.ID()), dockerContainer.RemoveOptions{
		Force:         true,          // Ensure any lingering processes are terminated before removal.
		RemoveVolumes: removeVolumes, // Remove associated volumes if the parameter is true.
	})
	if err != nil && !cerrdefs.IsNotFound(err) {
		// Log removal failure with elapsed time and error details, excluding cases where the container
		// was already removed by another process.
		clog.WithError(err).
			WithField("elapsed", time.Since(startTime)).
			Error("Failed to remove container")
		// Wrap the error with a specific Watchtower error type for consistent error handling.
		return fmt.Errorf("%w: %w", errRemoveContainerFailed, err)
	}

	if cerrdefs.IsNotFound(err) {
		// Log that the container was already removed, likely by another process or AutoRemove.
		clog.WithField("elapsed", time.Since(startTime)).
			Debug("Container already removed by another process")

		return nil // Container already gone.
	}

	// Log successful removal with elapsed time to confirm the operation's duration.
	clog.WithField("elapsed", time.Since(startTime)).Debug("Container removed successfully")

	return nil
}

// buildListFilterArgs builds filter arguments for retrieving container states.
//
// It uses client options to determine which statuses to include.
//
// Parameters:
//   - opts: Client options for filtering.
//
// Returns:
//   - dockerFiltersType.Args: Arguments for filtering Docker containers
func buildListFilterArgs(opts ClientOptions, isPodman bool) dockerFilters.Args {
	filterArgs := dockerFilters.NewArgs()

	filterArgs.Add("status", "running")

	if opts.IncludeStopped {
		filterArgs.Add("status", "created")
		filterArgs.Add("status", "exited")
	}

	// Podman doesn't have the "restarting" status
	if opts.IncludeRestarting && !isPodman {
		filterArgs.Add("status", "restarting")
	}

	return filterArgs
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
) *dockerNetwork.NetworkingConfig {
	clog := logrus.WithFields(logrus.Fields{
		"container": sourceContainer.Name(),
		"id":        sourceContainer.ID().ShortID(),
		"version":   clientVersion,
	})

	// Initialize default network config
	config := newEmptyNetworkConfig()

	clog.Debug("Initialized empty network configuration")

	// Get network settings and mode from container info
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

	// Process each network endpoint
	for networkName, sourceEndpoint := range containerInfo.NetworkSettings.Networks {
		if sourceEndpoint == nil {
			clog.WithField("network", networkName).Debug("Skipping nil endpoint")

			continue
		}

		targetEndpoint, err := processEndpoint(
			sourceEndpoint,
			sourceContainer.ID(),
			clientVersion,
			isHostNetwork,
		)
		if err != nil {
			clog.WithError(err).
				WithField("network", networkName).
				Debug("Failed to process endpoint")

			continue
		}

		config.EndpointsConfig[networkName] = targetEndpoint

		clog.WithField("network", networkName).Debug("Added endpoint to network config")
	}

	// Validate MAC addresses, passing sourceContainer for state checking
	err := validateMacAddresses(
		config,
		sourceContainer.ID(),
		clientVersion,
		isHostNetwork,
		sourceContainer,
	)
	if err != nil {
		clog.WithError(err).Debug("MAC address validation issue")
	}

	return config
}

// newEmptyNetworkConfig creates an empty network configuration.
//
// Returns:
//   - *dockerNetworkType.NetworkingConfig: Empty configuration with initialized EndpointsConfig.
func newEmptyNetworkConfig() *dockerNetwork.NetworkingConfig {
	return &dockerNetwork.NetworkingConfig{
		EndpointsConfig: make(map[string]*dockerNetwork.EndpointSettings),
	}
}

// processEndpoint sanitizes a single network endpoint for the target container.
//
// It filters aliases, copies IPAM config, and handles MAC addresses based on API version and network mode. Returns an error if sourceEndpoint is nil.
//
// Parameters:
//   - sourceEndpoint: Source endpoint to process.
//   - containerID: ID of the source container.
//   - clientVersion: API version of the client.
//   - isHostNetwork: Whether the container uses host network mode.
//
// Returns:
//   - *dockerNetwork.EndpointSettings: Sanitized endpoint settings.
//   - error: Non-nil if sourceEndpoint is nil, nil otherwise.
func processEndpoint(
	sourceEndpoint *dockerNetwork.EndpointSettings,
	containerID types.ContainerID,
	clientVersion string,
	isHostNetwork bool,
) (*dockerNetwork.EndpointSettings, error) {
	if sourceEndpoint == nil {
		return nil, errNilSourceEndpoint
	}

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
		targetEndpoint.IPAMConfig = &dockerNetwork.EndpointIPAMConfig{
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

	// Handle aliases, MAC address, IP address, and DNS names based on API version and network mode.
	if dockerAPIVersion.LessThan(clientVersion, "1.44") || isHostNetwork {
		targetEndpoint.MacAddress = ""
		targetEndpoint.IPAddress = ""
		targetEndpoint.DNSNames = nil

		if isHostNetwork {
			clog.Debug("Cleared MAC address, IP address, and DNS names for host network mode")
		} else {
			clog.Debug("Cleared MAC address, IP address, and DNS names for legacy API")
		}
	}

	return targetEndpoint, nil
}

// validateMacAddresses verifies the presence of MAC addresses in a container's network configuration
// and logs appropriate messages based on the container's state, network mode, and Docker API version.
// It ensures that MAC addresses are correctly handled for modern API versions (>= 1.44) and logs
// warnings for potential issues in running containers while using debug logs for non-critical cases,
// such as non-running containers (e.g., created or exited states), to reduce user-facing noise.
//
// Parameters:
//   - config: The network configuration to validate, containing endpoint settings for each network.
//   - containerID: The unique identifier of the container, used for logging context.
//   - clientVersion: The Docker API version in use (e.g., "1.49"), determining MAC address handling rules.
//   - isHostNetwork: Indicates whether the container uses host network mode, where MAC addresses are not expected.
//   - sourceContainer: The container object, providing access to state (running, created, exited) and metadata.
//
// Returns:
//   - error: Returns a non-nil error (e.g., errNoMacInNonHost, errUnexpectedMacInHost) if validation
//     detects a critical issue requiring attention, such as unexpected MAC addresses in legacy APIs
//     or host mode, or missing MAC addresses in running containers with modern APIs. Returns nil
//     for non-critical cases, such as non-running containers or expected absence of MAC addresses.
func validateMacAddresses(
	config *dockerNetwork.NetworkingConfig,
	containerID types.ContainerID,
	clientVersion string,
	isHostNetwork bool,
	sourceContainer types.Container,
) error {
	// Initialize logger with container and API version context for consistent log messages.
	clog := logrus.WithFields(logrus.Fields{
		"container": containerID.ShortID(),
		"version":   clientVersion,
	})

	// Handle nil network configuration by checking if the container is running and not in host network mode.
	if config == nil {
		clog.Debug("Nil network configuration provided")
		// Check if the container is running and not in host network mode.
		isRunning := sourceContainer.IsRunning()
		// If the container is running and not in host network mode, and the API version is >= 1.44, return an error.
		if !dockerAPIVersion.LessThan(clientVersion, "1.44") && !isHostNetwork && isRunning {
			// Log a warning for running containers with missing network configuration in modern APIs.
			return errNoMacInNonHost
		}

		// For non-running containers or host network mode, return nil as no validation is needed.
		return nil
	}

	// Scan network endpoints to check for MAC address presence.
	// A MAC address is expected for running containers in non-host networks with modern API versions.
	foundMac := false

	for networkName, endpoint := range config.EndpointsConfig {
		if endpoint.MacAddress != "" {
			foundMac = true
			// Log the found MAC address at debug level for diagnostic purposes.
			clog.WithFields(logrus.Fields{
				"network":     networkName,
				"mac_address": endpoint.MacAddress,
			}).Debug("MAC address found in network configuration")
		}
	}

	// Retrieve container state to determine if it’s running, which affects MAC address expectations.
	// Non-running containers (e.g., created, exited) typically lack MAC addresses due to inactive network interfaces.
	containerInfo := sourceContainer.ContainerInfo()
	isRunning := sourceContainer.IsRunning()

	// Extract the container’s state (e.g., "running", "created", "exited") for logging context.
	// Use "unknown" as a fallback if container metadata is incomplete to ensure safe logging.
	containerState := "unknown"
	if containerInfo != nil && containerInfo.State != nil {
		containerState = containerInfo.State.Status
	}

	// Handle legacy Docker API versions (< 1.44), where MAC address preservation is not fully supported.
	// In legacy APIs, MAC addresses should not appear in non-host networks, as they are managed differently.
	if dockerAPIVersion.LessThan(clientVersion, "1.44") {
		if foundMac && !isHostNetwork {
			// Unexpected MAC address in a legacy API is a potential misconfiguration; log a warning and return an error.
			clog.Warn("Unexpected MAC address in legacy config")

			return fmt.Errorf("%w: API version %s", errUnexpectedMacInLegacy, clientVersion)
		}
		// No MAC address in legacy config is expected; log at debug level and return no error.
		clog.Debug("No MAC address in legacy API configuration (expected)")

		return nil
	}

	// Handle host network mode, where the container uses the host’s network stack and should not have its own MAC addresses.
	if isHostNetwork {
		if foundMac {
			// MAC addresses in host mode are unexpected and indicate a misconfiguration; log a warning and return an error.
			clog.Warn("Unexpected MAC address in host network config")

			return errUnexpectedMacInHost
		}
		// No MAC address in host mode is correct; log at debug level and return no error.
		clog.Debug("No MAC address in host network mode (expected)")

		return nil
	}

	// Handle non-host network mode (e.g., bridge, overlay) for modern API versions (>= 1.44).
	// MAC addresses are expected for running containers but not for non-running ones.
	if !foundMac {
		if !isRunning {
			// Non-running containers (e.g., created, exited) typically lack MAC addresses
			// because their network interfaces are inactive.
			clog.WithField("state", containerState).
				Debug("No MAC address for non-running container (expected)")

			return nil
		}
		// Running containers should have MAC addresses, but absence may indicate
		// either a lack of support or a configuration issue.
		clog.WithField("state", containerState).
			Debug("No MAC address found in non-host network config")

		return errNoMacInNonHost
	}

	// MAC address found in a running container with a modern API; this is the expected case.
	// Log at debug level to confirm successful validation.
	clog.Debug("MAC address validation passed")

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
	networkConfig *dockerNetwork.NetworkingConfig,
	containerID types.ContainerID,
	clientVersion string,
	minSupportedVersion string,
	isHostNetwork bool,
) {
	clog := logrus.WithFields(logrus.Fields{
		"container":   containerID.ShortID(),
		"version":     clientVersion,
		"min_version": minSupportedVersion,
	})

	// Check for MAC addresses in the config.
	foundMac := false

	// Iterate through network endpoints to find MAC addresses.
	if networkConfig != nil {
		// Iterate through network endpoints to find MAC addresses.
		for networkName, endpoint := range networkConfig.EndpointsConfig {
			// Check if the endpoint has a MAC address.
			if endpoint.MacAddress != "" {
				clog.WithFields(logrus.Fields{
					"network":     networkName,
					"mac_address": endpoint.MacAddress,
				}).Debug("Found MAC address in config")

				// Set flag to indicate MAC address was found.
				foundMac = true
			}
		}
	}

	// Log based on API version, MAC presence, and network mode.
	switch {
	// API < v1.44, MAC present
	case dockerAPIVersion.LessThan(clientVersion, minSupportedVersion):
		if foundMac {
			clog.Debug("Unexpected MAC address in legacy config")

			return
		}

		clog.Debug("No MAC address in legacy config, Docker will handle")
	// API < v1.44, MAC present
	case dockerAPIVersion.LessThan(clientVersion, "1.44") && !isHostNetwork:
		if foundMac {
			clog.Debug("Unexpected MAC address in legacy config")

			return
		}

		clog.Debug("No MAC address in legacy config, as expected")
	// API < v1.44, MAC present
	case foundMac:
		clog.Debug("Verified MAC address configuration")
	// API >= v1.44, no MAC, non-host network
	case !isHostNetwork:
		clog.Debug("No MAC address found in config")
	// API >= v1.44, no MAC, host network
	default:
		clog.Debug("No MAC address in host network mode, as expected")
	}
}

// IsWatchtowerParent checks if the current container ID exists in the comma-separated container-chain label values.
//
// It handles edge cases like empty chain or invalid IDs by returning false appropriately.
// The chain values are trimmed of whitespace before comparison.
//
// Parameters:
//   - currentID: The container ID to check for in the chain.
//   - chain: Comma-separated string of container IDs from the container-chain label.
//
// Returns:
//   - bool: True if currentID is found in the chain, false otherwise.
func IsWatchtowerParent(currentID types.ContainerID, chain string) bool {
	if currentID == "" || chain == "" {
		return false
	}

	ids := strings.SplitSeq(chain, ",")
	for id := range ids {
		if strings.TrimSpace(id) == string(currentID) {
			return true
		}
	}

	return false
}
