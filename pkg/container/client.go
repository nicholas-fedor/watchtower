package container

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/context"

	"github.com/nicholas-fedor/watchtower/pkg/registry"
	"github.com/nicholas-fedor/watchtower/pkg/registry/digest"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// defaultStopSignal defines the default signal used to stop containers when no custom signal is specified.
// This is set to "SIGTERM" to allow containers to terminate gracefully by default.
const defaultStopSignal = "SIGTERM"

// Client defines the interface for interacting with the Docker API within Watchtower.
// It provides methods for managing containers, images, and executing commands, abstracting the underlying Docker client operations.
type Client interface {
	// ListContainers retrieves a filtered list of containers running on the host.
	// The provided filter determines which containers are included in the result.
	ListContainers(types.Filter) ([]types.Container, error)

	// GetContainer fetches detailed information about a specific container by its ID.
	// Returns the container object or an error if the container cannot be retrieved.
	GetContainer(containerID types.ContainerID) (types.Container, error)

	// StopContainer stops and removes a specified container, respecting the given timeout.
	// It ensures the container is no longer running or present on the host.
	StopContainer(types.Container, time.Duration) error

	// StartContainer creates and starts a new container based on the provided container's configuration.
	// Returns the new container's ID or an error if creation or startup fails.
	StartContainer(types.Container) (types.ContainerID, error)

	// RenameContainer renames an existing container to the specified new name.
	// Returns an error if the rename operation fails.
	RenameContainer(types.Container, string) error

	// IsContainerStale checks if a container's image is outdated compared to the latest available version.
	// Returns whether the container is stale, the latest image ID, and any error encountered.
	IsContainerStale(types.Container, types.UpdateParams) (stale bool, latestImage types.ImageID, err error)

	// ExecuteCommand runs a command inside a container and returns whether to skip updates based on the result.
	// The timeout specifies how long to wait for the command to complete.
	ExecuteCommand(containerID types.ContainerID, command string, timeout int) (SkipUpdate bool, err error)

	// RemoveImageByID deletes an image from the Docker host by its ID.
	// Returns an error if the removal fails.
	RemoveImageByID(types.ImageID) error

	// WarnOnHeadPullFailed determines whether to log a warning when a HEAD request fails during image pulls.
	// The decision is based on the configured warning strategy and container context.
	WarnOnHeadPullFailed(container types.Container) bool
}

// NewClient initializes a new Client instance for interacting with the Docker API.
// It configures the client using environment variables and the provided options.
// Environment variables used:
//   - DOCKER_HOST: The Docker engine host to connect to (e.g., unix:///var/run/docker.sock).
//   - DOCKER_TLS_VERIFY: Enables TLS verification if set to "1".
//   - DOCKER_API_VERSION: Specifies the minimum Docker API version to use.
//
// Panics if the Docker client cannot be instantiated due to invalid configuration.
func NewClient(opts ClientOptions) Client {
	cli, err := client.NewClientWithOpts(client.FromEnv)

	if err != nil {
		logrus.Fatalf("Error instantiating Docker client: %s", err)
	}

	return dockerClient{
		api:           cli,
		ClientOptions: opts,
	}
}

// ClientOptions configures the behavior of the dockerClient wrapper around the Docker API.
// These options control container management and warning behaviors.
type ClientOptions struct {
	// RemoveVolumes specifies whether to remove volumes when deleting containers.
	RemoveVolumes bool

	// IncludeStopped determines if stopped containers are included in listings.
	IncludeStopped bool

	// ReviveStopped indicates whether stopped containers should be restarted during updates.
	ReviveStopped bool

	// IncludeRestarting includes containers in the "restarting" state in listings if true.
	IncludeRestarting bool

	// WarnOnHeadFailed sets the strategy for logging warnings on HEAD request failures during image pulls.
	WarnOnHeadFailed WarningStrategy
}

