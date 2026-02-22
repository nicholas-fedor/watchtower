package container

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/afero"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/docker/docker/api/types/container"
	dockerClient "github.com/docker/docker/client"

	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/pkg/registry"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Constants for CPUCopyMode values.
const (
	// CPUCopyModeAuto indicates automatic detection of container runtime for CPU copying behavior.
	CPUCopyModeAuto = "auto"

	// DaemonInitTimeout is the default timeout for Docker daemon initialization operations.
	// This timeout applies to initial connection, ping, API version negotiation, and
	// server version retrieval during client initialization.
	// The value is 30 seconds, which should be sufficient for most Docker daemon
	// initialization scenarios while preventing indefinite hangs.
	DaemonInitTimeout = 30 * time.Second
)

// Errors for container health operations.
var (
	// errHealthCheckTimeout indicates that waiting for a container to become healthy timed out.
	errHealthCheckTimeout = errors.New("timeout waiting for container to become healthy")
	// errHealthCheckFailed indicates that a container's health check failed.
	errHealthCheckFailed = errors.New("container health check failed")
)

// Client defines the interface for interacting with the Docker API within Watchtower.
//
// It provides methods for managing containers, images, and executing commands, abstracting the underlying Docker client operations.
type Client interface {
	// ListContainers retrieves a list of containers, optionally filtered.
	//
	// If no filters are provided, all containers are returned.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - filter: Optional filters to apply to container list. Multiple filters are combined with logical AND.
	//
	// Returns:
	//   - []types.Container: List of matching containers.
	//   - error: Non-nil if listing fails, nil on success.
	ListContainers(ctx context.Context, filter ...types.Filter) ([]types.Container, error)

	// GetContainer fetches detailed information about a specific container by its ID.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - containerID: ID of the container to retrieve.
	//
	// Returns:
	//   - types.Container: Container details if found.
	//   - error: Non-nil if retrieval fails, nil on success.
	GetContainer(ctx context.Context, containerID types.ContainerID) (types.Container, error)

	// GetCurrentWatchtowerContainer retrieves minimal container information for the specified container ID, skipping image inspection.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - containerID: ID of the container to retrieve.
	//
	// Returns:
	//   - types.Container: Container with imageInfo set to nil.
	//   - error: Non-nil if inspection fails, nil on success.
	GetCurrentWatchtowerContainer(ctx context.Context, containerID types.ContainerID) (types.Container, error)

	// StopContainer stops a specified container, respecting the given timeout.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - container: Container to stop.
	//   - timeout: Duration to wait before forcing stop.
	//
	// Returns:
	//   - error: Non-nil if stop fails, nil on success.
	StopContainer(ctx context.Context, container types.Container, timeout time.Duration) error

	// StopAndRemoveContainer stops and removes a specified container, respecting the given timeout.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - container: Container to stop and remove.
	//   - timeout: Duration to wait before forcing stop.
	//
	// Returns:
	//   - error: Non-nil if stop/removal fails, nil on success.
	StopAndRemoveContainer(ctx context.Context, container types.Container, timeout time.Duration) error

	// StartContainer creates and starts a new container based on the provided container's configuration.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - container: Source container to replicate.
	//
	// Returns:
	//   - types.ContainerID: ID of the new container.
	//   - error: Non-nil if creation/start fails, nil on success.
	StartContainer(ctx context.Context, container types.Container) (types.ContainerID, error)

	// RenameContainer renames an existing container to the specified new name.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - container: Container to rename.
	//   - newName: New name for the container.
	//
	// Returns:
	//   - error: Non-nil if rename fails, nil on success.
	RenameContainer(ctx context.Context, container types.Container, newName string) error

	// IsContainerStale checks if a container's image is outdated compared to the latest available version.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - container: Container to check.
	//   - params: Update parameters for staleness check.
	//
	// Returns:
	//   - bool: True if image is stale, false otherwise.
	//   - types.ImageID: Latest image ID.
	//   - error: Non-nil if check fails, nil on success.
	IsContainerStale(
		ctx context.Context,
		container types.Container,
		params types.UpdateParams,
	) (bool, types.ImageID, error)

	// ExecuteCommand runs a command inside a container and returns whether to skip updates based on the result.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - container: Container to execute command in.
	//   - command: Command to execute.
	//   - timeout: Minutes to wait before timeout (0 for no timeout).
	//   - uid: UID to run command as (-1 to use container default).
	//   - gid: GID to run command as (-1 to use container default).
	//
	// Returns:
	//   - bool: True if updates should be skipped, false otherwise.
	//   - error: Non-nil if execution fails, nil on success.
	ExecuteCommand(
		ctx context.Context,
		container types.Container,
		command string,
		timeout int,
		uid int,
		gid int,
	) (bool, error)

	// RemoveImageByID deletes an image from the Docker host by its ID.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - imageID: ID of the image to remove.
	//   - imageName: Name of the image to remove (for logging purposes).
	//
	// Returns:
	//   - error: Non-nil if removal fails, nil on success.
	RemoveImageByID(ctx context.Context, imageID types.ImageID, imageName string) error

	// WarnOnHeadPullFailed determines whether to log a warning when a HEAD request fails during image pulls.
	//
	// The decision is based on the configured warning strategy and container context.
	//
	// Parameters:
	//   - container: Container to evaluate.
	//
	// Returns:
	//   - bool: True if warning is needed, false otherwise.
	WarnOnHeadPullFailed(container types.Container) bool

	// GetVersion returns the client's API version.
	//
	// Returns:
	//   - string: Docker API version (e.g., "1.44").
	GetVersion() string

	// GetInfo returns system information from the Docker daemon.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//
	// Returns:
	//   - map[string]any: System information.
	//   - error: Non-nil if retrieval fails, nil on success.
	GetInfo(ctx context.Context) (map[string]any, error)

	// WaitForContainerHealthy waits for a container to become healthy or times out.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - containerID: ID of the container to wait for.
	//   - timeout: Maximum duration to wait for health.
	//
	// Returns:
	//   - error: Non-nil if timeout is reached or inspection fails, nil if healthy or no health check.
	WaitForContainerHealthy(ctx context.Context, containerID types.ContainerID, timeout time.Duration) error

	// UpdateContainer updates the configuration of an existing container.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - container: Container to update.
	//   - config: Update configuration containing the changes to apply.
	//
	// Returns:
	//   - error: Non-nil if update fails, nil on success.
	UpdateContainer(ctx context.Context, container types.Container, config dockerContainer.UpdateConfig) error

	// RemoveContainer removes a container from the Docker host.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - container: Container to remove.
	//
	// Returns:
	//   - error: Non-nil if removal fails, nil on success.
	RemoveContainer(ctx context.Context, container types.Container) error
}

