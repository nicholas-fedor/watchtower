package container

import (
	"strings"
	"sync"

	"github.com/rs/zerolog"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"

	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Constants for container operations.
const (
	linkPartsCount = 2 // Number of parts expected in a link (name:alias)
)

// nopLog returns a fresh discarded logger for optional construction and fallbacks.
// It is not a package-level logger store: each call allocates a new Nop instance.
func nopLog() *zerolog.Logger {
	n := zerolog.Nop()

	return &n
}

// Container represents a running Docker container managed by Watchtower.
//
// It implements the types.Container interface, storing state and metadata
// for container operations such as updates and lifecycle hooks.
//

type Container struct {
	mu                 sync.RWMutex                     // Protects concurrent access to mutable fields
	LinkedToRestarting bool                             // Indicates if linked to a restarting container
	Stale              bool                             // Marks the container as having an outdated image
	OldImageID         types.ImageID                    // Stores the image ID before update for cleanup tracking
	normalizedName     string                           // Cached normalized container name
	imageName          string                           // Cached resolved image name with tag
	containerInfo      *dockerContainer.InspectResponse // Docker container metadata
	imageInfo          *dockerImage.InspectResponse     // Docker image metadata
	// log is the process logger for operational Warn/Error/Debug on this instance.
	// Set at construction (client list/get). Nil falls back to nopLog() so interface
	// methods never panic. Production paths always set a real logger.
	log *zerolog.Logger
}

// NewContainer creates a new Container instance with the specified metadata.
//
// The resolved image name is cached so later ImageName calls do not recompute it.
//
// Parameters:
//   - log: Process logger. A nop logger is used when nil.
//   - containerInfo: Docker container metadata.
//   - imageInfo: Docker image metadata.
//
// Returns:
//   - *Container: Initialized container instance.
func NewContainer(
	log *zerolog.Logger,
	containerInfo *dockerContainer.InspectResponse,
	imageInfo *dockerImage.InspectResponse,
) *Container {
	if log == nil {
		log = nopLog()
	}

	name := ""
	if containerInfo != nil {
		name = containerInfo.Name
	}
	// Initialize with default state.
	c := &Container{
		LinkedToRestarting: false,
		Stale:              false,
		OldImageID:         "",
		normalizedName:     NormalizeContainerName(name),
		containerInfo:      containerInfo,
		imageInfo:          imageInfo,
		log:                log,
	}
	// Cache the resolved image name at construct time.
	c.imageName = c.resolveImageName()
	c.Logger().Debug().
		Str("container", c.Name()).
		Str("id", c.ID().ShortID()).
		Str("image", string(c.ImageID())).
		Msg("Created new container instance")

	return c
}

// IsLinkedToRestarting returns whether the container is linked to a restarting container.
//
// Returns:
//   - bool: True if linked, false otherwise.
func (c *Container) IsLinkedToRestarting() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.LinkedToRestarting
}

// SetLinkedToRestarting sets the linked-to-restarting state.
//
// Parameters:
//   - value: New state value.
func (c *Container) SetLinkedToRestarting(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.LinkedToRestarting = value
}

// IsStale returns whether the container's image is outdated.
//
// Returns:
//   - bool: True if stale, false otherwise.
func (c *Container) IsStale() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Stale
}

// SetStale marks the container as having an outdated image.
//
// Parameters:
//   - value: New stale value.
func (c *Container) SetStale(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Stale = value
}

// SetOldImageID sets the old image ID for cleanup tracking.
//
// Parameters:
//   - id: The old image ID.
func (c *Container) SetOldImageID(id types.ImageID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.OldImageID = id
}

// ToRestart determines if the container should be restarted.
//
// Returns:
//   - bool: True if stale or linked to restarting, false otherwise.
func (c *Container) ToRestart() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Stale || c.LinkedToRestarting
}

// ContainerInfo returns the full Docker container metadata.
//
// Returns:
//   - *dockerContainer.InspectResponse: Container metadata.
func (c *Container) ContainerInfo() *dockerContainer.InspectResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.containerInfo
}

// ID returns the unique identifier of the container.
//
// Returns:
//   - types.ContainerID: Container ID.
func (c *Container) ID() types.ContainerID {
	info := c.ContainerInfo()

	if info == nil {
		return ""
	}

	return types.ContainerID(info.ID)
}

// IsRunning checks if the container is currently running.
//
// Returns:
//   - bool: True if running, false otherwise.
func (c *Container) IsRunning() bool {
	info := c.ContainerInfo()

	if info == nil || info.State == nil {
		return false
	}

	return info.State.Running
}

// IsRestarting checks if the container is currently restarting.
//
// Returns:
//   - bool: True if restarting, false otherwise.
func (c *Container) IsRestarting() bool {
	info := c.ContainerInfo()

	if info == nil || info.State == nil {
		return false
	}

	return info.State.Restarting
}

// IsCreated checks if the container is in the Docker "created" state,
// meaning it was successfully created but never started.
//
// Returns:
//   - bool: True if the container is in created state, false otherwise.
func (c *Container) IsCreated() bool {
	info := c.ContainerInfo()

	if info == nil || info.State == nil {
		return false
	}

	return info.State.Status == dockerContainer.StateCreated
}

// Name returns the normalized name of the container.
//
// Returns:
//   - string: Normalized container name.
func (c *Container) Name() string {
	return c.normalizedName
}