// WarningStrategy defines the policy for logging warnings when HEAD requests fail during image pulls.
// It controls the verbosity of error reporting in various scenarios.
type WarningStrategy string

const (
	// WarnAlways triggers a warning whenever a HEAD request fails, regardless of context.
	WarnAlways WarningStrategy = "always"

	// WarnNever suppresses warnings for HEAD request failures in all cases.
	WarnNever WarningStrategy = "never"

	// WarnAuto logs warnings for HEAD failures only when unexpected, based on registry heuristics.
	WarnAuto WarningStrategy = "auto"
)

// dockerClient is the concrete implementation of the Client interface.
// It wraps the Docker API client and applies custom behavior via ClientOptions.
type dockerClient struct {
	// api is the underlying Docker API client used for all operations.
	api client.APIClient

	// ClientOptions holds configuration settings for this client instance.
	ClientOptions
}

// WarnOnHeadPullFailed decides whether to warn about failed HEAD requests during image pulls.
// Returns true if a warning should be logged, based on the configured strategy:
// - WarnAlways: Always returns true.
// - WarnNever: Always returns false.
// - WarnAuto: Delegates to registry.WarnOnAPIConsumption for context-aware decision.
func (c dockerClient) WarnOnHeadPullFailed(targetContainer types.Container) bool {
	if c.WarnOnHeadFailed == WarnAlways {
		return true
	}
	if c.WarnOnHeadFailed == WarnNever {
		return false
	}

	return registry.WarnOnAPIConsumption(targetContainer)
}

// ListContainers retrieves a list of containers from the Docker host, filtered by the provided function.
// It respects the IncludeStopped and IncludeRestarting options to determine which container states to include.
// Returns a slice of containers or an error if the listing fails.
func (c dockerClient) ListContainers(fn types.Filter) ([]types.Container, error) {
	hostContainers := []types.Container{}
	ctx := context.Background()

	// Log the scope of containers being retrieved based on configuration.
	if c.IncludeStopped && c.IncludeRestarting {
		logrus.Debug("Retrieving running, stopped, restarting and exited containers")
	} else if c.IncludeStopped {
		logrus.Debug("Retrieving running, stopped and exited containers")
	} else if c.IncludeRestarting {
		logrus.Debug("Retrieving running and restarting containers")
	} else {
		logrus.Debug("Retrieving running containers")
	}

	// Apply filters based on configured options.
	filter := c.createListFilter()
	containers, err := c.api.ContainerList(
		ctx,
		container.ListOptions{
			Filters: filter,
		},
	)
	if err != nil {
		return nil, err
	}

	// Fetch detailed info for each container and apply the user-provided filter.
	for _, runningContainer := range containers {
		c, err := c.GetContainer(types.ContainerID(runningContainer.ID))
		if err != nil {
			return nil, err
		}
		if fn(c) {
			hostContainers = append(hostContainers, c)
		}
	}

	return hostContainers, nil
}

// createListFilter constructs a filter for container listing based on ClientOptions.
// It always includes running containers and optionally adds stopped or restarting states.
func (c dockerClient) createListFilter() filters.Args {
	filterArgs := filters.NewArgs()
	filterArgs.Add("status", "running")

	if c.IncludeStopped {
		filterArgs.Add("status", "created")
		filterArgs.Add("status", "exited")
	}

	if c.IncludeRestarting {
		filterArgs.Add("status", "restarting")
	}

	return filterArgs
}