// client is the concrete implementation of the Client interface.
//
// It wraps the Docker API client and applies custom behavior via ClientOptions.
type client struct {
	ClientOptions

	api dockerClient.APIClient
}

// ClientOptions configures the behavior of the dockerClient wrapper around the Docker API.
//
// It controls container management and warning behaviors.
type ClientOptions struct {
	RemoveVolumes           bool
	IncludeStopped          bool
	ReviveStopped           bool
	IncludeRestarting       bool
	DisableMemorySwappiness bool
	CPUCopyMode             string
	WarnOnHeadFailed        WarningStrategy
	Fs                      afero.Fs
}

// NewClient initializes a new Client instance for Docker API interactions.
//
// It configures the client using environment variables (e.g., DOCKER_HOST, DOCKER_API_VERSION) and validates the API version, falling back to autonegotiation if necessary.
//
// Parameters:
//   - opts: Options to customize container management behavior.
//
// Returns:
//   - Client: Initialized client instance (exits on failure).
func NewClient(opts ClientOptions) Client {
	// Use a timeout context for Docker daemon initialization to prevent indefinite hangs.
	ctx, cancel := context.WithTimeout(context.Background(), DaemonInitTimeout)
	defer cancel()

	// Initialize client with autonegotiation, ignoring DOCKER_API_VERSION initially.
	cli, err := dockerClient.NewClientWithOpts(
		dockerClient.FromEnv,
		dockerClient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to initialize Docker client")
	}
	// Set default filesystem if not provided
	if opts.Fs == nil {
		opts.Fs = afero.NewOsFs()
	}

	// Apply forced API version if set and valid.
	if version := strings.Trim(os.Getenv("DOCKER_API_VERSION"), "\""); version != "" {
		pingCli, err := dockerClient.NewClientWithOpts(
			dockerClient.FromEnv, // Include env config for TLS, etc.
			dockerClient.WithVersion(version),
		)
		if err != nil {
			logrus.WithError(err).Fatal("Failed to create test client")
		}

		_, err = pingCli.Ping(ctx)
		switch {
		case err == nil:
			// Ping succeeded: use the forced version client
			cli = pingCli
		case strings.Contains(err.Error(), "page not found"):
			logrus.WithFields(logrus.Fields{
				"version":  version,
				"error":    err,
				"endpoint": "/_ping",
			}).Warn("Invalid API version; falling back to autonegotiation")
			cli.NegotiateAPIVersion(ctx)
		default:
			// Other ping failure: fall back to negotiation (don't override with broken client)
			logrus.WithFields(logrus.Fields{
				"version":  version,
				"error":    err,
				"endpoint": "/_ping",
			}).Warn("Ping failed with non-version error; falling back to autonegotiation")
			cli.NegotiateAPIVersion(ctx)
		}
	} else {
		cli.NegotiateAPIVersion(ctx)
	}

	// Log client and server API versions.
	selectedVersion := cli.ClientVersion()

	serverVersion, err := cli.ServerVersion(ctx)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error":    err,
			"endpoint": "/version",
		}).Error("Failed to retrieve server version")
	} else {
		logrus.WithFields(logrus.Fields{
			"client_version": selectedVersion,
			"server_version": serverVersion.APIVersion,
		}).Debug("Initialized Docker client")
	}

	return &client{
		api:           cli,
		ClientOptions: opts,
	}
}

