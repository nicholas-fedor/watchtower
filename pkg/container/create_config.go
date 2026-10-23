package container

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerNetwork "github.com/moby/moby/api/types/network"

	"github.com/nicholas-fedor/watchtower/internal/container/labels"
)

// Errors for building a container's create configuration.
var (
	// errNoImageInfo indicates the container lacks image metadata required for recreation.
	errNoImageInfo = errors.New("no image info available")
	// errNoContainerInfo indicates the container lacks metadata required for recreation.
	errNoContainerInfo = errors.New("no container info available")
	// errInvalidConfig indicates the container's configuration is invalid for recreation.
	errInvalidConfig = errors.New("invalid container configuration")
)

// GetCreateConfig generates a container configuration for recreation.
//
// It isolates runtime overrides from image defaults and sets the image name.
//
// Returns:
//   - *dockerContainer.Config: Configuration for container creation.
func (c *Container) GetCreateConfig() *dockerContainer.Config {
	c.mu.RLock()
	defer c.mu.RUnlock()

	clogVal := c.logger().With().
		Str("container", c.Name()).
		Logger()
	clog := &clogVal

	if c.containerInfo == nil {
		clog.Warn().Msg("No container info available, returning minimal config")

		return &dockerContainer.Config{Image: c.imageNameLocked()}
	}

	config := *c.containerInfo.Config
	hostConfig := c.containerInfo.HostConfig

	// Handle missing image info, including a nil image Config. The containerd
	// image store can return image inspect with Config unset.
	if c.imageInfo == nil || c.imageInfo.Config == nil {
		clog.Warn().Msg("No image config available, using container config as-is")

		config.Image = c.imageNameLocked()

		return &config
	}

	// Compare with image config to clear defaults.
	imageConfig := c.imageInfo.Config
	if config.WorkingDir == imageConfig.WorkingDir {
		config.WorkingDir = ""
	}

	if config.User == imageConfig.User {
		config.User = ""
	}

	if hostConfig.NetworkMode.IsContainer() {
		config.Hostname = "" // Clear hostname for container network mode.
	}

	if hostConfig.UTSMode != "" {
		config.Hostname = "" // Clear hostname for UTS mode.
	}

	if slices.Equal(config.Entrypoint, imageConfig.Entrypoint) {
		config.Entrypoint = nil
		if slices.Equal(config.Cmd, imageConfig.Cmd) {
			config.Cmd = nil
		}
	}
	// Clear HEALTHCHECK if it matches the image default. Work on a copy, as
	// the health check is shared with the container's inspect data.
	if config.Healthcheck != nil && imageConfig.Healthcheck != nil {
		healthcheck := *config.Healthcheck
		config.Healthcheck = &healthcheck

		if slices.Equal(config.Healthcheck.Test, imageConfig.Healthcheck.Test) {
			config.Healthcheck.Test = nil
		}

		if config.Healthcheck.Retries == imageConfig.Healthcheck.Retries {
			config.Healthcheck.Retries = 0
		}

		if config.Healthcheck.Interval == imageConfig.Healthcheck.Interval {
			config.Healthcheck.Interval = 0
		}

		if config.Healthcheck.Timeout == imageConfig.Healthcheck.Timeout {
			config.Healthcheck.Timeout = 0
		}

		if config.Healthcheck.StartPeriod == imageConfig.Healthcheck.StartPeriod {
			config.Healthcheck.StartPeriod = 0
		}
	}

	// Subtract image defaults from config.
	config.Env = sliceSubtract(config.Env, imageConfig.Env)

	// Preserve the watchtower label if present, as it may be subtracted as an image default.
	watchtowerLabelValue, hasWatchtowerLabel := config.Labels[labels.Watchtower]

	config.Labels = stringMapSubtract(config.Labels, imageConfig.Labels)
	if hasWatchtowerLabel {
		if config.Labels == nil {
			config.Labels = make(map[string]string)
		}

		config.Labels[labels.Watchtower] = watchtowerLabelValue
	}

	config.Volumes = structMapSubtract(config.Volumes, imageConfig.Volumes)

	// Ensure ExposedPorts is initialized before removing image-exposed ports
	// and adding ports from host config bindings. Work on a copy, as the map
	// is shared with the container's inspect data.
	config.ExposedPorts = maps.Clone(config.ExposedPorts)
	if config.ExposedPorts == nil {
		config.ExposedPorts = dockerNetwork.PortSet{}
	}

	for port := range config.ExposedPorts {
		_, ok := imageConfig.ExposedPorts[port.String()]
		if ok {
			delete(config.ExposedPorts, port) // Remove ports exposed by image.
		}
	}

	for p := range hostConfig.PortBindings {
		config.ExposedPorts[p] = struct{}{} // Add ports from bindings.
	}

	config.Image = c.imageNameLocked()
	clog.Debug().
		Str("image", config.Image).
		Msg("Generated create config")

	return &config
}

