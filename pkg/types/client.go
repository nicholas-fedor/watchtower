package types

import (
	"time"

	dockerContainer "github.com/docker/docker/api/types/container"
)

// Client defines the interface for interacting with the Docker API within Watchtower.
//
// It provides methods for managing containers, images, and executing commands, abstracting the underlying Docker client operations.
type Client interface {
	// ListContainers retrieves a filtered list of containers running on the host.
	//
	// The provided filter determines which containers are included in the result.
	ListContainers(filter Filter) ([]Container, error)

	// GetContainer fetches detailed information about a specific container by its ID.
	//
	// Returns the container object or an error if the container cannot be retrieved.
	GetContainer(containerID ContainerID) (Container, error)

	// StopContainer stops and removes a specified container, respecting the given timeout.
	//
	// It ensures the container is no longer running or present on the host.
	StopContainer(container Container, timeout time.Duration) error

	// StartContainer creates and starts a new container based on the provided container's configuration.
	//
	// Returns the new container's ID or an error if creation or startup fails.
	StartContainer(container Container) (ContainerID, error)

	// RenameContainer renames an existing container to the specified new name.
	//
	// Returns an error if the rename operation fails.
	RenameContainer(container Container, newName string) error

	// IsContainerStale checks if a container's image is outdated compared to the latest available version.
	//
	// Returns whether the container is stale, the latest image ID, and any error encountered.
	IsContainerStale(
		container Container,
		params UpdateParams,
	) (bool, ImageID, error)

	// ExecuteCommand runs a command inside a container and returns whether to skip updates based on the result.
	//
	// The timeout specifies how long to wait for the command to complete.
	// UID and GID specify the user to run the command as, defaulting to container's configured user.
	ExecuteCommand(
		container Container,
		command string,
		timeout int,
		uid int,
		gid int,
	) (bool, error)

	// RemoveImageByID deletes an image from the Docker host by its ID.
	//
	// Parameters:
	//   - imageID: ID of the image to remove.
	//   - imageName: Name of the image to remove (for logging purposes).
	//
	// Returns an error if the removal fails.
	RemoveImageByID(imageID ImageID, imageName string) error

	// WarnOnHeadPullFailed determines whether to log a warning when a HEAD request fails during image pulls.
	//
	// The decision is based on the configured warning strategy and container context.
	WarnOnHeadPullFailed(container Container) bool

	// GetVersion returns the client's API version.
	GetVersion() string

	// GetInfo returns system information from the Docker daemon.
	//
	// Returns system-wide information about the Docker installation.
	GetInfo() (SystemInfo, error)

	// GetServerVersion returns version information from the Docker daemon.
	//
	// Returns detailed version information about the Docker server.
	GetServerVersion() (VersionInfo, error)

	// GetDiskUsage returns disk usage information from the Docker daemon.
	//
	// Returns comprehensive disk usage statistics for images, containers, and volumes.
	GetDiskUsage() (DiskUsage, error)

	// GetTotalDiskUsage returns the total disk usage from the Docker daemon.
	//
	// Returns the total size of layers, containers, volumes, and build cache.
	GetTotalDiskUsage() (int64, error)

	// WaitForContainerHealthy waits for a container to become healthy or times out.
	//
	// It polls the container's health status until it reports "healthy" or the timeout is reached.
	// If the container has no health check configured, it returns immediately.
	WaitForContainerHealthy(containerID ContainerID, timeout time.Duration) error

	// ListAllContainers retrieves a list of all containers from the Docker host, regardless of status.
	//
	// Returns all containers without filtering by status or other criteria.
	ListAllContainers() ([]Container, error)

	// UpdateContainer updates the configuration of an existing container.
	//
	// It modifies container settings such as restart policy using the Docker API ContainerUpdate.
	UpdateContainer(container Container, config dockerContainer.UpdateConfig) error
}