// ListContainers retrieves a list of containers, optionally filtered.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - filter: Optional filters to apply to container list. Multiple filters are combined with logical AND.
//
// Returns:
//   - []types.Container: List of matching containers.
//   - error: Non-nil if listing fails, nil on success.
func (c client) ListContainers(ctx context.Context, filter ...types.Filter) ([]types.Container, error) {
	// Determine if the container runtime is Podman to handle runtime-specific differences.
	isPodman := c.getPodmanFlag(ctx)

	var containerFilter types.Filter

	if len(filter) > 0 {
		if len(filter) == 1 {
			// Single filter: use it directly
			containerFilter = filter[0]
		} else {
			// Multiple filters: combine them with logical AND
			// A container must pass ALL filters to be included
			containerFilter = func(container types.FilterableContainer) bool {
				for _, f := range filter {
					if !f(container) {
						return false
					}
				}

				return true
			}
		}
	}

	// Attempt to list source containers and handle errors by logging and returning them.
	containers, err := ListSourceContainers(ctx, c.api, c.ClientOptions, containerFilter, isPodman)
	if err != nil {
		logrus.WithError(err).Debug("Failed to list containers")

		return nil, err
	}

	logrus.WithField("count", len(containers)).Debug("Listed containers")

	return containers, nil
}

// GetContainer fetches detailed information about a specific container by its ID.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - containerID: ID of the container to retrieve.
//
// Returns:
//   - types.Container: Container details if found.
//   - error: Non-nil if retrieval fails, nil on success.
func (c client) GetContainer(ctx context.Context, containerID types.ContainerID) (types.Container, error) {
	// Retrieve container details using helper function.
	container, err := GetSourceContainer(ctx, c.api, containerID)
	if err != nil {
		logrus.WithError(err).
			WithField("container_id", containerID).
			Debug("Failed to get container")

		return nil, err
	}

	logrus.WithField("container_id", containerID).Debug("Retrieved container details")

	return container, nil
}

// GetCurrentWatchtowerContainer retrieves container information for the specified container ID, skipping image inspection.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - containerID: ID of the container to retrieve.
//
// Returns:
//   - types.Container: Container with imageInfo set to nil.
//   - error: Non-nil if inspection fails, nil on success.
func (c client) GetCurrentWatchtowerContainer(
	ctx context.Context,
	containerID types.ContainerID,
) (types.Container, error) {
	clog := logrus.WithField("container_id", containerID)

	clog.Debug("Inspecting current Watchtower container")

	containerInfo, err := c.api.ContainerInspect(ctx, string(containerID))
	if err != nil {
		clog.WithError(err).Debug("Failed to inspect current Watchtower container")

		return nil, fmt.Errorf("%w: %w", errInspectContainerFailed, err)
	}

	clog.Debug("Retrieved minimal container info")

	return NewContainer(&containerInfo, nil), nil
}

// StopContainer stops a specified container.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - container: Container to stop.
//   - timeout: Duration to wait before forcing stop.
//
// Returns:
//   - error: Non-nil if stop fails, nil on success.
func (c client) StopContainer(ctx context.Context, container types.Container, timeout time.Duration) error {
	// Stop container using helper function.
	err := StopSourceContainer(ctx, c.api, container, timeout)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"container": container.Name(),
			"image":     container.ImageName(),
		}).Debug("Failed to stop container")

		return err
	}

	logrus.WithFields(logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}).Debug("Stopped container")

	return nil
}

