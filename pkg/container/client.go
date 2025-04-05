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
// It provides methods for managing containers, images, and executing commands, abstracting the underlying Docker client operations.
type Client interface {
	// ListContainers retrieves a filtered list of containers running on the host.
	// The provided filter determines which containers are included in the result.
	ListContainers(filter types.Filter) ([]types.Container, error)

	// GetContainer fetches detailed information about a specific container by its ID.
	// Returns the container object or an error if the container cannot be retrieved.
	GetContainer(containerID types.ContainerID) (types.Container, error)

	// StopContainer stops and removes a specified container, respecting the given timeout.
	// It ensures the container is no longer running or present on the host.
	StopContainer(container types.Container, timeout time.Duration) error

	// StartContainer creates and starts a new container based on the provided container's configuration.
	// Returns the new container's ID or an error if creation or startup fails.
	StartContainer(container types.Container) (types.ContainerID, error)

	// RenameContainer renames an existing container to the specified new name.
	// Returns an error if the rename operation fails.
	RenameContainer(container types.Container, newName string) error

	// IsContainerStale checks if a container's image is outdated compared to the latest available version.
	// Returns whether the container is stale, the latest image ID, and any error encountered.
	IsContainerStale(
		container types.Container,
		params types.UpdateParams,
	) (bool, types.ImageID, error)

	// ExecuteCommand runs a command inside a container and returns whether to skip updates based on the result.
	// The timeout specifies how long to wait for the command to complete.
	ExecuteCommand(containerID types.ContainerID, command string, timeout int) (bool, error)

	// RemoveImageByID deletes an image from the Docker host by its ID.
	// Returns an error if the removal fails.
	RemoveImageByID(imageID types.ImageID) error

	// WarnOnHeadPullFailed determines whether to log a warning when a HEAD request fails during image pulls.
	// The decision is based on the configured warning strategy and container context.
	WarnOnHeadPullFailed(container types.Container) bool

	// GetVersion returns the client's API version.
	GetVersion() string
}

// client is the concrete implementation of the Client interface.
// It wraps the Docker API client and applies custom behavior via ClientOptions.
type client struct {
	api dockerClient.APIClient
	ClientOptions
}

// ClientOptions configures the behavior of the dockerClient wrapper around the Docker API.
// It controls container management and warning behaviors.
type ClientOptions struct {
	RemoveVolumes     bool
	IncludeStopped    bool
	ReviveStopped     bool
	IncludeRestarting bool
	WarnOnHeadFailed  WarningStrategy
}

