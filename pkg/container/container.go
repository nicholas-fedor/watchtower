// Package container provides functionality for managing Docker containers within Watchtower.
// This file defines the Container type and its core methods, implementing the types.Container interface
// to handle container state, metadata, and configuration for updates and recreation.
package container

import (
	"fmt"
	"strings"

	"github.com/docker/go-connections/nat"
	"github.com/sirupsen/logrus"

	dockerContainerType "github.com/docker/docker/api/types/container"
	dockerImageType "github.com/docker/docker/api/types/image"

	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Constants for container operations.
const (
	linkPartsCount = 2 // Number of parts expected in a link (name:alias)
)

// Container represents a running Docker container managed by Watchtower.
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
// It initializes the container with the provided containerInfo and imageInfo,
// setting LinkedToRestarting and Stale to false by default.
func NewContainer(
	containerInfo *dockerContainerType.InspectResponse,
	imageInfo *dockerImageType.InspectResponse,
) *Container {
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

// State Management Methods
// These methods manage and query the container’s state related to updates and dependencies.

// IsLinkedToRestarting returns whether the container is linked to a restarting container.
// It reflects the current value of the LinkedToRestarting field.
func (c Container) IsLinkedToRestarting() bool {
	return c.LinkedToRestarting
}

// SetLinkedToRestarting sets whether the container is linked to a restarting container.
// It updates the LinkedToRestarting field to the specified value.
func (c *Container) SetLinkedToRestarting(value bool) {
	c.LinkedToRestarting = value
}

// IsStale returns whether the container’s image is outdated.
// It reflects the current value of the Stale field.
func (c Container) IsStale() bool {
	return c.Stale
}

// SetStale marks the container as having an outdated image.
// It updates the Stale field to the specified value.
func (c *Container) SetStale(value bool) {
	c.Stale = value
}

// ToRestart determines if the container should be restarted.
// It returns true if the container is stale or linked to a restarting container.
func (c Container) ToRestart() bool {
	return c.Stale || c.LinkedToRestarting
}

// Container Metadata Methods
// These methods provide access to the container’s basic metadata and state.

// ContainerInfo returns the full Docker container metadata.
// It provides the InspectResponse data for detailed container information.
func (c Container) ContainerInfo() *dockerContainerType.InspectResponse {
	return c.containerInfo
}

// ID returns the unique identifier of the container.
// It extracts the container ID from the metadata as a types.ContainerID.
func (c Container) ID() types.ContainerID {
	return types.ContainerID(c.containerInfo.ID)
}

// IsRunning checks if the container is currently running.
// It returns true if the container’s State.Running property is set.
func (c Container) IsRunning() bool {
	return c.containerInfo.State.Running
}

// IsRestarting checks if the container is currently restarting.
// It returns true if the container’s State.Restarting property is set.
func (c Container) IsRestarting() bool {
	return c.containerInfo.State.Restarting
}

// Name returns the name of the container.
// It extracts the name from the container’s metadata, typically prefixed with a slash.
func (c Container) Name() string {
	return c.containerInfo.Name
}

// ImageID returns the ID of the image used to start the container.
// It retrieves the image ID from imageInfo, panicking if imageInfo is nil.
func (c Container) ImageID() types.ImageID {
	return types.ImageID(c.imageInfo.ID)
}

// SafeImageID returns the image ID if available, or an empty string if not.
// It safely handles cases where imageInfo is nil, avoiding panics.
func (c Container) SafeImageID() types.ImageID {
	if c.imageInfo == nil {
		return ""
	}

	return types.ImageID(c.imageInfo.ID)
}

// ImageName returns the name of the image used to start the container.
// It prefers the Zodiac label if set, otherwise uses the Config.Image value,
// appending ":latest" if no tag is specified.
func (c Container) ImageName() string {
	clog := logrus.WithField("container", c.Name())

	imageName, ok := c.getLabelValue(zodiacLabel)
	if !ok {
		imageName = c.containerInfo.Config.Image

		clog.Debug("Using Config.Image for image name")
	} else {
		clog.WithField("label", zodiacLabel).Debug("Using Zodiac label for image name")
	}

	if !strings.Contains(imageName, ":") {
		imageName += ":latest"
		clog.WithField("image", imageName).Debug("Appended :latest to image name")
	}

	return imageName
}

// HasImageInfo indicates whether image metadata is available for the container.
// It returns true if imageInfo is non-nil, false otherwise.
func (c Container) HasImageInfo() bool {
	return c.imageInfo != nil
}

// ImageInfo returns the Docker image metadata for the container.
// It provides the InspectResponse data for the image, or nil if unavailable.
func (c Container) ImageInfo() *dockerImageType.InspectResponse {
	return c.imageInfo
}

// Configuration and Dependency Methods
// These methods handle container configuration and dependencies for recreation.

// GetCreateConfig generates a container configuration for recreation.
// It compares the current Config with the image’s Config to isolate runtime overrides,
// ensuring defaults are not unintentionally reapplied, and sets the image name accordingly.
func (c Container) GetCreateConfig() *dockerContainerType.Config {
	clog := logrus.WithField("container", c.Name())
	config := c.containerInfo.Config
	hostConfig := c.containerInfo.HostConfig

	if c.imageInfo == nil {
		clog.Warn("No image info available, using container config as-is")

		config.Image = c.ImageName()

		return config
	}

	imageConfig := c.imageInfo.Config
	if config.WorkingDir == imageConfig.WorkingDir {
		config.WorkingDir = ""
	}

	if config.User == imageConfig.User {
		config.User = ""
	}

	if hostConfig.NetworkMode.IsContainer() {
		config.Hostname = ""
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

	config.Env = util.SliceSubtract(config.Env, imageConfig.Env)
	config.Labels = util.StringMapSubtract(config.Labels, imageConfig.Labels)
	config.Volumes = util.StructMapSubtract(config.Volumes, imageConfig.Volumes)
	// Subtract ports exposed in the image from the container’s ports.
	for k := range config.ExposedPorts {
		if _, ok := imageConfig.ExposedPorts[k]; ok {
			delete(config.ExposedPorts, k)
		}
	}

	for p := range hostConfig.PortBindings {
		config.ExposedPorts[p] = struct{}{}
	}

	config.Image = c.ImageName()
	clog.WithField("image", config.Image).Debug("Generated create config")

	return config
}

// GetCreateHostConfig generates a host configuration for recreation.
// It adjusts the current HostConfig’s Links to a format suitable for the Docker create API.
func (c Container) GetCreateHostConfig() *dockerContainerType.HostConfig {
	clog := logrus.WithField("container", c.Name())

	hostConfig := c.containerInfo.HostConfig
	for i, link := range hostConfig.Links {
		if !strings.Contains(link, ":") {
			clog.WithField("link", link).Warn("Invalid link format, expected 'name:alias'")

			continue // Skip invalid links
		}

		parts := strings.SplitN(link, ":", linkPartsCount)
		name := parts[0]
		alias := strings.TrimPrefix(parts[1], "/") // Remove leading '/' if present
		hostConfig.Links[i] = fmt.Sprintf("%s:/%s", name, alias)
		clog.WithField("link", hostConfig.Links[i]).Debug("Adjusted link for host config")
	}

	return hostConfig
}

// VerifyConfiguration validates the container’s metadata for recreation.
// It checks for nil references in imageInfo, containerInfo, Config, and HostConfig,
// returning an error if any are missing or invalid, ensuring the container can be recreated.
func (c Container) VerifyConfiguration() error {
	if c.imageInfo == nil {
		logrus.WithField("container", "<unknown>").Debug("No image info available")

		return errNoImageInfo
	}

	if c.containerInfo == nil {
		logrus.WithField("container", "<unknown>").Debug("No container info available")

		return errNoContainerInfo
	}

	clog := logrus.WithField("container", c.Name())
	if c.containerInfo.Config == nil || c.containerInfo.HostConfig == nil {
		clog.Debug("Invalid container configuration")

		return errInvalidConfig
	}

	if len(c.containerInfo.HostConfig.PortBindings) > 0 &&
		c.containerInfo.Config.ExposedPorts == nil {
		c.containerInfo.Config.ExposedPorts = make(map[nat.Port]struct{})

		clog.Debug("Initialized ExposedPorts due to PortBindings")
	}

	clog.Debug("Verified container configuration")

	return nil
}

// Links returns a list of container names this container depends on.
// It combines names from the depends-on label and HostConfig.Links, ensuring a leading slash,
// and includes implicit network dependencies if applicable.
func (c Container) Links() []string {
	clog := logrus.WithField("container", c.Name())

	var links []string

	dependsOnLabelValue := c.getLabelValueOrEmpty(dependsOnLabel)
	if dependsOnLabelValue != "" {
		for _, link := range strings.Split(dependsOnLabelValue, ",") {
			if !strings.HasPrefix(link, "/") {
				link = "/" + link
			}

			links = append(links, link)
		}

		clog.WithField("depends_on", dependsOnLabelValue).
			Debug("Retrieved links from depends-on label")

		return links
	}

	if c.containerInfo != nil && c.containerInfo.HostConfig != nil {
		for _, link := range c.containerInfo.HostConfig.Links {
			name := strings.Split(link, ":")[0]
			links = append(links, name)
		}

		networkMode := c.containerInfo.HostConfig.NetworkMode
		if networkMode.IsContainer() {
			links = append(links, networkMode.ConnectedContainer())
		}

		clog.WithField("links", links).Debug("Retrieved links from host config")
	}

	return links
}