// GetContainer retrieves detailed information about a container by its ID.
// It also resolves network container references by replacing IDs with names when possible.
// Returns a Container object or an error if inspection fails.
func (c dockerClient) GetContainer(containerID types.ContainerID) (types.Container, error) {
	ctx := context.Background()

	// Fetch basic container information.
	containerInfo, err := c.api.ContainerInspect(ctx, string(containerID))
	if err != nil {
		return &Container{}, err
	}

	// Handle container network mode dependencies.
	netType, netContainerId, found := strings.Cut(string(containerInfo.HostConfig.NetworkMode), ":")
	if found && netType == "container" {
		parentContainer, err := c.api.ContainerInspect(ctx, netContainerId)
		if err != nil {
			logrus.WithFields(map[string]interface{}{
				"container":         containerInfo.Name,
				"error":             err,
				"network-container": netContainerId,
			}).Warnf("Unable to resolve network container: %v", err)
		} else {
			// Update NetworkMode to use the parent container's name for stable references across recreations.
			containerInfo.HostConfig.NetworkMode = container.NetworkMode(fmt.Sprintf("container:%s", parentContainer.Name))
		}
	}

	// Fetch associated image information.
	imageInfo, err := c.api.ImageInspect(ctx, containerInfo.Image)
	if err != nil {
		logrus.Warnf("Failed to retrieve container image info: %v", err)
		return &Container{containerInfo: &containerInfo, imageInfo: nil}, nil
	}

	return &Container{containerInfo: &containerInfo, imageInfo: &imageInfo}, nil
}

// StopContainer stops and removes the specified container within the given timeout.
// It first attempts to stop the container gracefully, then removes it unless AutoRemove is enabled.
// Returns an error if stopping or removal fails.
func (c dockerClient) StopContainer(targetContainer types.Container, timeout time.Duration) error {
	ctx := context.Background()
	signal := targetContainer.StopSignal()
	if signal == "" {
		signal = defaultStopSignal
	}

	idStr := string(targetContainer.ID())
	shortID := targetContainer.ID().ShortID()

	// Stop the container if it’s running.
	if targetContainer.IsRunning() {
		logrus.Infof("Stopping %s (%s) with %s", targetContainer.Name(), shortID, signal)
		if err := c.api.ContainerKill(ctx, idStr, signal); err != nil {
			return err
		}
	}

	// Wait for the container to stop or timeout.
	stopped, err := c.waitForStopOrTimeout(targetContainer, timeout)
	if err != nil {
		return fmt.Errorf("failed to wait for container %s (%s) to stop: %w", targetContainer.Name(), shortID, err)
	}
	if !stopped {
		logrus.Warnf("Container %s (%s) did not stop within %v", targetContainer.Name(), shortID, timeout)
	}

	// Handle removal based on AutoRemove setting.
	if targetContainer.ContainerInfo().HostConfig.AutoRemove {
		logrus.Debugf("AutoRemove container %s, skipping ContainerRemove call.", shortID)
	} else {
		logrus.Debugf("Removing container %s", shortID)
		if err := c.api.ContainerRemove(ctx, idStr, container.RemoveOptions{Force: true, RemoveVolumes: c.RemoveVolumes}); err != nil {
			if client.IsErrNotFound(err) {
				logrus.Debugf("Container %s not found, skipping removal.", shortID)
				return nil
			}
			return err
		}
	}

	// Confirm the container is gone.
	stopped, err = c.waitForStopOrTimeout(targetContainer, timeout)
	if err != nil {
		return fmt.Errorf("failed to confirm removal of container %s (%s): %w", targetContainer.Name(), shortID, err)
	}
	if !stopped { // Container still present after timeout
		return fmt.Errorf("container %s (%s) could not be removed", targetContainer.Name(), shortID)
	}

	return nil
}

// GetNetworkConfig constructs the network configuration for a container.
// It filters out the container’s short ID from network aliases to prevent accumulation across updates.
// Returns the updated network configuration.
func (c dockerClient) GetNetworkConfig(targetContainer types.Container) *network.NetworkingConfig {
	config := &network.NetworkingConfig{
		EndpointsConfig: targetContainer.ContainerInfo().NetworkSettings.Networks,
	}

	for _, ep := range config.EndpointsConfig {
		aliases := make([]string, 0, len(ep.Aliases))
		cidAlias := targetContainer.ID().ShortID()

		// Exclude the container’s short ID from aliases to avoid alias buildup.
		for _, alias := range ep.Aliases {
			if alias == cidAlias {
				continue
			}
			aliases = append(aliases, alias)
		}
		ep.Aliases = aliases
	}
	return config
}

