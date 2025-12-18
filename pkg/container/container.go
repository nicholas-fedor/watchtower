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

	dockerContainer "github.com/docker/docker/api/types/container"
	dockerImage "github.com/docker/docker/api/types/image"
	dockerNetwork "github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/compose"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Constants for container operations.
const (
	linkPartsCount = 2 // Number of parts expected in a link (name:alias)
)

// Operations defines the minimal interface for container operations in Watchtower.
type Operations interface {
	ContainerCreate(
		ctx context.Context,
		config *dockerContainer.Config,
		hostConfig *dockerContainer.HostConfig,
		networkingConfig *dockerNetwork.NetworkingConfig,
		platform *ocispec.Platform,
		containerName string,
	) (dockerContainer.CreateResponse, error)
	ContainerStart(
		ctx context.Context,
		containerID string,
		options dockerContainer.StartOptions,
	) error
	ContainerRemove(
		ctx context.Context,
		containerID string,
		options dockerContainer.RemoveOptions,
	) error
	NetworkConnect(
		ctx context.Context,
		networkID, containerID string,
		config *dockerNetwork.EndpointSettings,
	) error
	ContainerRename(
		ctx context.Context,
		containerID, newContainerName string,
	) error
}

// Container represents a running Docker container managed by Watchtower.
//
// It implements the types.Container interface, storing state and metadata
// for container operations such as updates and lifecycle hooks.
//
//nolint:recvcheck // Intentional mix: value receivers for reads, pointer receivers for writes
type Container struct {
	LinkedToRestarting bool                             // Indicates if linked to a restarting container
	Stale              bool                             // Marks the container as having an outdated image
	OldImageID         types.ImageID                    // Stores the image ID before update for cleanup tracking
	normalizedName     string                           // Cached normalized container name
	containerInfo      *dockerContainer.InspectResponse // Docker container metadata
	imageInfo          *dockerImage.InspectResponse     // Docker image metadata
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
	containerInfo *dockerContainer.InspectResponse,
	imageInfo *dockerImage.InspectResponse,
) *Container {
	name := ""
	if containerInfo != nil {
		name = containerInfo.Name
	}
	// Initialize with default state.
	c := &Container{
		LinkedToRestarting: false,
		Stale:              false,
		OldImageID:         "",
		normalizedName:     util.NormalizeContainerName(name),
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
func (c Container) ContainerInfo() *dockerContainer.InspectResponse {
	return c.containerInfo
}

// ID returns the unique identifier of the container.
//
// Returns:
//   - types.ContainerID: Container ID.
func (c Container) ID() types.ContainerID {
	if c.containerInfo == nil {
		return ""
	}

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
	if c.containerInfo == nil || c.containerInfo.State == nil {
		return false
	}

	return c.containerInfo.State.Restarting
}

// Name returns the normalized name of the container.
//
// Returns:
//   - string: Normalized container name.
func (c Container) Name() string {
	return c.normalizedName
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
		if c.containerInfo == nil || c.containerInfo.Config == nil {
			clog.Warn("No container config available, using default image name")

			return "unknown:latest"
		}

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
func (c Container) ImageInfo() *dockerImage.InspectResponse {
	return c.imageInfo
}

// GetCreateConfig generates a container configuration for recreation.
//
// It isolates runtime overrides from image defaults and sets the image name.
//
// Returns:
//   - *dockerContainerType.Config: Configuration for container creation.
func (c Container) GetCreateConfig() *dockerContainer.Config {
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

	if hostConfig.UTSMode != "" {
		config.Hostname = "" // Clear hostname for UTS mode.
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
func (c Container) GetCreateHostConfig() *dockerContainer.HostConfig {
	clog := logrus.WithField("container", c.Name())

	if c.containerInfo == nil || c.containerInfo.HostConfig == nil {
		clog.Warn("No container host config available")

		return &dockerContainer.HostConfig{}
	}

	hostConfig := c.containerInfo.HostConfig

	// Adjust link format for each entry (and drop invalid ones).
	adjusted := make([]string, 0, len(hostConfig.Links))
	for _, link := range hostConfig.Links {
		if !strings.Contains(link, ":") {
			clog.WithField("link", link).Error("Invalid link format, expected 'name:alias'")

			continue
		}

		parts := strings.SplitN(link, ":", linkPartsCount)
		if len(parts) != linkPartsCount {
			clog.WithField("link", link).
				Error("Invalid link format, expected exactly one colon separator")

			continue
		}

		normalizedName := util.NormalizeContainerName(parts[0])
		alias := parts[1]
		adjustedLink := fmt.Sprintf("%s:%s", normalizedName, alias)
		adjusted = append(adjusted, adjustedLink)
		clog.WithField("link", adjustedLink).Debug("Adjusted link for host config")
	}

	hostConfig.Links = adjusted

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
// It checks com.centurylinklabs.watchtower.depends-on first,
// then com.docker.compose.depends_on using Docker Compose v5 API functions,
// then falls back to HostConfig links and network mode.
//
// Returns:
//   - []string: List of linked container names.
func (c Container) Links() []string {
	clog := logrus.WithField("container", c.Name())

	// Check watchtower depends-on label first.
	if links := getLinksFromWatchtowerLabel(c, clog); links != nil {
		return links
	}

	// Check compose depends-on label.
	if links := getLinksFromComposeLabel(c, clog); links != nil {
		return links
	}

	// Fall back to HostConfig links and network mode.
	return getLinksFromHostConfig(c, clog)
}

// ResolveContainerIdentifier returns the service name if available,
// otherwise container name. Falls back to container ID if name is empty.
//
// Parameters:
//   - c: Container to get identifier for
//
// Returns:
//   - string: Service name if available, otherwise container name, otherwise container ID
//     Always returns a non-empty string for valid containers
func ResolveContainerIdentifier(c types.Container) string {
	// Get the container information.
	info := c.ContainerInfo()
	// Return container name if nil.
	if info == nil {
		return nameOrID(c)
	}

	// Get the container configuration
	cfg := info.Config
	// Return container name if nil.
	if cfg == nil {
		return nameOrID(c)
	}

	// Get the container labels
	labels := cfg.Labels
	// Return container name if empty.
	if len(labels) == 0 {
		return nameOrID(c)
	}

	if serviceName := compose.GetServiceName(labels); serviceName != "" {
		// Return service name from Compose label.
		return serviceName
	}

	return nameOrID(c)
}

// nameOrID returns the container name if non-empty, otherwise returns the container ID.
func nameOrID(c types.Container) string {
	if name := c.Name(); name != "" {
		return name
	}

	return string(c.ID())
}

// getLinksFromWatchtowerLabel extracts dependency links from the
// watchtower depends-on label.
//
// It parses the com.centurylinklabs.watchtower.depends-on label value,
// splitting on commas and normalizing each container name.
//
// Parameters:
//   - c: Container instance
//   - clog: Logger instance for debug output
//
// Returns:
//   - []string: List of linked container names, empty if label not present
func getLinksFromWatchtowerLabel(c Container, clog *logrus.Entry) []string {
	dependsOnLabelValue := c.getLabelValueOrEmpty(dependsOnLabel)
	if dependsOnLabelValue == "" {
		return nil
	}

	// Pre-allocate slice based on comma-separated values
	parts := strings.Split(dependsOnLabelValue, ",")
	normalizedLinks := make([]string, 0, len(parts))

	for _, normalizedLink := range parts {
		normalizedLink = strings.TrimSpace(normalizedLink)
		if normalizedLink == "" {
			continue
		}

		normalizedLink = util.NormalizeContainerName(normalizedLink)
		normalizedLinks = append(normalizedLinks, normalizedLink)
	}

	clog.WithField("depends_on", dependsOnLabelValue).
		Debug("Retrieved links from watchtower depends-on label")

	return normalizedLinks
}

// getLinksFromComposeLabel extracts dependency links from the
// Docker Compose depends-on label.
//
// It parses the com.docker.compose.depends_on label value using the compose package,
// and normalizes each service name.
//
// Parameters:
//   - c: Container instance
//   - clog: Logger instance for debug output
//
// Returns:
//   - []string: List of linked container names, empty if label not present
func getLinksFromComposeLabel(c Container, clog *logrus.Entry) []string {
	composeDependsOnLabelValue := c.getLabelValueOrEmpty(compose.ComposeDependsOnLabel)
	clog.WithFields(logrus.Fields{
		"label": compose.ComposeDependsOnLabel,
		"value": composeDependsOnLabelValue,
	}).Debug("Checked compose depends-on label")

	if composeDependsOnLabelValue == "" {
		return nil
	}

	clog.WithField("raw_label_value", composeDependsOnLabelValue).
		Debug("Parsing compose depends-on label")

	services := compose.ParseDependsOnLabel(composeDependsOnLabelValue)

	normalizedLinks := make([]string, 0, len(services))
	for _, service := range services {
		normalizedLinks = append(normalizedLinks, util.NormalizeContainerName(service))
	}

	if len(normalizedLinks) == 0 {
		return nil
	}

	clog.WithFields(logrus.Fields{
		"compose_depends_on": composeDependsOnLabelValue,
		"parsed_links":       normalizedLinks,
	}).Debug("Retrieved links from compose depends-on label")

	return normalizedLinks
}

// getLinksFromHostConfig extracts dependency links from Docker HostConfig.
//
// It parses HostConfig.Links and network mode to determine container dependencies.
//
// Parameters:
//   - c: Container instance
//   - clog: Logger instance for debug output
//
// Returns:
//   - []string: List of linked container names
func getLinksFromHostConfig(c Container, clog *logrus.Entry) []string {
	if c.containerInfo == nil || c.containerInfo.HostConfig == nil {
		return nil
	}

	// Pre-allocate for links plus potential network mode dependency
	capacity := len(c.containerInfo.HostConfig.Links)

	networkMode := c.containerInfo.HostConfig.NetworkMode
	if networkMode.IsContainer() {
		capacity++
	}

	normalizedLinks := make([]string, 0, capacity)

	for _, link := range c.containerInfo.HostConfig.Links {
		if !strings.Contains(link, ":") {
			clog.WithField("link", link).
				Warn("Invalid link format in host config, expected 'name:alias'")

			continue
		}

		parts := strings.SplitN(link, ":", linkPartsCount)
		if len(parts) < 1 || parts[0] == "" {
			clog.WithField("link", link).
				Warn("Invalid link format in host config, missing container name")

			continue
		}

		normalizedName := util.NormalizeContainerName(parts[0])
		normalizedLinks = append(normalizedLinks, normalizedName)
	}

	// Add network dependency.
	if networkMode.IsContainer() {
		normalizedLinks = append(
			normalizedLinks,
			util.NormalizeContainerName(networkMode.ConnectedContainer()),
		)
	}

	clog.WithField("links", normalizedLinks).Debug("Retrieved links from host config")

	return normalizedLinks
}