// NewClient initializes a new Client instance for interacting with the Docker API.
// It configures the client using environment variables and the provided options.
// Environment variables used include DOCKER_HOST, DOCKER_TLS_VERIFY, and DOCKER_API_VERSION.
// Panics if the Docker client cannot be instantiated due to invalid configuration.
func NewClient(opts ClientOptions) Client {
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

// ListContainers retrieves a list of existing containers.
// It delegates to ListSourceContainers to fetch and filter containers based on the provided function.
// Returns a slice of containers or an error if listing fails.
func (c client) ListContainers(filter types.Filter) ([]types.Container, error) {
	containers, err := ListSourceContainers(c.api, c.ClientOptions, filter)
	if err != nil {
		logrus.WithError(err).Debug("Failed to list containers")

		return nil, err
	}

	logrus.WithField("count", len(containers)).Debug("Listed containers")

	return containers, nil
}

// GetContainer fetches detailed information about an existing container by its ID.
// It delegates to GetSourceContainer to retrieve the container details.
// Returns the container object as a types.Container interface, which is intentional to support multiple container implementations.
// Returns an error if retrieval fails.
func (c client) GetContainer(containerID types.ContainerID) (types.Container, error) {
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

// StopContainer stops and removes an existing container within the given timeout.
// It delegates to StopSourceContainer to handle the stopping and removal process.
// Returns an error if stopping or removal fails.
func (c client) StopContainer(container types.Container, timeout time.Duration) error {
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

// StartContainer creates and starts a new container based on the source container’s configuration.
// It extracts the network configuration from the source and passes it to StartTargetContainer.
// Returns the new container’s ID or an error if creation or startup fails.
func (c client) StartContainer(container types.Container) (types.ContainerID, error) {
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	var networkConfig *network.NetworkingConfig

	clientVersion := c.GetVersion()
	minSupportedVersion := flags.DockerAPIMinVersion

	// Obtain the container's network configuration based on the client's API version
	switch {
	// If the client is using version 1.44 or greater
	case versions.GreaterThanOrEqualTo(clientVersion, minSupportedVersion):
		logrus.WithFields(fields).WithFields(logrus.Fields{
			"client_version": clientVersion,
			"min_version":    minSupportedVersion,
		}).Debug("Using modern network config")

		networkConfig = getNetworkConfig(container)

		// If the client is using versions less than 1.44
	case versions.LessThan(clientVersion, minSupportedVersion):
		logrus.WithFields(fields).WithFields(logrus.Fields{
			"client_version": clientVersion,
			"min_version":    minSupportedVersion,
		}).Debug("Using legacy network config")

		networkConfig = getLegacyNetworkConfig(container, clientVersion)
	}

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

// RenameContainer renames an existing container to the specified new name.
// It delegates to RenameTargetContainer to perform the renaming.
// Returns an error if the rename operation fails.
func (c client) RenameContainer(container types.Container, newName string) error {
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

// WarnOnHeadPullFailed decides whether to warn about failed HEAD requests during image pulls.
// It returns true if a warning should be logged, based on the configured strategy.
// Uses WarnAlways, WarnNever, or delegates to registry logic for WarnAuto.
func (c client) WarnOnHeadPullFailed(container types.Container) bool {
	if c.WarnOnHeadFailed == WarnAlways {
		return true
	}

	if c.WarnOnHeadFailed == WarnNever {
		return false
	}

	return registry.WarnOnAPIConsumption(container)
}

// IsContainerStale determines if a container’s image is outdated compared to the latest available version.
// It delegates to the imageClient to check staleness.
// Returns whether the container is stale, the latest image ID, and any error encountered.
func (c client) IsContainerStale(
	container types.Container,
	params types.UpdateParams,
) (bool, types.ImageID, error) {
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
// It creates an exec instance, runs it, and waits for completion or timeout.
// Returns whether to skip updates (based on exit code) and any error encountered.
func (c client) ExecuteCommand(
	containerID types.ContainerID,
	command string,
	timeout int,
) (bool, error) {
	ctx := context.Background()
	clog := logrus.WithField("container_id", containerID)

	// Configure and create the exec instance.
	clog.WithField("command", command).Debug("Creating exec instance")
	execConfig := dockerContainer.ExecOptions{
		Tty:    true,
		Detach: false,
		Cmd:    []string{"sh", "-c", command},
	}

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

	// Wait for completion and interpret the result.
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
// It logs errors internally and returns the trimmed output or an error if attachment or reading fails.
func (c client) captureExecOutput(ctx context.Context, execID string) (string, error) {
	clog := logrus.WithField("exec_id", execID)

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

	var writer bytes.Buffer

	written, err := writer.ReadFrom(response.Reader)
	if err != nil {
		clog.WithError(err).Debug("Failed to read exec output")

		return "", fmt.Errorf("%w: %w", errReadExecOutputFailed, err)
	}

	if written > 0 {
		output := strings.TrimSpace(writer.String())
		clog.WithField("output", output).Debug("Captured exec output")

		return output, nil
	}

	return "", nil
}

// waitForExecOrTimeout waits for an exec instance to complete or times out.
// It checks the exit code: 75 (ExTempFail) skips updates, >0 indicates failure.
// Returns whether to skip updates and any error encountered.
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
			time.Sleep(1 * time.Second)

			continue
		}

		if len(execOutput) > 0 {
			clog.WithField("output", execOutput).Info("Command output captured")
		}

		if execInspect.ExitCode == ExTempFail {
			return true, nil
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
// It delegates to the imageClient to perform the removal.
// Returns an error if the removal fails.
func (c client) RemoveImageByID(imageID types.ImageID) error {
	imgClient := newImageClient(c.api)

	err := imgClient.RemoveImageByID(imageID)
	if err != nil {
		logrus.WithError(err).WithField("image_id", imageID).Debug("Failed to remove image")

		return err
	}

	logrus.WithField("image_id", imageID).Debug("Removed image")

	return nil
}

// GetVersion gets the client API version from the Docker host.
func (c client) GetVersion() string {
	return c.api.ClientVersion()
}