// StartContainer creates and starts a new container based on the provided container’s configuration.
// It reuses the original config, optionally reconnecting network settings, and respects ReviveStopped.
// Returns the new container’s ID or an error if creation or startup fails.
func (c dockerClient) StartContainer(targetContainer types.Container) (types.ContainerID, error) {
	bg := context.Background()
	config := targetContainer.GetCreateConfig()
	hostConfig := targetContainer.GetCreateHostConfig()
	networkConfig := c.GetNetworkConfig(targetContainer)

	// simpleNetworkConfig is a networkConfig with only 1 network.
	// See: https://github.com/docker/docker/issues/29265
	simpleNetworkConfig := func() *network.NetworkingConfig {
		oneEndpoint := make(map[string]*network.EndpointSettings)
		for k, v := range networkConfig.EndpointsConfig {
			oneEndpoint[k] = v
			// We only need 1
			break
		}
		return &network.NetworkingConfig{EndpointsConfig: oneEndpoint}
	}

	name := targetContainer.Name()

	logrus.Infof("Creating %s", name)

	createdContainer, err := c.api.ContainerCreate(bg, config, hostConfig, simpleNetworkConfig(), nil, name)
	if err != nil {
		return "", err
	}

	if !(hostConfig.NetworkMode.IsHost()) {
		for k := range simpleNetworkConfig().EndpointsConfig {
			err = c.api.NetworkDisconnect(bg, k, createdContainer.ID, true)
			if err != nil {
				return "", err
			}
		}

		for k, v := range networkConfig.EndpointsConfig {
			err = c.api.NetworkConnect(bg, k, createdContainer.ID, v)
			if err != nil {
				return "", err
			}
		}
	}

	createdContainerID := types.ContainerID(createdContainer.ID)
	// Skip starting if the original wasn’t running and ReviveStopped is false.
	if !targetContainer.IsRunning() && !c.ReviveStopped {
		return createdContainerID, nil
	}

	return createdContainerID, c.doStartContainer(bg, targetContainer, createdContainer)
}

// doStartContainer starts a newly created container.
// It logs the action and returns an error if the start operation fails.
func (c dockerClient) doStartContainer(ctx context.Context, targetContainer types.Container, creation container.CreateResponse) error {
	name := targetContainer.Name()

	logrus.Debugf("Starting container %s (%s)", name, types.ContainerID(creation.ID).ShortID())
	err := c.api.ContainerStart(ctx, creation.ID, container.StartOptions{})
	if err != nil {
		return err
	}
	return nil
}

// RenameContainer renames an existing container to the specified new name.
// Logs the action and returns an error if the rename fails.
func (c dockerClient) RenameContainer(target types.Container, newName string) error {
	bg := context.Background()
	logrus.Debugf("Renaming container %s (%s) to %s", target.Name(), target.ID().ShortID(), newName)
	return c.api.ContainerRename(bg, string(target.ID()), newName)
}

// IsContainerStale determines if a container’s image is outdated.
// It pulls the latest image if needed and compares it to the current image.
// Returns whether the container is stale, the latest image ID, and any error encountered.
func (c dockerClient) IsContainerStale(target types.Container, params types.UpdateParams) (stale bool, latestImage types.ImageID, err error) {
	ctx := context.Background()

	if target.IsNoPull(params) {
		logrus.Debugf("Skipping image pull.")
	} else if err := c.PullImage(ctx, target); err != nil {
		return false, target.SafeImageID(), err
	}

	return c.HasNewImage(ctx, target)
}