// GetCreateHostConfig generates a host configuration for recreation.
//
// It adjusts link formats for Docker API compatibility and copies slices that
// must not alias inspect data.
//
// Returns:
//   - *dockerContainer.HostConfig: Host configuration for container creation.
func (c *Container) GetCreateHostConfig() *dockerContainer.HostConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	clogVal := c.logger().With().
		Str("container", c.Name()).
		Logger()
	clog := &clogVal

	if c.containerInfo == nil || c.containerInfo.HostConfig == nil {
		clog.Warn().Msg("No container host config available")

		return &dockerContainer.HostConfig{}
	}

	hostConfigCopy := *c.containerInfo.HostConfig
	hostConfig := &hostConfigCopy

	// Deep copy slices that will be mutated to avoid modifying the original
	// under a read lock.
	if len(hostConfig.Devices) > 0 {
		devicesCopy := make([]dockerContainer.DeviceMapping, len(hostConfig.Devices))
		copy(devicesCopy, hostConfig.Devices)
		hostConfig.Devices = devicesCopy
	}

	if len(hostConfig.VolumesFrom) > 0 {
		// Copy VolumesFrom so the returned host config does not alias inspect.
		volumesFromCopy := slices.Clone(hostConfig.VolumesFrom)
		hostConfig.VolumesFrom = volumesFromCopy
	}

	// Adjust link format for each entry (and drop invalid ones).
	adjusted := make([]string, 0, len(hostConfig.Links))
	for _, link := range hostConfig.Links {
		if !strings.Contains(link, ":") {
			clog.Error().
				Str("link", link).
				Msg("Invalid link format, expected 'name:alias'")

			continue
		}

		parts := strings.SplitN(link, ":", linkPartsCount)
		if len(parts) != linkPartsCount {
			clog.Error().
				Str("link", link).
				Msg("Invalid link format, expected exactly one colon separator")

			continue
		}

		normalizedName := NormalizeContainerName(parts[0])
		alias := parts[1]
		adjustedLink := fmt.Sprintf("%s:%s", normalizedName, alias)
		adjusted = append(adjusted, adjustedLink)
		clog.Debug().
			Str("link", adjustedLink).
			Msg("Adjusted link for host config")
	}

	hostConfig.Links = adjusted

	// Normalize device CgroupPermissions for Podman compatibility.
	//
	// Podman leaves CgroupPermissions empty in Docker API inspect responses.
	// Both Docker and Podman treat bare device specifications (without explicit
	// permissions) as "rwm". Defaulting here prevents "empty device mode"
	// errors when recreating containers.
	for i := range hostConfig.Devices {
		if hostConfig.Devices[i].CgroupPermissions == "" {
			hostConfig.Devices[i].CgroupPermissions = "rwm"
			clog.Debug().
				Str("device", hostConfig.Devices[i].PathOnHost).
				Msg("Defaulted empty device CgroupPermissions to 'rwm'")
		}
	}

	return hostConfig
}

