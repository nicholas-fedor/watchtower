package container

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"

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
	IsContainerStale(container types.Container, params types.UpdateParams) (bool, types.ImageID, error)

	// ExecuteCommand runs a command inside a container and returns whether to skip updates based on the result.
	// The timeout specifies how long to wait for the command to complete.
	ExecuteCommand(containerID types.ContainerID, command string, timeout int) (bool, error)

	// RemoveImageByID deletes an image from the Docker host by its ID.
	// Returns an error if the removal fails.
	RemoveImageByID(imageID types.ImageID) error

	// WarnOnHeadPullFailed determines whether to log a warning when a HEAD request fails during image pulls.
	// The decision is based on the configured warning strategy and container context.
	WarnOnHeadPullFailed(container types.Container) bool
}

// dockerClient is the concrete implementation of the Client interface.
// It wraps the Docker API client and applies custom behavior via ClientOptions.
type dockerClient struct {
	api client.APIClient
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
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		logrus.Fatalf("Error instantiating Docker client: %s", err)
	}

	return &dockerClient{
		api:           cli,
		ClientOptions: opts,
	}
}

// ListContainers retrieves a list of existing containers.
// It delegates to ListSourceContainers to fetch and filter containers based on the provided function.
// Returns a slice of containers or an error if listing fails.
func (c dockerClient) ListContainers(filter types.Filter) ([]types.Container, error) {
	return ListSourceContainers(c.api, c.ClientOptions, filter)
}

// GetContainer fetches detailed information about an existing container by its ID.
// It delegates to GetSourceContainer to retrieve the container details.
// Returns the container object as a types.Container interface, which is intentional to support multiple container implementations.
// Returns an error if retrieval fails.
func (c dockerClient) GetContainer(containerID types.ContainerID) (types.Container, error) {
	return GetSourceContainer(c.api, containerID)
}

// StopContainer stops and removes an existing container within the given timeout.
// It delegates to StopSourceContainer to handle the stopping and removal process.
// Returns an error if stopping or removal fails.
func (c dockerClient) StopContainer(container types.Container, timeout time.Duration) error {
	return StopSourceContainer(c.api, container, timeout, c.RemoveVolumes)
}

// StartContainer creates and starts a new container based on the source container’s configuration.
// It extracts the network configuration from the source and passes it to StartTargetContainer.
// Returns the new container’s ID or an error if creation or startup fails.
func (c dockerClient) StartContainer(container types.Container) (types.ContainerID, error) {
	networkConfig := getNetworkConfig(container)

	return StartTargetContainer(c.api, container, networkConfig, c.ReviveStopped)
}

// RenameContainer renames an existing container to the specified new name.
// It delegates to RenameTargetContainer to perform the renaming.
// Returns an error if the rename operation fails.
func (c dockerClient) RenameContainer(container types.Container, newName string) error {
	return RenameTargetContainer(c.api, container, newName)
}