// StopAndRemoveContainer stops and removes a specified container.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - container: Container to stop and remove.
//   - timeout: Duration to wait before forcing stop.
//
// Returns:
//   - error: Non-nil if stop/removal fails, nil on success.
func (c client) StopAndRemoveContainer(ctx context.Context, container types.Container, timeout time.Duration) error {
	// Stop and remove container using helper function with volume option.
	err := StopAndRemoveSourceContainer(ctx, c.api, container, timeout, c.RemoveVolumes)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"container": container.Name(),
			"image":     container.ImageName(),
		}).Debug("Failed to stop and remove container")

		return err
	}

	logrus.WithFields(logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}).Debug("Stopped and removed container")

	return nil
}

// StartContainer creates and starts a new container based on an existing container's configuration.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - container: Source container to replicate.
//
// Returns:
//   - types.ContainerID: ID of the new container.
//   - error: Non-nil if creation/start fails, nil on success.
func (c client) StartContainer(ctx context.Context, container types.Container) (types.ContainerID, error) {
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}
	// Determine if the container runtime is Podman to handle runtime-specific differences.
	isPodman := c.getPodmanFlag(ctx)

	clientVersion := c.GetVersion()

	logrus.WithFields(fields).WithField("client_version", clientVersion).
		Debug("Obtaining source container network configuration")

	// Get unified network config.
	networkConfig := getNetworkConfig(container, clientVersion)

	// Start new container with selected config.
	newID, err := StartTargetContainer(
		ctx,
		c.api,
		container,
		networkConfig,
		c.ReviveStopped,
		clientVersion,
		flags.DockerAPIMinVersion, // Docker API Version 1.24
		c.DisableMemorySwappiness,
		c.CPUCopyMode,
		isPodman,
	)
	if err != nil {
		logrus.WithFields(fields).WithError(err).Debug("Failed to start new container")

		return "", err
	}

	logrus.WithFields(fields).
		WithField("new_id", newID.ShortID()).
		Debug("Started new container")

	return newID, nil
}

// UpdateContainer updates the configuration of an existing container.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - container: Container to update.
//   - config: Update configuration containing the changes to apply.
//
// Returns:
//   - error: Non-nil if update fails, nil on success.
func (c client) UpdateContainer(
	ctx context.Context,
	container types.Container,
	config dockerContainer.UpdateConfig,
) error {
	clog := logrus.WithField("container_id", container.ID())

	clog.Debug("Updating container configuration")

	_, err := c.api.ContainerUpdate(ctx, string(container.ID()), config)
	if err != nil {
		clog.WithError(err).Debug("Failed to update container")

		return fmt.Errorf("failed to update container %s: %w", container.ID(), err)
	}

	clog.Debug("Container configuration updated")

	return nil
}

// RemoveContainer removes a container from the Docker host.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - container: Container to remove.
//
// Returns:
//   - error: Non-nil if removal fails, nil on success.
func (c client) RemoveContainer(ctx context.Context, container types.Container) error {
	clog := logrus.WithFields(logrus.Fields{
		"container": container.Name(),
		"id":        container.ID().ShortID(),
	})

	clog.Debug("Removing container")

	err := c.api.ContainerRemove(ctx, string(container.ID()), dockerContainer.RemoveOptions{
		Force: true,
	})
	if err != nil && !cerrdefs.IsNotFound(err) {
		clog.WithError(err).Debug("Failed to remove container")

		return fmt.Errorf("%w: %w", errRemoveContainerFailed, err)
	}

	if cerrdefs.IsNotFound(err) {
		clog.Debug("Container already removed")

		return nil
	}

	clog.Debug("Container removed")

	return nil
}

// RenameContainer renames an existing container to a new name.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - container: Container to rename.
//   - newName: New name for the container.
//
// Returns:
//   - error: Non-nil if rename fails, nil on success.
func (c client) RenameContainer(ctx context.Context, container types.Container, newName string) error {
	// Perform rename using helper function.
	err := RenameTargetContainer(ctx, c.api, container, newName)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"container": container.Name(),
			"new_name":  newName,
		}).Debug("Failed to rename container")

		return err
	}

	logrus.WithFields(logrus.Fields{
		"container": container.Name(),
		"new_name":  newName,
	}).Debug("Renamed container")

	return nil
}

