// Package container provides functionality for managing Docker containers within Watchtower.
// This file defines the Container type and its core methods, implementing the types.Container interface
// to handle container state, metadata, and configuration for updates and recreation.
package container

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/go-connections/nat"
	"github.com/sirupsen/logrus"

	dockerContainerType "github.com/docker/docker/api/types/container"
	dockerImageType "github.com/docker/docker/api/types/image"
	dockerNetworkType "github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Operations defines the minimal interface for container operations in Watchtower.
type Operations interface {
	ContainerCreate(
		ctx context.Context,
		config *dockerContainerType.Config,
		hostConfig *dockerContainerType.HostConfig,
		networkingConfig *dockerNetworkType.NetworkingConfig,
		platform *ocispec.Platform,
		containerName string,
	) (dockerContainerType.CreateResponse, error)
	ContainerStart(
		ctx context.Context,
		containerID string,
		options dockerContainerType.StartOptions,
	) error
	ContainerRemove(
		ctx context.Context,
		containerID string,
		options dockerContainerType.RemoveOptions,
	) error
	NetworkConnect(
		ctx context.Context,
		networkID, containerID string,
		config *dockerNetworkType.EndpointSettings,
	) error
	ContainerRename(
		ctx context.Context,
		containerID, newContainerName string,
	) error
}

// Constants for container operations.
const (
	linkPartsCount = 2 // Number of parts expected in a link (name:alias)
)

// Container represents a running Docker container managed by Watchtower.
//
// It implements the types.Container interface, storing state and metadata
// for container operations such as updates and lifecycle hooks.
//
//nolint:recvcheck // Intentional mix: value receivers for reads, pointer receivers for writes
type Container struct {
	LinkedToRestarting bool                                 // Indicates if linked to a restarting container
	Stale              bool                                 // Marks the container as having an outdated image
	containerInfo      *dockerContainerType.InspectResponse // Docker container metadata
	imageInfo          *dockerImageType.InspectResponse     // Docker image metadata
}

// NewContainer creates a new Container instance with the specified metadata.
//
// Parameters:
//   - containerInfo: Docker container metadata.
//   - imageInfo: Docker image metadata.
//
// Returns:
//   - *Container: Initialized container instance.
func NewContainer(
	containerInfo *dockerContainerType.InspectResponse,
	imageInfo *dockerImageType.InspectResponse,
) *Container {
	// Initialize with default state.
	c := &Container{
		LinkedToRestarting: false,
		Stale:              false,
		containerInfo:      containerInfo,
		imageInfo:          imageInfo,
	}
	logrus.WithFields(logrus.Fields{
		"container": c.Name(),
		"id":        c.ID().ShortID(),
		"image":     c.SafeImageID(),
	}).Debug("Created new container instance")

	return c
}

// IsLinkedToRestarting returns whether the container is linked to a restarting container.
//
// Returns:
//   - bool: True if linked, false otherwise.
func (c Container) IsLinkedToRestarting() bool {
	return c.LinkedToRestarting
}

// SetLinkedToRestarting sets the linked-to-restarting state.
//
// Parameters:
//   - value: New state value.
func (c *Container) SetLinkedToRestarting(value bool) {
	c.LinkedToRestarting = value
}

// IsStale returns whether the container’s image is outdated.
//
// Returns:
//   - bool: True if stale, false otherwise.
func (c Container) IsStale() bool {
	return c.Stale
}

// SetStale marks the container as having an outdated image.
//
// Parameters:
//   - value: New stale value.
func (c *Container) SetStale(value bool) {
	c.Stale = value
}

// ToRestart determines if the container should be restarted.
//
// Returns:
//   - bool: True if stale or linked to restarting, false otherwise.
func (c Container) ToRestart() bool {
	return c.Stale || c.LinkedToRestarting
}

// ContainerInfo returns the full Docker container metadata.
//
// Returns:
//   - *dockerContainerType.InspectResponse: Container metadata.
func (c Container) ContainerInfo() *dockerContainerType.InspectResponse {
	return c.containerInfo
}

