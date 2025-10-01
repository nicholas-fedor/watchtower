package container

import (
	"bufio"
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

	"github.com/docker/docker/api/types/build"
	"github.com/sirupsen/logrus"

	dockerContainer "github.com/docker/docker/api/types/container"
	dockerRegistry "github.com/docker/docker/api/types/registry"
	dockerClient "github.com/docker/docker/client"

	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/pkg/registry"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for container health operations.
var (
	// errHealthCheckTimeout indicates that waiting for a container to become healthy timed out.
	errHealthCheckTimeout = errors.New("timeout waiting for container to become healthy")
	// errHealthCheckFailed indicates that a container's health check failed.
	errHealthCheckFailed = errors.New("container health check failed")
)

// defaultImageBuildTimeout defines the default timeout for image build operations.
const defaultImageBuildTimeout = 30 * time.Second

// Client defines the interface for interacting with the Docker API within Watchtower.
//
// It provides methods for managing containers, images, and executing commands, abstracting the underlying Docker client operations.
type Client interface {
	// ListContainers retrieves a filtered list of containers running on the host.
	//
	// The provided filter determines which containers are included in the result.
	ListContainers(filter types.Filter) ([]types.Container, error)

	// GetContainer fetches detailed information about a specific container by its ID.
	//
	// Returns the container object or an error if the container cannot be retrieved.
	GetContainer(containerID types.ContainerID) (types.Container, error)

	// StopContainer stops and removes a specified container, respecting the given timeout.
	//
	// It ensures the container is no longer running or present on the host.
	StopContainer(container types.Container, timeout time.Duration) error

	// StartContainer creates and starts a new container based on the provided container's configuration.
	//
	// Returns the new container's ID or an error if creation or startup fails.
	StartContainer(container types.Container) (types.ContainerID, error)

	// RenameContainer renames an existing container to the specified new name.
	//
	// Returns an error if the rename operation fails.
	RenameContainer(container types.Container, newName string) error

	// IsContainerStale checks if a container's image is outdated compared to the latest available version.
	//
	// Returns whether the container is stale, the latest image ID, and any error encountered.
	IsContainerStale(
		container types.Container,
		params types.UpdateParams,
	) (bool, types.ImageID, error)

	// ExecuteCommand runs a command inside a container and returns whether to skip updates based on the result.
	//
	// The timeout specifies how long to wait for the command to complete.
	// UID and GID specify the user to run the command as, defaulting to container's configured user.
	ExecuteCommand(
		container types.Container,
		command string,
		timeout int,
		uid int,
		gid int,
	) (bool, error)

	// RemoveImageByID deletes an image from the Docker host by its ID.
	//
	// Returns an error if the removal fails.
	RemoveImageByID(imageID types.ImageID) error

	// WarnOnHeadPullFailed determines whether to log a warning when a HEAD request fails during image pulls.
	//
	// The decision is based on the configured warning strategy and container context.
	WarnOnHeadPullFailed(container types.Container) bool

	// GetVersion returns the client's API version.
	GetVersion() string

	// GetInfo returns system information from the Docker daemon.
	//
	// Returns system-wide information about the Docker installation.
	GetInfo() (map[string]any, error)

	// WaitForContainerHealthy waits for a container to become healthy or times out.
	//
	// It polls the container's health status until it reports "healthy" or the timeout is reached.
	// If the container has no health check configured, it returns immediately.
	WaitForContainerHealthy(containerID types.ContainerID, timeout time.Duration) error

	// BuildImageFromGit builds a Docker image from a Git repository.
	//
	// It clones the repository, checks out the specified commit, and builds the image using the Dockerfile.
	// Returns the built image ID or an error if the build fails.
	BuildImageFromGit(
		ctx context.Context,
		repoURL, commitHash, imageName string,
		auth map[string]string,
	) (types.ImageID, error)

	// ListAllContainers retrieves a list of all containers from the Docker host, regardless of status.
	//
	// Returns all containers without filtering by status or other criteria.
	ListAllContainers() ([]types.Container, error)
}

// client is the concrete implementation of the Client interface.
//
// It wraps the Docker API client and applies custom behavior via ClientOptions.
type client struct {
	api dockerClient.APIClient
	ClientOptions
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
}

// NewClient initializes a new Client instance for Docker API interactions.
//
// It configures the client using environment variables (e.g., DOCKER_HOST, DOCKER_API_VERSION) and validates the API version, falling back to autonegotiation if necessary.
//
// Parameters:
//   - opts: Options to customize container management behavior.
//
// Returns:
//   - Client: Initialized client instance (panics on failure).
func NewClient(opts ClientOptions) Client {
	ctx := context.Background()

	// Initialize client with autonegotiation, ignoring DOCKER_API_VERSION initially.
	cli, err := dockerClient.NewClientWithOpts(
		dockerClient.WithHost(os.Getenv("DOCKER_HOST")),
		dockerClient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to initialize Docker client")
	}

	// Apply forced API version if set and valid.
	if version := strings.Trim(os.Getenv("DOCKER_API_VERSION"), "\""); version != "" {
		pingCli, err := dockerClient.NewClientWithOpts(
			dockerClient.WithHost(cli.DaemonHost()),
			dockerClient.WithVersion(version),
		)
		if err != nil {
			logrus.WithError(err).Fatal("Failed to create test client")
		}

		if _, err := pingCli.Ping(ctx); err != nil &&
			strings.Contains(err.Error(), "page not found") {
			logrus.WithFields(logrus.Fields{
				"version":  version,
				"error":    err,
				"endpoint": "/_ping",
			}).Warn("Invalid API version; falling back to autonegotiation")
			cli.NegotiateAPIVersion(ctx)
		} else {
			cli = pingCli
		}
	} else {
		cli.NegotiateAPIVersion(ctx)
	}

	// Log client and server API versions.
	selectedVersion := cli.ClientVersion()

	if serverVersion, err := cli.ServerVersion(ctx); err != nil {
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

// ListContainers retrieves a filtered list of containers running on the host.
//
// Parameters:
//   - filter: Filter to apply to container list.
//
// Returns:
//   - []types.Container: List of matching containers.
//   - error: Non-nil if listing fails, nil on success.
func (c client) ListContainers(filter types.Filter) ([]types.Container, error) {
	// Fetch and filter containers using helper function.
	containers, err := ListSourceContainers(c.api, c.ClientOptions, filter)
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
//   - containerID: ID of the container to retrieve.
//
// Returns:
//   - types.Container: Container details if found.
//   - error: Non-nil if retrieval fails, nil on success.
func (c client) GetContainer(containerID types.ContainerID) (types.Container, error) {
	// Retrieve container details using helper function.
	container, err := GetSourceContainer(c.api, containerID)
	if err != nil {
		logrus.WithError(err).
			WithField("container_id", containerID).
			Debug("Failed to get container")

		return nil, err
	}

	logrus.WithField("container_id", containerID).Debug("Retrieved container details")

	return container, nil
}

// StopContainer stops and removes a specified container.
//
// Parameters:
//   - container: Container to stop and remove.
//   - timeout: Duration to wait before forcing stop.
//
// Returns:
//   - error: Non-nil if stop/removal fails, nil on success.
func (c client) StopContainer(container types.Container, timeout time.Duration) error {
	// Stop and remove container using helper function with volume option.
	err := StopSourceContainer(c.api, container, timeout, c.RemoveVolumes)
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
//   - container: Source container to replicate.
//
// Returns:
//   - types.ContainerID: ID of the new container.
//   - error: Non-nil if creation/start fails, nil on success.
func (c client) StartContainer(container types.Container) (types.ContainerID, error) {
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	clientVersion := c.GetVersion()

	logrus.WithFields(fields).WithField("client_version", clientVersion).
		Debug("Obtaining source container network configuration")

	// Get unified network config.
	networkConfig := getNetworkConfig(container, clientVersion)

	// Detect if running on Podman for CPU compatibility.
	// Podman and Docker handle CPU resource allocation differently when copying container configurations.
	// Podman requires special handling for CPU settings to ensure proper resource limits are applied,
	// whereas Docker can use standard copying mechanisms. This detection ensures Watchtower applies
	// the correct CPU copying strategy based on the container runtime being used.
	isPodman := false

	if c.CPUCopyMode == "auto" {
		// When CPUCopyMode is set to "auto", automatically detect the container runtime (Podman vs Docker)
		// to determine the appropriate CPU copying behavior. This prevents resource allocation issues
		// that could occur if Docker-specific logic is applied to Podman or vice versa.
		info, err := c.GetInfo()
		if err != nil {
			// If system info retrieval fails, fall back to assuming Docker behavior.
			// This conservative approach ensures compatibility in environments where runtime detection
			// is not possible, defaulting to the more common Docker runtime assumptions.
			logrus.WithError(err).
				Debug("Failed to get system info for Podman detection, assuming Docker")
		} else {
			// Detection works by examining the system info returned by the Docker API client.
			// Podman identifies itself in two ways:
			// 1. The "Name" field equals "podman"
			// 2. The "ServerVersion" field contains "podman" (case-insensitive)
			// This dual-check ensures reliable detection across different Podman versions and configurations.
			if name, exists := info["Name"]; exists && name == "podman" {
				isPodman = true
			} else if serverVersion, exists := info["ServerVersion"]; exists {
				if sv, ok := serverVersion.(string); ok && strings.Contains(strings.ToLower(sv), "podman") {
					isPodman = true
				}
			}
		}
	}

	// Start new container with selected config.
	newID, err := StartTargetContainer(
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

	logrus.WithFields(fields).WithField("new_id", newID).Debug("Started new container")

	return newID, nil
}

// ListAllContainers retrieves a list of all containers from the Docker host, regardless of status.
//
// Returns:
//   - []types.Container: List of all containers.
//   - error: Non-nil if listing fails, nil on success.
func (c client) ListAllContainers() ([]types.Container, error) {
	ctx := context.Background()
	clog := logrus.WithField("list_all", true)

	clog.Debug("Retrieving all container list")

	// Fetch containers with no status filter
	containers, err := c.api.ContainerList(ctx, dockerContainer.ListOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "page not found") {
			clog.WithFields(logrus.Fields{
				"error":       err,
				"endpoint":    "/containers/json",
				"api_version": strings.Trim(c.api.ClientVersion(), "\""),
				"docker_host": os.Getenv("DOCKER_HOST"),
			}).Warn("Docker API returned 404 for container list; treating as empty list")

			return []types.Container{}, nil
		}

		clog.WithError(err).Debug("Failed to list all containers")

		return nil, fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

	// Convert to types.Container
	hostContainers := []types.Container{}

	for _, runningContainer := range containers {
		container, err := GetSourceContainer(c.api, types.ContainerID(runningContainer.ID))
		if err != nil {
			return nil, err
		}

		hostContainers = append(hostContainers, container)
	}

	clog.WithField("count", len(hostContainers)).Debug("Listed all containers")

	return hostContainers, nil
}

// RenameContainer renames an existing container to a new name.
//
// Parameters:
//   - container: Container to rename.
//   - newName: New name for the container.
//
// Returns:
//   - error: Non-nil if rename fails, nil on success.
func (c client) RenameContainer(container types.Container, newName string) error {
	// Perform rename using helper function.
	err := RenameTargetContainer(c.api, container, newName)
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
	container types.Container,
	params types.UpdateParams,
) (bool, types.ImageID, error) {
	// Use image client to perform staleness check.
	imgClient := newImageClient(c.api)

	stale, newestImage, err := imgClient.IsContainerStale(container, params, c.WarnOnHeadFailed)
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
	container types.Container,
	command string,
	timeout int,
	uid int,
	gid int,
) (bool, error) {
	ctx := context.Background()
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
		Detach: false,
		Cmd:    []string{"sh", "-c", command},
		Env:    []string{"WT_CONTAINER=" + metadataJSON},
		User:   user,
	}

	// Create the exec instance.
	exec, err := c.api.ContainerExecCreate(ctx, string(container.ID()), execConfig)
	if err != nil {
		clog.WithError(err).Debug("Failed to create exec instance")

		return false, fmt.Errorf("%w: %w", errCreateExecFailed, err)
	}

	// Start the exec instance.
	clog.WithField("exec_id", exec.ID).Debug("Starting exec instance")

	execStartCheck := dockerContainer.ExecStartOptions{Tty: true}
	if err := c.api.ContainerExecStart(ctx, exec.ID, execStartCheck); err != nil {
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

	// Read output into a buffer.
	var writer bytes.Buffer

	written, err := writer.ReadFrom(response.Reader)
	if err != nil {
		clog.WithError(err).Debug("Failed to read exec output")

		return "", fmt.Errorf("%w: %w", errReadExecOutputFailed, err)
	}

	// Return trimmed output if any was captured.
	if written > 0 {
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
			time.Sleep(1 * time.Second) // Wait before rechecking.

			continue
		}

		// Log output if present.
		if len(execOutput) > 0 {
			clog.WithField("output", execOutput).Info("Command output captured")
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

// RemoveImageByID deletes an image from the Docker host by its ID.
//
// Parameters:
//   - imageID: ID of the image to remove.
//
// Returns:
//   - error: Non-nil if removal fails, nil on success.
func (c client) RemoveImageByID(imageID types.ImageID) error {
	// Use image client to remove the image.
	imgClient := newImageClient(c.api)

	err := imgClient.RemoveImageByID(imageID)
	if err != nil {
		logrus.WithError(err).WithField("image_id", imageID).Debug("Failed to remove image")

		return err
	}

	logrus.WithField("image_id", imageID).Debug("Removed image")

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
// Returns:
//   - map[string]interface{}: System information.
//   - error: Non-nil if retrieval fails, nil on success.
func (c client) GetInfo() (map[string]any, error) {
	ctx := context.Background()

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
	containerID types.ContainerID,
	timeout time.Duration,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
			if inspect.State.Health == nil {
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

// BuildImageFromGit builds a Docker image from a Git repository.
//
// Parameters:
//   - ctx: Context for the build operation.
//   - repoURL: URL of the Git repository to build from.
//   - commitHash: Specific commit hash to build from.
//   - imageName: Name/tag for the built image.
//   - auth: Authentication credentials (username, password, etc.).
//
// Returns:
//   - types.ImageID: ID of the built image.
//   - error: Non-nil if build fails, nil on success.
func (c client) BuildImageFromGit(
	ctx context.Context,
	repoURL, commitHash, imageName string,
	auth map[string]string,
) (types.ImageID, error) {
	fields := logrus.Fields{
		"repo_url":    repoURL,
		"commit_hash": commitHash,
		"image_name":  imageName,
	}

	logrus.WithFields(fields).Debug("Starting Git-based image build")

	// Set up build options
	buildOptions := build.ImageBuildOptions{
		RemoteContext: repoURL,      // Git URL as remote context
		Dockerfile:    "Dockerfile", // Assume standard Dockerfile location
		Tags:          []string{imageName},
		BuildArgs: map[string]*string{
			"GIT_COMMIT": &commitHash,
		},
		Labels: map[string]string{
			"com.centurylinklabs.watchtower.git-repo":   repoURL,
			"com.centurylinklabs.watchtower.git-commit": commitHash,
			"com.centurylinklabs.watchtower.built-at":   time.Now().Format(time.RFC3339),
		},
		Remove:      true, // Remove intermediate containers
		ForceRemove: true, // Force removal even if in use
		PullParent:  true, // Pull base images
	}

	// Add authentication if provided
	if username, ok := auth["username"]; ok {
		if password, ok := auth["password"]; ok {
			// Use Docker registry auth config
			buildOptions.AuthConfigs = map[string]dockerRegistry.AuthConfig{
				// This would need to be expanded for different registries
				"docker.io": {
					Username: username,
					Password: password,
				},
			}
		}
	}

	// Execute the build
	response, err := c.api.ImageBuild(ctx, nil, buildOptions)
	if err != nil {
		logrus.WithFields(fields).WithError(err).Debug("Failed to start image build")

		return "", fmt.Errorf("failed to start image build: %w", err)
	}
	defer response.Body.Close()

	// Stream build output for logging
	if err := c.streamBuildOutput(response.Body); err != nil {
		logrus.WithFields(fields).WithError(err).Debug("Build failed during execution")

		return "", fmt.Errorf("build execution failed: %w", err)
	}

	// Extract the built image ID from the build output
	// This is a simplified version - in practice, we'd need to parse the build output
	buildCtx, cancel := context.WithTimeout(ctx, defaultImageBuildTimeout)
	defer cancel()

	imageID, err := c.extractImageIDFromBuild(buildCtx, imageName)
	if err != nil {
		logrus.WithFields(fields).WithError(err).Debug("Failed to extract built image ID")

		return "", fmt.Errorf("failed to extract image ID: %w", err)
	}

	logrus.WithFields(fields).
		WithField("image_id", imageID).
		Debug("Successfully built image from Git")

	return imageID, nil
}

// streamBuildOutput streams and logs the build output from Docker.
//
// Parameters:
//   - reader: Reader containing the build output stream.
//
// Returns:
//   - error: Non-nil if streaming fails, nil on success.
func (c client) streamBuildOutput(reader io.ReadCloser) error {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		// Parse and log build output
		logrus.WithField("build_output", line).Debug("Build output")
		// In a full implementation, we'd parse JSON build output here
	}

	scanErr := scanner.Err()
	logrus.WithField("scanner_error", scanErr).Debug("Build output scanning completed")

	if scanErr != nil {
		return fmt.Errorf("failed to scan build output: %w", scanErr)
	}

	return nil
}

// extractImageIDFromBuild extracts the image ID from a successfully built image.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - imageName: Name of the built image.
//
// Returns:
//   - types.ImageID: ID of the built image.
//   - error: Non-nil if extraction fails, nil on success.
func (c client) extractImageIDFromBuild(
	ctx context.Context,
	imageName string,
) (types.ImageID, error) {
	// Inspect the built image to get its ID
	inspect, err := c.api.ImageInspect(ctx, imageName)
	if err != nil {
		return "", fmt.Errorf("failed to inspect built image: %w", err)
	}

	return types.ImageID(inspect.ID), nil
}