// WarnOnHeadPullFailed decides whether to warn about failed HEAD requests.
//
// Parameters:
//   - container: Container to evaluate.
//
// Returns:
//   - bool: True if warning is needed, false otherwise.
func (c client) WarnOnHeadPullFailed(container types.Container) bool {
	// Apply warning strategy based on configuration.
	if c.WarnOnHeadFailed == WarnAlways {
		return true
	}

	if c.WarnOnHeadFailed == WarnNever {
		return false
	}

	// Delegate to registry logic for auto strategy.
	return registry.WarnOnAPIConsumption(container)
}

// IsContainerStale checks if a container’s image is outdated.
//
// Parameters:
//   - container: Container to check.
//   - params: Update parameters for staleness check.
//
// Returns:
//   - bool: True if stale, false otherwise.
//   - types.ImageID: Latest image ID.
//   - error: Non-nil if check fails, nil on success.
func (c client) IsContainerStale(
	ctx context.Context,
	container types.Container,
	params types.UpdateParams,
) (bool, types.ImageID, error) {
	// Use image client to perform staleness check.
	imgClient := newImageClient(c.api)

	stale, newestImage, err := imgClient.IsContainerStale(ctx, container, params, c.WarnOnHeadFailed)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"container": container.Name(),
			"image":     container.ImageName(),
		}).Debug("Failed to check container staleness")
	} else {
		logrus.WithFields(logrus.Fields{
			"container":    container.Name(),
			"image":        container.ImageName(),
			"stale":        stale,
			"newest_image": newestImage,
		}).Debug("Checked container staleness")
	}

	return stale, newestImage, err
}

// ExecuteCommand runs a command inside a container and evaluates its result.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - container: Container to execute command in.
//   - command: Command to execute.
//   - timeout: Minutes to wait before timeout (0 for no timeout).
//   - uid: UID to run command as (-1 to use container default).
//   - gid: GID to run command as (-1 to use container default).
//
// Returns:
//   - bool: True if updates should be skipped, false otherwise.
//   - error: Non-nil if execution fails, nil on success.
func (c client) ExecuteCommand(
	ctx context.Context,
	container types.Container,
	command string,
	timeout int,
	uid int,
	gid int,
) (bool, error) {
	clog := logrus.WithField("container_id", container.ID())

	// Generate JSON metadata for the container.
	metadataJSON, err := generateContainerMetadata(container)
	if err != nil {
		clog.WithError(err).Debug("Failed to generate container metadata")

		return false, err
	}

	// Set User if UID or GID are specified (non-zero).
	var user string

	switch {
	case uid > 0 && gid > 0:
		user = fmt.Sprintf("%d:%d", uid, gid)
	case uid > 0:
		user = strconv.Itoa(uid)
	case gid > 0:
		user = fmt.Sprintf(":%d", gid)
	}

	if user != "" {
		clog.WithField("user", user).Debug("Setting exec user")
	}

	// Set up exec configuration with command and metadata.
	clog.WithField("command", command).Debug("Creating exec instance")
	execConfig := dockerContainer.ExecOptions{
		Tty:    true,
		Detach: true,
		Cmd:    []string{"sh", "-c", command},
		Env:    []string{"WT_CONTAINER=" + metadataJSON},
		User:   user,
	}

	// Create the exec instance using the parent context.
	// Timeout management is handled by waitForExecOrTimeout.
	exec, err := c.api.ContainerExecCreate(ctx, string(container.ID()), execConfig)
	if err != nil {
		clog.WithError(err).Debug("Failed to create exec instance")

		return false, fmt.Errorf("%w: %w", errCreateExecFailed, err)
	}

	// Start the exec instance.
	clog.WithField("exec_id", exec.ID).Debug("Starting exec instance")

	execStartCheck := dockerContainer.ExecStartOptions{Tty: true}

	err = c.api.ContainerExecStart(ctx, exec.ID, execStartCheck)
	if err != nil {
		clog.WithError(err).Debug("Failed to start exec instance")

		return false, fmt.Errorf("%w: %w", errStartExecFailed, err)
	}

	// Capture output and handle attachment.
	output, err := c.captureExecOutput(ctx, exec.ID)
	if err != nil {
		clog.WithError(err).Warn("Failed to capture command output")
	}

	// Wait for completion and evaluate result.
	skipUpdate, err := c.waitForExecOrTimeout(ctx, exec.ID, output, timeout)
	if err != nil {
		clog.WithError(err).Debug("Failed to inspect exec instance")

		return true, fmt.Errorf("%w: %w", errInspectExecFailed, err)
	}

	clog.WithFields(logrus.Fields{
		"command":     command,
		"output":      output,
		"skip_update": skipUpdate,
	}).Debug("Executed command")

	return skipUpdate, nil
}