// HasNewImage checks if a newer image exists for the container’s image name.
// Compares the current image ID with the latest available ID.
// Returns whether a new image exists, the latest image ID, and any error.
func (c dockerClient) HasNewImage(ctx context.Context, targetContainer types.Container) (hasNew bool, latestImage types.ImageID, err error) {
	currentImageID := types.ImageID(targetContainer.ContainerInfo().ContainerJSONBase.Image)
	imageName := targetContainer.ImageName()

	newImageInfo, err := c.api.ImageInspect(ctx, imageName)
	if err != nil {
		return false, currentImageID, err
	}

	newImageID := types.ImageID(newImageInfo.ID)
	if newImageID == currentImageID {
		logrus.Debugf("No new images found for %s", targetContainer.Name())
		return false, currentImageID, nil
	}

	logrus.Infof("Found new %s image (%s)", imageName, newImageID.ShortID())
	return true, newImageID, nil
}

// PullImage fetches the latest image for a container, optionally skipping if digest matches.
// It performs a HEAD request to compare digests and falls back to a full pull if needed.
// Returns an error if the pull fails or if the image is pinned (sha256).
func (c dockerClient) PullImage(ctx context.Context, targetContainer types.Container) error {
	containerName := targetContainer.Name()
	imageName := targetContainer.ImageName()

	fields := logrus.Fields{
		"image":     imageName,
		"container": containerName,
	}

	// Prevent pulling pinned images.
	if strings.HasPrefix(imageName, "sha256:") {
		return fmt.Errorf("container uses a pinned image, and cannot be updated by watchtower")
	}

	logrus.WithFields(fields).Debugf("Trying to load authentication credentials.")
	opts, err := registry.GetPullOptions(imageName)
	if err != nil {
		logrus.Debugf("Error loading authentication credentials %s", err)
		return err
	}
	if opts.RegistryAuth != "" {
		logrus.Debug("Credentials loaded")
	}

	logrus.WithFields(fields).Debugf("Checking if pull is needed")
	if match, err := digest.CompareDigest(targetContainer, opts.RegistryAuth); err != nil {
		headLevel := logrus.DebugLevel
		if c.WarnOnHeadPullFailed(targetContainer) {
			headLevel = logrus.WarnLevel
		}
		logrus.WithFields(fields).Logf(headLevel, "Could not do a head request for %q, falling back to regular pull.", imageName)
		logrus.WithFields(fields).Log(headLevel, "Reason: ", err)
	} else if match {
		logrus.Debug("No pull needed. Skipping image.")
		return nil
	} else {
		logrus.Debug("Digests did not match, doing a pull.")
	}

	logrus.WithFields(fields).Debugf("Pulling image")
	response, err := c.api.ImagePull(ctx, imageName, opts)
	if err != nil {
		logrus.Debugf("Error pulling image %s, %s", imageName, err)
		return err
	}

	defer response.Close()
	// Read the response fully to avoid aborting the pull prematurely.
	if _, err = io.ReadAll(response); err != nil {
		logrus.Error(err)
		return err
	}
	return nil
}

// RemoveImageByID deletes an image from the Docker host by its ID.
// Logs detailed removal info if debug logging is enabled.
// Returns an error if removal fails.
func (c dockerClient) RemoveImageByID(id types.ImageID) error {
	logrus.Infof("Removing image %s", id.ShortID())

	items, err := c.api.ImageRemove(
		context.Background(),
		string(id),
		image.RemoveOptions{
			Force: true,
		},
	)

	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		deleted := strings.Builder{}
		untagged := strings.Builder{}
		for _, item := range items {
			if item.Deleted != "" {
				if deleted.Len() > 0 {
					deleted.WriteString(`, `)
				}
				deleted.WriteString(types.ImageID(item.Deleted).ShortID())
			}
			if item.Untagged != "" {
				if untagged.Len() > 0 {
					untagged.WriteString(`, `)
				}
				untagged.WriteString(types.ImageID(item.Untagged).ShortID())
			}
		}
		fields := logrus.Fields{`deleted`: deleted.String(), `untagged`: untagged.String()}
		logrus.WithFields(fields).Debug("Image removal completed")
	}

	return err
}