// ID returns the unique identifier of the container.
//
// Returns:
//   - types.ContainerID: Container ID.
func (c Container) ID() types.ContainerID {
	return types.ContainerID(c.containerInfo.ID)
}

// IsRunning checks if the container is currently running.
//
// Returns:
//   - bool: True if running, false otherwise.
func (c Container) IsRunning() bool {
	if c.containerInfo == nil || c.containerInfo.State == nil {
		return false
	}

	return c.containerInfo.State.Running
}

// IsRestarting checks if the container is currently restarting.
//
// Returns:
//   - bool: True if restarting, false otherwise.
func (c Container) IsRestarting() bool {
	return c.containerInfo.State.Restarting
}

// Name returns the name of the container.
//
// Returns:
//   - string: Container name.
func (c Container) Name() string {
	return strings.TrimPrefix(c.containerInfo.Name, "/")
}

// ImageID returns the ID of the container’s image.
//
// Returns:
//   - types.ImageID: Image ID or empty string if imageInfo is nil.
func (c Container) ImageID() types.ImageID {
	if c.imageInfo == nil {
		return ""
	}

	return types.ImageID(c.imageInfo.ID)
}

// SafeImageID returns the image ID or an empty string if unavailable.
//
// Returns:
//   - types.ImageID: Image ID or empty string if imageInfo is nil.
func (c Container) SafeImageID() types.ImageID {
	if c.imageInfo == nil {
		return ""
	}

	return types.ImageID(c.imageInfo.ID)
}

// ImageName returns the name of the container’s image.
//
// It uses the Zodiac label if present, otherwise Config.Image, appending ":latest" if untagged.
//
// Returns:
//   - string: Image name (e.g., "alpine:latest").
func (c Container) ImageName() string {
	clog := logrus.WithField("container", c.Name())

	// Prefer Zodiac label for image name.
	imageName, ok := c.getLabelValue(zodiacLabel)
	if !ok {
		imageName = c.containerInfo.Config.Image

		clog.Debug("Using Config.Image for image name")
	} else {
		clog.WithField("label", zodiacLabel).Debug("Using Zodiac label for image name")
	}

	// Append default tag if none specified.
	if !strings.Contains(imageName, ":") {
		imageName += ":latest"
		clog.WithField("image", imageName).Debug("Appended :latest to image name")
	}

	return imageName
}

// HasImageInfo indicates whether image metadata is available.
//
// Returns:
//   - bool: True if imageInfo is non-nil, false otherwise.
func (c Container) HasImageInfo() bool {
	return c.imageInfo != nil
}

// ImageInfo returns the Docker image metadata.
//
// Returns:
//   - *dockerImageType.InspectResponse: Image metadata or nil if unavailable.
func (c Container) ImageInfo() *dockerImageType.InspectResponse {
	return c.imageInfo
}