// VerifyConfiguration validates the container's metadata for recreation.
//
// Returns:
//   - error: Non-nil if metadata is missing or invalid, nil on success.
func (c *Container) VerifyConfiguration() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check for nil image info.
	if c.imageInfo == nil {
		c.logger().Debug().
			Str("container", "<unknown>").
			Msg("No image info available")

		return errNoImageInfo
	}

	// Check for nil container info.
	if c.containerInfo == nil {
		c.logger().Debug().
			Str("container", "<unknown>").
			Msg("No container info available")

		return errNoContainerInfo
	}

	clogVal := c.logger().With().
		Str("container", c.Name()).
		Logger()
	clog := &clogVal
	// Validate config and host config presence.
	if c.containerInfo.Config == nil || c.containerInfo.HostConfig == nil {
		clog.Debug().Msg("Invalid container configuration")

		return errInvalidConfig
	}

	// Ensure ExposedPorts is initialized if PortBindings exist.
	initExposed := len(c.containerInfo.HostConfig.PortBindings) > 0 &&
		c.containerInfo.Config.ExposedPorts == nil

	// Find port bindings with empty port values.
	// Docker rejects ports with empty port numbers (e.g., "/tcp") with
	// "invalid port range: value is empty" during ContainerCreate.
	var emptyPorts []dockerNetwork.Port

	for port := range c.containerInfo.HostConfig.PortBindings {
		if port.Port() == "" {
			emptyPorts = append(emptyPorts, port)
		}
	}

	if initExposed || len(emptyPorts) > 0 {
		c.replaceInspectLocked(func(info *dockerContainer.InspectResponse) {
			exposed := maps.Clone(info.Config.ExposedPorts)
			if exposed == nil && initExposed {
				exposed = dockerNetwork.PortSet{}

				clog.Debug().Msg("Initialized ExposedPorts due to PortBindings")
			}

			bindings := maps.Clone(info.HostConfig.PortBindings)

			// Skip and remove completely empty port entries.
			for _, port := range emptyPorts {
				clog.Warn().Msg("Skipping empty port binding and exposed port")

				delete(bindings, port)
				delete(exposed, port)
			}

			info.Config.ExposedPorts = exposed
			info.HostConfig.PortBindings = bindings
		})
	}

	clog.Debug().Msg("Verified container configuration")

	return nil
}

// HasExposedPorts checks if the container has any host-bound port mappings.
//
// This is used to determine if a Watchtower container should skip self-updates,
// as host-bound ports cause port conflicts during the self-update process where
// the old container holds the port while the new container tries to bind it.
//
// Only actual host-to-container port bindings (HostConfig.PortBindings) are
// considered, not merely declared exposed ports (Config.ExposedPorts), since
// declared-but-unbound ports do not cause conflicts.
//
// Returns:
//   - bool: True if the container has host-bound port mappings, false otherwise.
func (c *Container) HasExposedPorts() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.containerInfo == nil {
		return false
	}

	// Check if there are any port bindings (host-to-container port mappings).
	// Guard against nil HostConfig before inspecting PortBindings.
	if c.containerInfo.HostConfig != nil && len(c.containerInfo.HostConfig.PortBindings) > 0 {
		return true
	}

	return false
}

// sliceSubtract returns elements in the first slice that are not in the second slice.
//
// Parameters:
//   - slice: Source slice.
//   - subtractFrom: Slice containing elements to exclude.
//
// Returns:
//   - []string: Slice containing elements unique to the source slice.
func sliceSubtract(slice, subtractFrom []string) []string {
	result := []string{}

	for _, element1 := range slice {
		found := slices.Contains(subtractFrom, element1)

		if !found {
			result = append(result, element1)
		}
	}

	return result
}

// stringMapSubtract removes matching key-value pairs.
//
// Parameters:
//   - map1: Source map.
//   - map2: Map to subtract.
//
// Returns:
//   - map[string]string: New map with unique or differing entries.
func stringMapSubtract(map1, map2 map[string]string) map[string]string {
	result := map[string]string{}

	for key1, value1 := range map1 {
		if value2, ok := map2[key1]; ok {
			if value2 != value1 {
				result[key1] = value1
			}
		} else {
			result[key1] = value1
		}
	}

	return result
}

// structMapSubtract removes matching keys.
//
// Parameters:
//   - map1: Source map.
//   - map2: Map to subtract.
//
// Returns:
//   - map[string]struct{}: New map with unique keys.
func structMapSubtract(map1, map2 map[string]struct{}) map[string]struct{} {
	result := map[string]struct{}{}

	for key1, value1 := range map1 {
		if _, ok := map2[key1]; !ok {
			result[key1] = value1
		}
	}

	return result
}
