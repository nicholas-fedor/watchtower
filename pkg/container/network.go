package container

import (
	"errors"
	"fmt"
	"net/netip"

	"github.com/rs/zerolog"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerNetwork "github.com/moby/moby/api/types/network"
	dockerAPIVersion "github.com/moby/moby/client/pkg/versions"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for network configuration operations.
var (
	// errNoMacInNonHost indicates no MAC address was found in a non-host network configuration where one is expected.
	errNoMacInNonHost = errors.New("no MAC address found in non-host network config")
	// errNilSourceEndpoint indicates a nil source endpoint was provided.
	errNilSourceEndpoint = errors.New("nil source endpoint provided")
)

// getNetworkConfig extracts and sanitizes the network configuration from a container.
//
// It handles all network modes, including host, and supports both legacy and modern API versions.
//
// Parameters:
//   - sourceContainer: Container to extract config from.
//   - clientVersion: API version of the client.
//
// Returns:
//   - *dockerNetwork.NetworkingConfig: Sanitized network configuration.
func getNetworkConfig(log *zerolog.Logger,
	sourceContainer types.Container,
	clientVersion string,
) *dockerNetwork.NetworkingConfig {
	clogVal := log.With().
		Str("container", sourceContainer.Name()).
		Str("id", sourceContainer.ID().ShortID()).
		Str("version", clientVersion).
		Logger()
	clog := &clogVal

	// Initialize default network config
	config := newEmptyNetworkConfig()

	clog.Debug().Msg("Initialized empty network configuration")

	// Get network settings and mode from container info
	containerInfo := sourceContainer.ContainerInfo()
	if containerInfo == nil || containerInfo.NetworkSettings == nil {
		clog.Warn().Msg("No network settings available")

		return config
	}

	var networkMode dockerContainer.NetworkMode
	if containerInfo.HostConfig != nil {
		networkMode = containerInfo.HostConfig.NetworkMode
	}

	isHostNetwork := string(networkMode) == "host"
	clog.Debug().
		Str("network_mode", string(networkMode)).
		Bool("is_host", isHostNetwork).
		Msg("Evaluated network mode")

	// Process each network endpoint
	for networkName, sourceEndpoint := range containerInfo.NetworkSettings.Networks {
		if sourceEndpoint == nil {
			clog.Debug().
				Str("network", networkName).
				Msg("Skipping nil endpoint")

			continue
		}

		targetEndpoint, err := processEndpoint(log,
			sourceEndpoint,
			sourceContainer.ID(),
			clientVersion,
			isHostNetwork,
		)
		if err != nil {
			clog.Debug().
				Err(err).
				Str("network", networkName).
				Msg("Failed to process endpoint")

			continue
		}

		config.EndpointsConfig[networkName] = targetEndpoint

		clog.Debug().
			Str("network", networkName).
			Msg("Added endpoint to network config")
	}

	// Validate MAC addresses, passing sourceContainer for state checking
	err := validateMacAddresses(log,
		config,
		sourceContainer.ID(),
		clientVersion,
		isHostNetwork,
		sourceContainer,
	)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("MAC address validation issue")
	}

	return config
}

// newEmptyNetworkConfig creates an empty network configuration.
//
// Returns:
//   - *dockerNetwork.NetworkingConfig: Empty configuration with initialized EndpointsConfig.
func newEmptyNetworkConfig() *dockerNetwork.NetworkingConfig {
	return &dockerNetwork.NetworkingConfig{
		EndpointsConfig: make(map[string]*dockerNetwork.EndpointSettings),
	}
}