// ImageID returns the ID of the container's image.
//
// Returns:
//   - types.ImageID: Image ID or empty string if imageInfo is nil.
func (c *Container) ImageID() types.ImageID {
	if c.imageInfo == nil {
		return ""
	}

	return types.ImageID(c.imageInfo.ID)
}

// ImageName returns the cached name of the container's image.
//
// The value is resolved once in NewContainer from the Zodiac label or Config.Image.
// When containerInfo is later cleared, the name is resolved again.
// Callers that already hold c.mu must use imageNameLocked.
//
// Returns:
//   - string: Image name (e.g., "alpine:latest").
func (c *Container) ImageName() string {
	if c == nil {
		return "unknown:latest"
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.imageNameLocked()
}

// SetImageName updates Config.Image and the cached image name used by ImageName.
//
// An untagged name receives a ":latest" suffix so ImageName stays consistent
// with resolveImageName. Registry host ports are not treated as tags.
//
// Parameters:
//   - name: Image reference to store.
func (c *Container) SetImageName(name string) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	normalized := ensureImageTag(name)

	// Keep Config.Image in sync so GetCreateConfig uses the pinned reference.
	if c.containerInfo != nil && c.containerInfo.Config != nil {
		c.replaceInspectLocked(func(info *dockerContainer.InspectResponse) {
			info.Config.Image = normalized
		})
	}

	c.imageName = normalized
}

// HasImageInfo indicates whether image metadata is available.
//
// Returns:
//   - bool: True if imageInfo is non-nil, false otherwise.
func (c *Container) HasImageInfo() bool {
	return c.imageInfo != nil
}

// ImageInfo returns the Docker image metadata.
//
// Returns:
//   - *dockerImage.InspectResponse: Image metadata or nil if unavailable.
func (c *Container) ImageInfo() *dockerImage.InspectResponse {
	return c.imageInfo
}

// Logger returns the container's process logger, or a discarded nop if unset.
func (c *Container) Logger() *zerolog.Logger {
	if c != nil && c.log != nil {
		return c.log
	}

	return nopLog()
}

// replaceInspectLocked replaces the container's inspect data with a copy that
// change modifies. Inspect data returned earlier by ContainerInfo is never
// changed, so callers can read it without holding the lock. change receives
// copies of Config and HostConfig, and must copy any map or slice in them
// before changing it. The caller must hold c.mu for writing, and the inspect
// data must not be nil.
//
// Parameters:
//   - change: Modifies the copy of the inspect data.
func (c *Container) replaceInspectLocked(change func(info *dockerContainer.InspectResponse)) {
	info := *c.containerInfo

	if info.Config != nil {
		config := *info.Config
		info.Config = &config
	}

	if info.HostConfig != nil {
		hostConfig := *info.HostConfig
		info.HostConfig = &hostConfig
	}

	change(&info)
	c.containerInfo = &info
}

// imageNameLocked returns the cached image name. The caller must hold c.mu.
func (c *Container) imageNameLocked() string {
	// Re-resolve when inspect data was cleared after construction.
	if c.containerInfo == nil {
		return c.resolveImageName()
	}

	return c.imageName
}

// resolveImageName computes the image name from the Zodiac label or Config.Image.
//
// An untagged name receives a ":latest" suffix. Missing config yields "unknown:latest".
//
// Returns:
//   - string: Image name with a tag (e.g., "alpine:latest").
func (c *Container) resolveImageName() string {
	// Prefer the Zodiac label for the image name.
	// The caller holds c.mu or is constructing the container, so the label
	// is read from the field rather than through ContainerInfo.
	imageName, ok := labelValue(c.containerInfo, labels.ZodiacOriginalImage)
	if !ok {
		if c.containerInfo == nil || c.containerInfo.Config == nil {
			c.Logger().Warn().
				Str("container", c.Name()).
				Msg("No container config available, using default image name")

			return "unknown:latest"
		}

		imageName = c.containerInfo.Config.Image
	}

	return ensureImageTag(imageName)
}

// ensureImageTag appends ":latest" when the reference has no tag.
//
// A colon in the registry host (for example "registry.example:5000/app") is
// not treated as a tag.
//
// Parameters:
//   - name: Image reference to normalize.
//
// Returns:
//   - string: Image name with a tag.
func ensureImageTag(name string) string {
	if name == "" {
		return "unknown:latest"
	}

	slash := strings.LastIndex(name, "/")
	if strings.Contains(name[slash+1:], ":") {
		return name
	}

	return name + ":latest"
}

// NormalizeContainerName trims the leading "/" from container names.
//
// Parameters:
//   - name: Container name, potentially with leading "/".
//
// Returns:
//   - string: Normalized name without leading "/".
func NormalizeContainerName(name string) string {
	return strings.TrimLeft(name, "/")
}

// IsOldContainer reports whether the container's runtime name (from Name()
// or raw inspect) indicates a predecessor renamed during Watchtower self-update.
// It trims any leading '/' (as Docker names have) and checks for the
// WatchtowerOldPrefix convention. Used for disambiguation in hostname
// fallback, forcing cleanup of old containers, skipping redundant renames,
// and suppressing notifs for self-cleanup of prior incarnations.
//
// Parameters:
//   - name: The container name (may be empty, with or without leading '/').
//
// Returns:
//   - bool: true if the (normalized) name has the old- prefix.
func IsOldContainer(name string) bool {
	if name == "" {
		return false
	}

	n := strings.TrimLeft(name, "/")

	return strings.HasPrefix(n, types.WatchtowerOldPrefix)
}