// generateContainerMetadata creates a JSON-formatted string of container metadata.
//
// Parameters:
//   - container: Container object to extract metadata from.
//
// Returns:
//   - string: JSON string containing metadata (e.g., name, ID, image name, stop signal, labels).
//   - error: Non-nil if JSON marshaling fails, nil otherwise.
func generateContainerMetadata(container types.Container) (string, error) {
	// Filter Watchtower-specific labels to reduce JSON size
	labels := make(map[string]string)

	if containerInfo := container.ContainerInfo(); containerInfo != nil &&
		containerInfo.Config != nil {
		for key, value := range containerInfo.Config.Labels {
			if strings.HasPrefix(key, "com.centurylinklabs.watchtower.") {
				labels[key] = value
			}
		}
	}

	metadata := struct {
		Name       string            `json:"name"`
		ID         string            `json:"id"`
		ImageName  string            `json:"image_name"`
		StopSignal string            `json:"stop_signal"`
		Labels     map[string]string `json:"labels"`
	}{
		Name:       container.Name(),
		ID:         string(container.ID()),
		ImageName:  container.ImageName(),
		StopSignal: container.StopSignal(),
		Labels:     labels,
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("failed to marshal container metadata: %w", err)
	}

	return string(metadataJSON), nil
}

// RemoveImageByID deletes an image from the Docker host by its ID.
//
// Parameters:
//   - imageID: ID of the image to remove.
//   - imageName: Name of the image to remove (for logging purposes).
//
// Returns:
//   - error: Non-nil if removal fails, nil on success.
func (c client) RemoveImageByID(
	ctx context.Context,
	imageID types.ImageID,
	imageName string,
) error {
	// Use image client to remove the image.
	imgClient := newImageClient(c.api)

	err := imgClient.RemoveImageByID(ctx, imageID, imageName)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"image_id":   imageID,
			"image_name": imageName,
		}).Debug("Failed to remove image")

		return err
	}

	logrus.WithFields(logrus.Fields{
		"image_id":   imageID.ShortID(),
		"image_name": imageName,
	}).Debug("Cleaned up old image")

	return nil
}

// GetVersion returns the client’s API version.
//
// Returns:
//   - string: Docker API version (e.g., "1.44").
func (c client) GetVersion() string {
	return strings.Trim(c.api.ClientVersion(), "\"")
}

// GetInfo returns system information from the Docker daemon.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//
// Returns:
//   - map[string]interface{}: System information.
//   - error: Non-nil if retrieval fails, nil on success.
func (c client) GetInfo(ctx context.Context) (map[string]any, error) {
	info, err := c.api.Info(ctx)
	if err != nil {
		logrus.WithError(err).Debug("Failed to get system info")

		return nil, fmt.Errorf("failed to get system info: %w", err)
	}

	// Convert to map for easier access
	infoMap := map[string]any{
		"Name":            info.Name,
		"ServerVersion":   info.ServerVersion,
		"OSType":          info.OSType,
		"OperatingSystem": info.OperatingSystem,
		"Driver":          info.Driver,
	}

	return infoMap, nil
}

// WaitForContainerHealthy waits for a container to become healthy or times out.
//
// Parameters:
//   - containerID: ID of the container to wait for.
//   - timeout: Maximum duration to wait for health.
//
// Returns:
//   - error: Non-nil if timeout is reached or inspection fails, nil if healthy or no health check.
func (c client) WaitForContainerHealthy(
	ctx context.Context,
	containerID types.ContainerID,
	timeout time.Duration,
) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	clog := logrus.WithField("container_id", containerID)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			clog.Warn("Timeout waiting for container to become healthy")

			return fmt.Errorf("%w: %s", errHealthCheckTimeout, containerID)
		case <-ticker.C:
			// Inspect the container to check health status
			inspect, err := c.api.ContainerInspect(ctx, string(containerID))
			if err != nil {
				clog.WithError(err).Debug("Failed to inspect container for health check")

				return fmt.Errorf("failed to inspect container %s: %w", containerID, err)
			}

			// Check if health check is configured
			if inspect.State == nil || inspect.State.Health == nil {
				clog.Debug("No health check configured for container, proceeding")

				return nil
			}

			status := inspect.State.Health.Status
			clog.WithField("health_status", status).Debug("Checked container health status")

			if status == "healthy" {
				clog.Debug("Container is now healthy")

				return nil
			}

			if status == "unhealthy" {
				clog.Warn("Container health check failed")

				return fmt.Errorf("%w: %s", errHealthCheckFailed, containerID)
			}

			// Continue polling for "starting" or other statuses
		}
	}
}