// processEndpoint sanitizes a single network endpoint for the target container.
//
// It filters aliases, copies IPAM config, and handles MAC addresses based on API
// version and network mode. Returns an error if sourceEndpoint is nil.
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
func processEndpoint(log *zerolog.Logger,
	sourceEndpoint *dockerNetwork.EndpointSettings,
	containerID types.ContainerID,
	clientVersion string,
	isHostNetwork bool,
) (*dockerNetwork.EndpointSettings, error) {
	if sourceEndpoint == nil {
		return nil, errNilSourceEndpoint
	}

	clogVal := log.With().
		Str("container", containerID.ShortID()).
		Str("version", clientVersion).
		Logger()
	clog := &clogVal

	// Copy endpoint to preserve all fields.
	targetEndpoint := sourceEndpoint.Copy()

	clog.Debug().Msg("Copied endpoint settings")

	// Handle aliases: clear for host mode, filter for others.
	if isHostNetwork {
		targetEndpoint.Aliases = nil

		clog.Debug().Msg("Cleared aliases for host network mode")
	} else if len(targetEndpoint.Aliases) > 0 {
		targetEndpoint.Aliases = filterAliases(targetEndpoint.Aliases, containerID.ShortID())
		clog.Debug().
			Strs("source_aliases", sourceEndpoint.Aliases).
			Strs("target_aliases", targetEndpoint.Aliases).
			Msg("Filtered aliases")
	}

	// Copy IPAM config if present and not in host mode.
	if sourceEndpoint.IPAMConfig != nil && !isHostNetwork {
		targetEndpoint.IPAMConfig = &dockerNetwork.EndpointIPAMConfig{
			IPv4Address:  sourceEndpoint.IPAMConfig.IPv4Address,
			IPv6Address:  sourceEndpoint.IPAMConfig.IPv6Address,
			LinkLocalIPs: sourceEndpoint.IPAMConfig.LinkLocalIPs,
		}

		clog.Debug().Msg("Copied IPAM configuration")
	} else {
		targetEndpoint.IPAMConfig = nil

		if isHostNetwork {
			clog.Debug().Msg("Cleared IPAM config for host network mode")
		}
	}

	// Handle aliases, MAC address, IP address, and DNS names based on API version and network mode.
	if dockerAPIVersion.LessThan(clientVersion, "1.44") || isHostNetwork {
		targetEndpoint.MacAddress = dockerNetwork.HardwareAddr{}
		targetEndpoint.IPAddress = netip.Addr{}
		targetEndpoint.DNSNames = nil

		if isHostNetwork {
			clog.Debug().Msg("Cleared MAC address, IP address, and DNS names for host network mode")
		} else {
			clog.Debug().Msg("Cleared MAC address, IP address, and DNS names for legacy API")
		}
	} else if isEngineGeneratedMAC(targetEndpoint.MacAddress, sourceEndpoint.IPAddress) {
		// Clear MACs derived from the endpoint's IPv4 address.
		//
		// Docker 28.0.0+ bridge networks assign random MACs and will not match
		// the engine-generated pattern, but older Docker versions and overlay
		// networks still produce IP-derived MACs that become stale after IP
		// reassignment.
		targetEndpoint.MacAddress = dockerNetwork.HardwareAddr{}

		clog.Debug().Msg("Cleared engine-generated MAC address. Docker will regenerate for new IP")
	}

	return targetEndpoint, nil
}

// isEngineGeneratedMAC reports whether mac matches the bridge driver's engine-generated
// pattern derived from the endpoint's current IPv4 address.
//
// Docker inspect unmarshals MacAddress into a 6-byte HardwareAddr.
// Compare the canonical colon-separated form, not the raw byte length.
// Docker < 28.0.0 bridge networks assigned MACs of the form "02:42:aa:bb:cc:dd",
// where the last four bytes are the IPv4 octets.
// Docker 28.0.0+ switched the bridge driver to fully random MACs, but overlay
// networks still use IP-derived MACs, so this check remains relevant for those
// drivers and for older Docker versions.
// A MAC matching that pattern for the given IP is not a user-configured value
// and should not be preserved across container recreation.
// If ip is not a valid IPv4 address, the MAC is assumed to be user-configured
// and false is returned.
//
// Parameters:
//   - mac: MAC address to check.
//   - ip: Endpoint's current IPv4 address.
//
// Returns:
//   - bool: True if the MAC was engine-generated from ip, false otherwise.
func isEngineGeneratedMAC(mac dockerNetwork.HardwareAddr, ip netip.Addr) bool {
	if !ip.Is4() {
		return false
	}

	ipv4 := ip.As4()
	expected := fmt.Sprintf("02:42:%02x:%02x:%02x:%02x", ipv4[0], ipv4[1], ipv4[2], ipv4[3])

	return mac.String() == expected
}

// onlyGeneratedMacs reports whether every non-empty MAC in the source
// container's original network settings matches the bridge driver's
// engine-generated pattern for that endpoint's IP, and whether every non-nil
// original endpoint is present in the processed configuration.
//
// It is used by validateMacAddresses to allow missing MACs in the processed
// config only when those MACs were intentionally cleared by processEndpoint and
// no endpoints were lost during processing.
//
// Parameters:
//   - sourceContainer: The container whose original network settings are inspected.
//   - processedConfig: The processed network configuration whose endpoint coverage is checked.
//
// Returns:
//   - bool: True if the container had at least one MAC and every one of them was
//     engine-generated, and all non-nil original endpoints appear in the processed
//     config. False otherwise.
func onlyGeneratedMacs(sourceContainer types.Container, processedConfig *dockerNetwork.NetworkingConfig) bool {
	if sourceContainer == nil {
		return false
	}

	containerInfo := sourceContainer.ContainerInfo()

	if containerInfo == nil || containerInfo.NetworkSettings == nil {
		return false
	}

	// Build a set of network names present in the processed config so we can
	// verify that every non-nil original endpoint survived processing.
	processedNetworks := make(map[string]bool, len(processedConfig.EndpointsConfig))

	for name := range processedConfig.EndpointsConfig {
		processedNetworks[name] = true
	}

	hasMac := false

	for networkName, endpoint := range containerInfo.NetworkSettings.Networks {
		// Nil endpoints carry no MAC and do not participate in coverage checks.
		if endpoint == nil {
			continue
		}

		// A non-nil original endpoint that was dropped during processing means
		// the missing-MAC result may not be fully explained by intentional
		// clearing, so do not grant the waiver.
		if !processedNetworks[networkName] {
			return false
		}

		// Endpoints without a MAC cannot be engine-generated, but they also do
		// not disprove the hypothesis that every original MAC was generated.
		if len(endpoint.MacAddress) == 0 {
			continue
		}

		hasMac = true

		// Any user-configured MAC disproves the all-generated hypothesis and
		// preserves normal missing-MAC validation behavior.
		if !isEngineGeneratedMAC(endpoint.MacAddress, endpoint.IPAddress) {
			return false
		}
	}

	// Require at least one original MAC so that containers with no MACs at all
	// do not incorrectly bypass validation.
	return hasMac
}

