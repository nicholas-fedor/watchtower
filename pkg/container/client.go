package container

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/versions"
	"github.com/sirupsen/logrus"

	dockerContainer "github.com/docker/docker/api/types/container"
	dockerClient "github.com/docker/docker/client"

	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/pkg/registry"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

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
	ExecuteCommand(containerID types.ContainerID, command string, timeout int) (bool, error)

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
	RemoveVolumes     bool
	IncludeStopped    bool
	ReviveStopped     bool
	IncludeRestarting bool
	WarnOnHeadFailed  WarningStrategy
}

// NewClient initializes a new Client instance for Docker API interactions.
//
// It uses environment variables (DOCKER_HOST, DOCKER_TLS_VERIFY, DOCKER_API_VERSION) for configuration.
//
// Parameters:
//   - opts: Client options for custom behavior.
//
// Returns:
//   - Client: Initialized client instance (panics on failure).
func NewClient(opts ClientOptions) Client {
	// Create Docker client from environment settings.
	cli, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to initialize Docker client")
	}

	logrus.WithFields(logrus.Fields{
		"version": cli.ClientVersion(),
		"opts":    fmt.Sprintf("%+v", opts),
	}).Debug("Initialized Docker client")

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

// StartContainer creates and starts a new container from an existing one’s config.
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

	var networkConfig *network.NetworkingConfig

	clientVersion := c.GetVersion()
	minSupportedVersion := flags.DockerAPIMinVersion

	// Select network config based on API version.
	switch {
	case versions.GreaterThanOrEqualTo(clientVersion, minSupportedVersion): // versions 1.44 or greater
		logrus.WithFields(fields).WithFields(logrus.Fields{
			"client_version": clientVersion,
			"min_version":    minSupportedVersion,
		}).Debug("Using modern network config")

		networkConfig = getNetworkConfig(container)

	case versions.LessThan(clientVersion, minSupportedVersion): // versions less than 1.44
		logrus.WithFields(fields).WithFields(logrus.Fields{
			"client_version": clientVersion,
			"min_version":    minSupportedVersion,
		}).Debug("Using legacy network config")

		networkConfig = getLegacyNetworkConfig(container, clientVersion)
	}

	// Start new container with selected config.
	newID, err := StartTargetContainer(
		c.api,
		container,
		networkConfig,
		c.ReviveStopped,
		clientVersion,
		minSupportedVersion,
	)
	if err != nil {
		logrus.WithFields(fields).WithError(err).Debug("Failed to start new container")

		return "", err
	}

	logrus.WithFields(fields).WithField("new_id", newID).Debug("Started new container")

	return newID, nil
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
//   - containerID: ID of the container.
//   - command: Command to execute.
//   - timeout: Minutes to wait before timeout (0 for no timeout).
//
// Returns:
//   - bool: True if updates should be skipped, false otherwise.
//   - error: Non-nil if execution fails, nil on success.
func (c client) ExecuteCommand(
	containerID types.ContainerID,
	command string,
	timeout int,
) (bool, error) {
	ctx := context.Background()
	clog := logrus.WithField("container_id", containerID)

	// Set up exec configuration with command.
	clog.WithField("command", command).Debug("Creating exec instance")
	execConfig := dockerContainer.ExecOptions{
		Tty:    true,
		Detach: false,
		Cmd:    []string{"sh", "-c", command},
	}

	// Create the exec instance.
	exec, err := c.api.ContainerExecCreate(ctx, string(containerID), execConfig)
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
	return c.api.ClientVersion()
}