// ExecuteCommand runs a command inside a container and evaluates its result.
// It creates an exec instance, runs it, and waits for completion or timeout.
// Returns whether to skip updates (based on exit code) and any error encountered.
func (c dockerClient) ExecuteCommand(containerID types.ContainerID, command string, timeout int) (SkipUpdate bool, err error) {
	ctx := context.Background()
	clog := logrus.WithField("containerID", containerID)

	// Configure and create the exec instance.
	execConfig := container.ExecOptions{
		Tty:    true,
		Detach: false,
		Cmd:    []string{"sh", "-c", command},
	}

	exec, err := c.api.ContainerExecCreate(ctx, string(containerID), execConfig)
	if err != nil {
		return false, err
	}

	// Attach to the exec to capture output.
	response, attachErr := c.api.ContainerExecAttach(ctx, exec.ID, container.ExecStartOptions{
		Tty:    true,
		Detach: false,
	})
	if attachErr != nil {
		clog.Errorf("Failed to extract command exec logs: %v", attachErr)
	}

	// Start the exec instance.
	execStartCheck := container.ExecStartOptions{Detach: false, Tty: true}
	err = c.api.ContainerExecStart(ctx, exec.ID, execStartCheck)
	if err != nil {
		return false, err
	}

	// Capture output if attachment succeeded.
	var output string
	if attachErr == nil {
		defer response.Close()
		var writer bytes.Buffer
		written, err := writer.ReadFrom(response.Reader)
		if err != nil {
			clog.Error(err)
		} else if written > 0 {
			output = strings.TrimSpace(writer.String())
		}
	}

	// Wait for completion and interpret the result.
	skipUpdate, err := c.waitForExecOrTimeout(ctx, exec.ID, output, timeout)
	if err != nil {
		return true, err
	}

	return skipUpdate, nil
}

// waitForExecOrTimeout waits for an exec instance to complete or times out.
// It checks the exit code: 75 (ExTempFail) skips updates, >0 indicates failure.
// Returns whether to skip updates and any error encountered.
func (c dockerClient) waitForExecOrTimeout(parentContext context.Context, ID string, execOutput string, timeout int) (SkipUpdate bool, err error) {
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
		execInspect, err := c.api.ContainerExecInspect(ctx, ID)

		// Log exec status for debugging.
		// goland:noinspection GoNilness
		logrus.WithFields(logrus.Fields{
			"exit-code":    execInspect.ExitCode,
			"exec-id":      execInspect.ExecID,
			"running":      execInspect.Running,
			"container-id": execInspect.ContainerID,
		}).Debug("Awaiting timeout or completion")

		if err != nil {
			return false, err
		}
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
			return false, fmt.Errorf("command exited with code %v  %s", execInspect.ExitCode, execOutput)
		}
		break
	}
	return false, nil
}

// waitForStopOrTimeout waits for a container to stop or times out.
// Returns true if stopped (or gone), false if still running after timeout, and any error.
// Treats a 404 (not found) as stopped, indicating successful removal or prior stop.
func (c dockerClient) waitForStopOrTimeout(targetContainer types.Container, waitTime time.Duration) (stopped bool, err error) {
	ctx := context.Background()
	timeout := time.After(waitTime)
	for {
		select {
		case <-timeout:
			return false, nil // Timed out, container still running
		default:
			ci, err := c.api.ContainerInspect(ctx, string(targetContainer.ID()))
			if err != nil {
				if client.IsErrNotFound(err) {
					return true, nil // Container gone, treat as stopped
				}
				return false, err // Other errors propagate
			}
			if !ci.State.Running {
				return true, nil // Stopped successfully
			}
		}
		time.Sleep(1 * time.Second)
	}
}