// detectPodman determines if the container runtime is Podman using multiple detection methods.
//
// It iterates through detection helpers in priority order, returning as soon as one
// helper indicates Podman/Docker or an error occurs.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//
// Returns:
//   - bool: True if Podman is detected, false otherwise.
//   - error: Non-nil if detection fails, nil on success.
func (c client) detectPodman(ctx context.Context) (bool, error) {
	// Priority 1: Check for marker files (Podman or Docker)
	isPodman, err := c.detectPodmanByMarker()
	if err != nil {
		return false, err
	}

	if isPodman {
		return true, nil
	}

	// If marker indicates Docker, return false (not Podman)
	// Otherwise continue to next detection method
	gotResult, _ := c.checkDockerMarker()
	if gotResult {
		return false, nil
	}

	// Priority 2: Check CONTAINER environment variable
	if c.detectPodmanByEnv() {
		return true, nil
	}

	// Priority 3: API-based detection
	return c.detectPodmanByAPI(ctx)
}

// detectPodmanByMarker checks for container runtime marker files.
//
// It first checks for Podman's marker file, then checks for Docker's marker file.
// The function returns (true, nil) if Podman is detected, (false, nil) if Docker
// is detected, and (false, error) if neither marker file exists.
//
// Parameters:
//   - c: The client instance for filesystem access.
//
// Returns:
//   - bool: True if Podman marker found, false if Docker marker found or no marker found.
//   - error: Non-nil if checking fails, nil on success.
func (c client) detectPodmanByMarker() (bool, error) {
	// Check for Podman marker file
	_, err := c.Fs.Stat("/run/.containerenv")
	if err == nil {
		logrus.Debug("Detected Podman via marker file /run/.containerenv")

		return true, nil
	}

	// Check for Docker marker file
	hasDockerMarker, dockerErr := c.checkDockerMarker()
	if dockerErr != nil {
		return false, dockerErr
	}

	if hasDockerMarker {
		logrus.Debug("Detected Docker via marker file /.dockerenv")

		return false, nil
	}

	// No marker files found
	return false, nil
}

// checkDockerMarker checks for Docker's marker file.
//
// Parameters:
//   - c: The client instance for filesystem access.
//
// Returns:
//   - bool: True if Docker marker file exists.
//   - error: Error if stat fails (excluding file not found).
func (c client) checkDockerMarker() (bool, error) {
	_, err := c.Fs.Stat("/.dockerenv")
	if err == nil {
		return true, nil
	}

	if os.IsNotExist(err) {
		return false, nil
	}

	return false, fmt.Errorf("failed to check Docker marker file: %w", err)
}

// detectPodmanByEnv checks the CONTAINER environment variable for Podman indicators.
//
// It checks if the CONTAINER environment variable is set to "podman" or "oci",
// both of which indicate Podman is the container runtime.
//
// Returns:
//   - bool: True if CONTAINER env var indicates Podman, false otherwise.
func (c client) detectPodmanByEnv() bool {
	container := os.Getenv("CONTAINER")
	if container == "podman" || container == "oci" {
		logrus.Debug("Detected Podman via CONTAINER environment variable")

		return true
	}

	return false
}

// detectPodmanByAPI uses the Docker API to detect if the runtime is Podman.
//
// It queries the system info endpoint and checks the Name and ServerVersion
// fields for Podman indicators.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - c: The client instance for API calls.
//
// Returns:
//   - bool: True if Podman is detected via API, false otherwise.
//   - error: Non-nil if API call fails, nil on success.
func (c client) detectPodmanByAPI(ctx context.Context) (bool, error) {
	info, err := c.GetInfo(ctx)
	if err != nil {
		logrus.WithError(err).
			Debug("Failed to get system info for Podman detection, assuming Docker")

		return false, err
	}

	// Check Name field
	if name, exists := info["Name"]; exists && name == "podman" {
		logrus.Debug("Detected Podman via API Name field")

		return true, nil
	}

	// Check ServerVersion field
	if serverVersion, exists := info["ServerVersion"]; exists {
		if sv, ok := serverVersion.(string); ok && strings.Contains(strings.ToLower(sv), "podman") {
			logrus.Debug("Detected Podman via API ServerVersion field")

			return true, nil
		}
	}

	logrus.Debug("No Podman detection criteria met, assuming Docker")

	return false, nil
}