// GetCreateConfig generates a container configuration for recreation.
//
// It isolates runtime overrides from image defaults and sets the image name.
//
// Returns:
//   - *dockerContainerType.Config: Configuration for container creation.
func (c Container) GetCreateConfig() *dockerContainerType.Config {
	clog := logrus.WithField("container", c.Name())
	config := c.containerInfo.Config
	hostConfig := c.containerInfo.HostConfig

	// Handle missing image info case.
	if c.imageInfo == nil {
		clog.Warn("No image info available, using container config as-is")

		config.Image = c.ImageName()

		return config
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

	if util.SliceEqual(config.Entrypoint, imageConfig.Entrypoint) {
		config.Entrypoint = nil
		if util.SliceEqual(config.Cmd, imageConfig.Cmd) {
			config.Cmd = nil
		}
	}
	// Clear HEALTHCHECK if it matches the image default.
	if config.Healthcheck != nil && imageConfig.Healthcheck != nil {
		if util.SliceEqual(config.Healthcheck.Test, imageConfig.Healthcheck.Test) {
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
	config.Env = util.SliceSubtract(config.Env, imageConfig.Env)
	config.Labels = util.StringMapSubtract(config.Labels, imageConfig.Labels)
	config.Volumes = util.StructMapSubtract(config.Volumes, imageConfig.Volumes)

	for k := range config.ExposedPorts {
		if _, ok := imageConfig.ExposedPorts[string(k)]; ok {
			delete(config.ExposedPorts, k) // Remove ports exposed by image.
		}
	}

	for p := range hostConfig.PortBindings {
		config.ExposedPorts[p] = struct{}{} // Add ports from bindings.
	}

	config.Image = c.ImageName()
	clog.WithField("image", config.Image).Debug("Generated create config")

	return config
}

// GetCreateHostConfig generates a host configuration for recreation.
//
// It adjusts link formats for Docker API compatibility.
//
// Returns:
//   - *dockerContainerType.HostConfig: Host configuration for container creation.
func (c Container) GetCreateHostConfig() *dockerContainerType.HostConfig {
	clog := logrus.WithField("container", c.Name())

	hostConfig := c.containerInfo.HostConfig

	// Adjust link format for each entry.
	for i, link := range hostConfig.Links {
		if !strings.Contains(link, ":") {
			clog.WithField("link", link).Warn("Invalid link format, expected 'name:alias'")

			continue // Skip invalid links
		}

		parts := strings.SplitN(link, ":", linkPartsCount)
		name := parts[0]
		alias := strings.TrimPrefix(parts[1], "/") // Remove leading slash if present.
		hostConfig.Links[i] = fmt.Sprintf("%s:/%s", name, alias)
		clog.WithField("link", hostConfig.Links[i]).Debug("Adjusted link for host config")
	}

	return hostConfig
}

// VerifyConfiguration validates the container’s metadata for recreation.
//
// Returns:
//   - error: Non-nil if metadata is missing or invalid, nil on success.
func (c Container) VerifyConfiguration() error {
	// Check for nil image info.
	if c.imageInfo == nil {
		logrus.WithField("container", "<unknown>").Debug("No image info available")

		return errNoImageInfo
	}

	// Check for nil container info.
	if c.containerInfo == nil {
		logrus.WithField("container", "<unknown>").Debug("No container info available")

		return errNoContainerInfo
	}

	clog := logrus.WithField("container", c.Name())
	// Validate config and host config presence.
	if c.containerInfo.Config == nil || c.containerInfo.HostConfig == nil {
		clog.Debug("Invalid container configuration")

		return errInvalidConfig
	}

	// Ensure ExposedPorts is initialized if PortBindings exist.
	if len(c.containerInfo.HostConfig.PortBindings) > 0 &&
		c.containerInfo.Config.ExposedPorts == nil {
		c.containerInfo.Config.ExposedPorts = make(map[nat.Port]struct{})

		clog.Debug("Initialized ExposedPorts due to PortBindings")
	}

	clog.Debug("Verified container configuration")

	return nil
}

// Links returns a list of container names this container depends on.
//
// It combines depends-on label and HostConfig links, ensuring leading slashes.
//
// Returns:
//   - []string: List of linked container names.
func (c Container) Links() []string {
	clog := logrus.WithField("container", c.Name())

	var links []string

	// Check depends-on label first.
	dependsOnLabelValue := c.getLabelValueOrEmpty(dependsOnLabel)
	if dependsOnLabelValue != "" {
		for link := range strings.SplitSeq(dependsOnLabelValue, ",") {
			if !strings.HasPrefix(link, "/") {
				link = "/" + link // Add leading slash if missing.
			}

			links = append(links, link)
		}

		clog.WithField("depends_on", dependsOnLabelValue).
			Debug("Retrieved links from depends-on label")

		return links
	}

	// Fall back to HostConfig links and network mode.
	if c.containerInfo != nil && c.containerInfo.HostConfig != nil {
		for _, link := range c.containerInfo.HostConfig.Links {
			name := strings.Split(link, ":")[0]
			links = append(links, name)
		}

		networkMode := c.containerInfo.HostConfig.NetworkMode
		if networkMode.IsContainer() {
			links = append(links, networkMode.ConnectedContainer()) // Add network dependency.
		}

		clog.WithField("links", links).Debug("Retrieved links from host config")
	}

	return links
}