// validateMacAddresses verifies the presence of MAC addresses in a container's processed network
// configuration and logs the result at debug level based on the container's state, network mode,
// and Docker API version. Only non-host networks on modern API versions (>= 1.44) are checked,
// since processEndpoint clears MAC addresses for host networks and legacy API versions.
//
// Parameters:
//   - config: The network configuration to validate, containing endpoint settings for each network.
//   - containerID: The unique identifier of the container, used for logging context.
//   - clientVersion: The Docker API version in use (e.g., "1.49"), determining MAC address handling rules.
//   - isHostNetwork: Indicates whether the container uses host network mode, where MAC addresses are not expected.
//   - sourceContainer: The container object, providing access to state (running, created, exited) and metadata.
//
// Returns:
//   - error: errNoMacInNonHost when a running container on a modern API version has no MAC address
//     in a non-host network that the engine did not generate. Nil for non-running containers, host
//     networks, legacy API versions, and other expected absences of MAC addresses.
func validateMacAddresses(log *zerolog.Logger,
	config *dockerNetwork.NetworkingConfig,
	containerID types.ContainerID,
	clientVersion string,
	isHostNetwork bool,
	sourceContainer types.Container,
) error {
	// Initialize logger with container and API version context for consistent log messages.
	// The name and ID fields match the other recreation logs.
	clogVal := log.With().
		Str("container", sourceContainer.Name()).
		Str("id", containerID.ShortID()).
		Str("version", clientVersion).
		Logger()
	clog := &clogVal

	// Handle nil network configuration by checking if the container is running and not in host network mode.
	if config == nil {
		clog.Debug().Msg("Nil network configuration provided")
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

	// processEndpoint clears every MAC address for host networks and legacy API
	// versions (< 1.44), so the processed config carries none in those modes. The
	// daemon discards endpoint MAC addresses below API 1.44, and accepts a MAC
	// address only for bridge and user-defined networks.
	if isHostNetwork || dockerAPIVersion.LessThan(clientVersion, "1.44") {
		clog.Debug().Msg("Skipping MAC address validation for host network or legacy API")

		return nil
	}

	// Scan network endpoints to check for MAC address presence.
	// A MAC address is expected for running containers in non-host networks with modern API versions.
	foundMac := false

	for networkName, endpoint := range config.EndpointsConfig {
		if endpoint == nil {
			clog.Debug().
				Str("network", networkName).
				Msg("Skipping nil endpoint in MAC validation")

			continue
		}

		if len(endpoint.MacAddress) > 0 {
			foundMac = true
			// Log the found MAC address at debug level for diagnostic purposes.
			clog.Debug().
				Str("network", networkName).
				Str("mac_address", endpoint.MacAddress.String()).
				Msg("MAC address found in network configuration")
		}
	}

	// Retrieve container state to determine if it's running, which affects MAC address expectations.
	// Non-running containers (e.g., created, exited) typically lack MAC addresses due to inactive network interfaces.
	containerInfo := sourceContainer.ContainerInfo()
	isRunning := sourceContainer.IsRunning()

	// Extract the container's state (e.g., "running", "created", "exited") for logging context.
	// Use "unknown" as a fallback if container metadata is incomplete to ensure safe logging.
	containerState := "unknown"
	if containerInfo != nil && containerInfo.State != nil {
		containerState = string(containerInfo.State.Status)
	}

	// Handle non-host network mode (e.g., bridge, overlay) for modern API versions (>= 1.44).
	// MAC addresses are expected for running containers but not for non-running ones.
	if !foundMac {
		if !isRunning {
			// Non-running containers (e.g., created, exited) typically lack MAC addresses
			// because their network interfaces are inactive.
			clog.Debug().
				Str("state", containerState).
				Msg("No MAC address for non-running container (expected)")

			return nil
		}

		// If all original MACs were engine-generated and every non-nil original
		// endpoint is present in the processed config, the absence is expected
		// because processEndpoint intentionally clears them to avoid stale MACs
		// after IP reassignment.
		if onlyGeneratedMacs(sourceContainer, config) {
			clog.Debug().Msg("No MAC address found. All original MACs were engine-generated and intentionally cleared")

			return nil
		}

		// Running containers should have MAC addresses, but absence may indicate
		// either a lack of support or a configuration issue.
		clog.Debug().
			Str("state", containerState).
			Msg("No MAC address found in non-host network config")

		return errNoMacInNonHost
	}

	// MAC addresses are expected to be found in a running container with a modern API.
	// Log at debug level to confirm successful validation.
	clog.Debug().Msg("MAC address validation passed")

	return nil
}

// filterAliases removes the container's short ID from the list of aliases.
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