// getPodmanFlag determines if Podman detection is needed and performs it.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//
// Returns:
//   - bool: True if Podman is detected, false otherwise.
func (c client) getPodmanFlag(ctx context.Context) bool {
	// Only perform detection in auto mode; otherwise, assume Docker
	if c.CPUCopyMode != CPUCopyModeAuto {
		return false
	}

	// Attempt to detect Podman using various methods (marker files, env vars, API info)
	isPodman, err := c.detectPodman(ctx)
	if err != nil {
		// On detection failure, fall back to assuming Docker
		logrus.WithError(err).Debug("Failed to detect container runtime, falling back to Docker")

		return false
	}

	// Return the detection result
	return isPodman
}

// captureExecOutput attaches to an exec instance and captures its output.
//
// Parameters:
//   - ctx: Context for lifecycle control.
//   - execID: ID of the exec instance.
//
// Returns:
//   - string: Captured output if successful.
//   - error: Non-nil if attachment or reading fails, nil on success.
func (c client) captureExecOutput(ctx context.Context, execID string) (string, error) {
	clog := logrus.WithField("exec_id", execID)

	// Attach to the exec instance for output.
	clog.Debug("Attaching to exec instance")

	response, err := c.api.ContainerExecAttach(
		ctx,
		execID,
		dockerContainer.ExecStartOptions{Tty: true},
	)
	if err != nil {
		clog.WithError(err).Debug("Failed to attach to exec instance")

		return "", fmt.Errorf("%w: %w", errAttachExecFailed, err)
	}

	defer response.Close()

	// Read output into a buffer with timeout.
	var writer bytes.Buffer

	done := make(chan error, 1)

	go func() {
		_, err := io.Copy(&writer, response.Reader)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			clog.WithError(err).Debug("Failed to read exec output")

			return "", fmt.Errorf("%w: %w", errReadExecOutputFailed, err)
		}
	case <-ctx.Done():
		response.Close()

		return "", fmt.Errorf("%w: %w", errReadExecOutputFailed, ctx.Err())
	}

	// Return trimmed output if any was captured.
	if writer.Len() > 0 {
		output := strings.TrimSpace(writer.String())
		clog.WithField("output", output).Debug("Captured exec output")

		return output, nil
	}

	return "", nil
}

// waitForExecOrTimeout waits for an exec instance to complete or times out.
//
// Parameters:
//   - ctx: Parent context.
//   - execID: ID of the exec instance.
//   - execOutput: Captured output for error reporting.
//   - timeout: Minutes to wait (0 for no timeout).
//
// Returns:
//   - bool: True if updates should be skipped (exit code 75), false otherwise.
//   - error: Non-nil if inspection fails or command errors, nil on success.
func (c client) waitForExecOrTimeout(
	ctx context.Context,
	execID string,
	execOutput string,
	timeout int,
) (bool, error) {
	const ExTempFail = 75

	clog := logrus.WithField("exec_id", execID)

	var execCtx context.Context

	var cancel context.CancelFunc

	// Set up context with timeout if specified.
	if timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Minute)
		defer cancel()
	} else {
		execCtx = ctx
	}

	// Poll exec status until completion.
	for {
		execInspect, err := c.api.ContainerExecInspect(execCtx, execID)
		if err != nil {
			clog.WithError(err).Debug("Failed to inspect exec instance")

			return false, fmt.Errorf("%w: %w", errInspectExecFailed, err)
		}

		clog.WithFields(logrus.Fields{
			"exit_code": execInspect.ExitCode,
			"running":   execInspect.Running,
		}).Debug("Checked exec status")

		if execInspect.Running {
			select {
			case <-time.After(1 * time.Second):
				continue
			case <-execCtx.Done():
				return false, fmt.Errorf("exec canceled: %w", execCtx.Err())
			}
		}

		// Log output if present.
		if len(execOutput) > 0 {
			clog.WithField("output_length", len(execOutput)).Debug("Command output captured")
		}

		// Handle specific exit codes.
		if execInspect.ExitCode == ExTempFail {
			return true, nil // Skip updates on temporary failure.
		}

		if execInspect.ExitCode > 0 {
			err := fmt.Errorf(
				"%w with exit code %d: %s",
				errCommandFailed,
				execInspect.ExitCode,
				execOutput,
			)
			clog.WithError(err).Debug("Command execution failed")

			return false, err
		}

		break
	}

	return false, nil
}