// SystemInfo represents system information from the Docker daemon.
type SystemInfo struct {
	Name            string          `json:"name"`
	ServerVersion   string          `json:"server_version"`
	OSType          string          `json:"os_type"`
	OperatingSystem string          `json:"operating_system"`
	Driver          string          `json:"driver"`
	RegistryConfig  *RegistryConfig `json:"registry_config,omitempty"`
}

// VersionInfo represents version information from the Docker daemon.
type VersionInfo struct {
	Version       string `json:"version"`
	APIVersion    string `json:"api_version"`
	MinAPIVersion string `json:"min_api_version"`
	GitCommit     string `json:"git_commit"`
	GoVersion     string `json:"go_version"`
	Os            string `json:"os"`
	Arch          string `json:"arch"`
	KernelVersion string `json:"kernel_version"`
	Experimental  bool   `json:"experimental"`
	BuildTime     string `json:"build_time"`
}

// RegistryConfig represents registry configuration from the Docker daemon.
type RegistryConfig struct {
	Mirrors               []string            `json:"mirrors,omitempty"`
	InsecureRegistryCIDRs []string            `json:"insecure_registry_cidrs,omitempty"`
	Registries            map[string][]string `json:"registries,omitempty"` // Per-registry mirrors: registry hostname -> list of mirrors
}

// DiskUsage represents disk usage information from the Docker daemon.
type DiskUsage struct {
	LayersSize int64              `json:"layers_size"`
	Images     []ImageSummary     `json:"images,omitempty"`
	Containers []ContainerSummary `json:"containers,omitempty"`
	Volumes    []VolumeSummary    `json:"volumes,omitempty"`
	BuildCache []BuildCache       `json:"build_cache,omitempty"`
}

// ImageSummary represents summary information about a Docker image.
type ImageSummary struct {
	ID          string            `json:"id"`
	ParentID    string            `json:"parent_id,omitempty"`
	RepoTags    []string          `json:"repo_tags,omitempty"`
	RepoDigests []string          `json:"repo_digests,omitempty"`
	Created     int64             `json:"created"`
	Size        int64             `json:"size"`
	SharedSize  int64             `json:"shared_size"`
	VirtualSize int64             `json:"virtual_size"`
	Labels      map[string]string `json:"labels,omitempty"`
	Containers  int64             `json:"containers"`
}

// ContainerSummary represents summary information about a Docker container.
type ContainerSummary struct {
	ID         string            `json:"id"`
	Names      []string          `json:"names,omitempty"`
	Image      string            `json:"image"`
	ImageID    string            `json:"image_id"`
	Command    string            `json:"command"`
	Created    int64             `json:"created"`
	Ports      []Port            `json:"ports,omitempty"`
	SizeRw     int64             `json:"size_rw,omitempty"`
	SizeRootFs int64             `json:"size_root_fs,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	State      string            `json:"state"`
	Status     string            `json:"status"`
}

// VolumeSummary represents summary information about a Docker volume.
type VolumeSummary struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	CreatedAt  string            `json:"created_at,omitempty"`
	Status     map[string]any    `json:"status,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	Scope      string            `json:"scope"`
	Options    map[string]string `json:"options,omitempty"`
	UsageData  *VolumeUsageData  `json:"usage_data,omitempty"`
}

// VolumeUsageData represents usage data for a Docker volume.
type VolumeUsageData struct {
	Size     int64 `json:"size"`
	RefCount int64 `json:"ref_count"`
}

// BuildCache represents a Docker build cache entry.
type BuildCache struct {
	ID          string   `json:"id"`
	Parents     []string `json:"parents,omitempty"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	InUse       bool     `json:"in_use"`
	Shared      bool     `json:"shared"`
	Size        int64    `json:"size"`
	CreatedAt   string   `json:"created_at"`
	LastUsedAt  string   `json:"last_used_at"`
	UsageCount  int64    `json:"usage_count"`
}

// Port represents a Docker container port mapping.
type Port struct {
	IP          string `json:"ip,omitempty"`
	PrivatePort uint16 `json:"private_port"`
	PublicPort  uint16 `json:"public_port,omitempty"`
	Type        string `json:"type"`
}