// WarnOnHeadPullFailed decides whether to warn about failed HEAD requests during image pulls.
// It returns true if a warning should be logged, based on the configured strategy.
// Uses WarnAlways, WarnNever, or delegates to registry logic for WarnAuto.
func (c dockerClient) WarnOnHeadPullFailed(container types.Container) bool {
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
func (c dockerClient) IsContainerStale(container types.Container, params types.UpdateParams) (bool, types.ImageID, error) {
	imgClient := newImageClient(c.api)

	return imgClient.IsContainerStale(container, params, c.WarnOnHeadFailed)
}

// ExecuteCommand runs a command inside a container and evaluates its result.
// It creates an exec instance, runs it, and waits for completion or timeout.
// Returns whether to skip updates (based on exit code) and any error encountered.
func (c dockerClient) ExecuteCommand(containerID types.ContainerID, command string, timeout int) (bool, error) {
	ctx := context.Background()
	clog := logrus.WithField("containerID", containerID)

	// Configure and create the exec instance.
	execConfig := container.ExecOptions{
		Tty:          true,
		Detach:       false,
		Cmd:          []string{"sh", "-c", command},
		User:         "",
		Privileged:   false,
		ConsoleSize:  nil,
		AttachStdin:  false,
		AttachStderr: false,
		AttachStdout: false,
		DetachKeys:   "",
		Env:          nil,
		WorkingDir:   "",
	}

	exec, err := c.api.ContainerExecCreate(ctx, string(containerID), execConfig)
	if err != nil {
		return false, fmt.Errorf("failed to create exec instance: %w", err)
	}

	// Start the exec instance.
	execStartCheck := container.ExecStartOptions{
		Detach:      false,
		Tty:         true,
		ConsoleSize: nil,
	}
	if err := c.api.ContainerExecStart(ctx, exec.ID, execStartCheck); err != nil {
		return false, fmt.Errorf("failed to start exec instance: %w", err)
	}

	// Capture output and handle attachment.
	output, err := c.captureExecOutput(ctx, exec.ID)
	if err != nil {
		clog.Warnf("Failed to capture command output: %v", err)
	}

	// Wait for completion and interpret the result.
	skipUpdate, err := c.waitForExecOrTimeout(ctx, exec.ID, output, timeout)
	if err != nil {
		return true, fmt.Errorf("failed to wait for exec completion: %w", err)
	}

	return skipUpdate, nil
}

// captureExecOutput attaches to an exec instance and captures its output.
// It logs errors internally and returns the trimmed output or an error if attachment or reading fails.
func (c dockerClient) captureExecOutput(ctx context.Context, execID string) (string, error) {
	response, err := c.api.ContainerExecAttach(ctx, execID, container.ExecStartOptions{
		Tty:         true,
		Detach:      false,
		ConsoleSize: nil,
	})
	if err != nil {
		return "", fmt.Errorf("failed to attach to exec instance: %w", err)
	}
	defer response.Close()

	var writer bytes.Buffer

	written, err := writer.ReadFrom(response.Reader)
	if err != nil {
		return "", fmt.Errorf("failed to read exec output: %w", err)
	}

	if written > 0 {
		return strings.TrimSpace(writer.String()), nil
	}

	return "", nil
}

// waitForExecOrTimeout waits for an exec instance to complete or times out.
// It checks the exit code: 75 (ExTempFail) skips updates, >0 indicates failure.
// Returns whether to skip updates and any error encountered.
func (c dockerClient) waitForExecOrTimeout(parentContext context.Context, execID string, execOutput string, timeout int) (bool, error) {
	const ExTempFail = 75

	var ctx context.Context

	var cancel context.CancelFunc

	// Set up context with timeout if specified.
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parentContext, time.Duration(timeout)*time.Minute)
		defer cancel()
	} else {
		ctx = parentContext
	}

	for {
		execInspect, err := c.api.ContainerExecInspect(ctx, execID)
		if err != nil {
			return false, fmt.Errorf("failed to inspect exec instance: %w", err)
		}

		// Log exec status for debugging.
		logrus.WithFields(logrus.Fields{
			"exit-code":    execInspect.ExitCode,
			"exec-id":      execInspect.ExecID,
			"running":      execInspect.Running,
			"container-id": execInspect.ContainerID,
		}).Debug("Awaiting timeout or completion")

		if execInspect.Running {
			time.Sleep(1 * time.Second)

			continue
		}

		if len(execOutput) > 0 {
			logrus.Infof("Command output:\n%v", execOutput)
		}

		if execInspect.ExitCode == ExTempFail {
			return true, nil
		}

		if execInspect.ExitCode > 0 {
			return false, fmt.Errorf("%w with exit code %d: %s", errCommandFailed, execInspect.ExitCode, execOutput)
		}

		break
	}

	return false, nil
}

// RemoveImageByID deletes an image from the Docker host by its ID.
// It delegates to the imageClient to perform the removal.
// Returns an error if the removal fails.
func (c dockerClient) RemoveImageByID(imageID types.ImageID) error {
	imgClient := newImageClient(c.api)

	return imgClient.RemoveImageByID(imageID)
}
