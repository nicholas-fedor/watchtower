package container

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/afero"

	cerrdefs "github.com/containerd/errdefs"
	dockerTypes "github.com/docker/docker/api/types"
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
)

// Errors for container health operations.
var (
	// errHealthCheckTimeout indicates that waiting for a container to become healthy timed out.
	errHealthCheckTimeout = errors.New("timeout waiting for container to become healthy")
	// errHealthCheckFailed indicates that a container's health check failed.
	errHealthCheckFailed = errors.New("container health check failed")
)

// client is the concrete implementation of the Client interface.
//
// It wraps the Docker API client and applies custom behavior via ClientOptions.
type client struct {
	api dockerClient.APIClient
	ClientOptions
	registryConfig *types.RegistryConfig
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
func NewClient(opts ClientOptions) types.Client {
	ctx := context.Background()

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
func (c *client) ListContainers(filter types.Filter) ([]types.Container, error) {
	// Determine if the container runtime is Podman to handle runtime-specific differences.
	isPodman := c.getPodmanFlag()

	// Attempt to list source containers and handle errors by logging and returning them.
	containers, err := ListSourceContainers(c.api, c.ClientOptions, filter, isPodman)
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
func (c *client) GetContainer(containerID types.ContainerID) (types.Container, error) {
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
func (c *client) StopContainer(container types.Container, timeout time.Duration) error {
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
func (c *client) StartContainer(container types.Container) (types.ContainerID, error) {
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}
	// Determine if the container runtime is Podman to handle runtime-specific differences.
	isPodman := c.getPodmanFlag()

	clientVersion := c.GetVersion()

	logrus.WithFields(fields).WithField("client_version", clientVersion).
		Debug("Obtaining source container network configuration")

	// Get unified network config.
	networkConfig := getNetworkConfig(container, clientVersion)

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

	logrus.WithFields(fields).
		WithField("new_id", newID.ShortID()).
		Debug("Started new container")

	return newID, nil
}

// ListAllContainers retrieves a list of all containers from the Docker host, regardless of status.
//
// Returns:
//   - []types.Container: List of all containers.
//   - error: Non-nil if listing fails, nil on success.
func (c *client) ListAllContainers() ([]types.Container, error) {
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
			// Handle race condition where containers disappear between API calls
			if cerrdefs.IsNotFound(err) {
				logrus.WithField("container_id", runningContainer.ID).
					Debug("Container no longer exists")

				continue
			}

			return nil, err
		}

		hostContainers = append(hostContainers, container)
	}

	clog.WithField("count", len(hostContainers)).Debug("Listed all containers")

	return hostContainers, nil
}

// UpdateContainer updates the configuration of an existing container.
//
// Parameters:
//   - container: Container to update.
//   - config: Update configuration containing the changes to apply.
//
// Returns:
//   - error: Non-nil if update fails, nil on success.
func (c *client) UpdateContainer(
	container types.Container,
	config dockerContainer.UpdateConfig,
) error {
	ctx := context.Background()
	clog := logrus.WithField("container_id", container.ID().ShortID())

	clog.Debug("Updating container configuration")

	_, err := c.api.ContainerUpdate(ctx, string(container.ID()), config)
	if err != nil {
		clog.WithError(err).Debug("Failed to update container")

		return fmt.Errorf("failed to update container %s: %w", container.ID().ShortID(), err)
	}

	clog.Debug("Container configuration updated")

	return nil
}

// RenameContainer renames an existing container to a new name.
//
// Parameters:
//   - container: Container to rename.
//   - newName: New name for the container.
//
// Returns:
//   - error: Non-nil if rename fails, nil on success.
func (c *client) RenameContainer(container types.Container, newName string) error {
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
func (c *client) WarnOnHeadPullFailed(container types.Container) bool {
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
func (c *client) IsContainerStale(
	container types.Container,
	params types.UpdateParams,
) (bool, types.ImageID, error) {
	// Use image client to perform staleness check.
	imgClient := newImageClient(c.api, c.registryConfig)

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
func (c *client) ExecuteCommand(
	container types.Container,
	command string,
	timeout int,
	uid int,
	gid int,
) (bool, error) {
	ctx := context.Background()
	clog := logrus.WithField("container_id", container.ID().ShortID())

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
func (c *client) captureExecOutput(ctx context.Context, execID string) (string, error) {
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
func (c *client) waitForExecOrTimeout(
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
//   - imageName: Name of the image to remove (for logging purposes).
//
// Returns:
//   - error: Non-nil if removal fails, nil on success.
func (c *client) RemoveImageByID(imageID types.ImageID, imageName string) error {
	// Use image client to remove the image.
	imgClient := newImageClient(c.api, c.registryConfig)

	err := imgClient.RemoveImageByID(imageID, imageName)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"image_id":   imageID.ShortID(),
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
func (c *client) GetVersion() string {
	return strings.Trim(c.api.ClientVersion(), "\"")
}

// GetInfo returns system information from the Docker daemon.
//
// Returns:
//   - types.SystemInfo: System information.
//   - error: Non-nil if retrieval fails, nil on success.
func (c *client) GetInfo() (types.SystemInfo, error) {
	ctx := context.Background()

	info, err := c.api.Info(ctx)
	if err != nil {
		logrus.WithError(err).Debug("Failed to get system info")

		return types.SystemInfo{}, fmt.Errorf("failed to get system info: %w", err)
	}

	// Convert to SystemInfo struct
	var registryConfig *types.RegistryConfig

	if info.RegistryConfig != nil {
		insecureCIDRs := make([]string, len(info.RegistryConfig.InsecureRegistryCIDRs))
		for i, cidr := range info.RegistryConfig.InsecureRegistryCIDRs {
			insecureCIDRs[i] = cidr.String()
		}

		registryConfig = &types.RegistryConfig{
			Mirrors:               info.RegistryConfig.Mirrors,
			InsecureRegistryCIDRs: insecureCIDRs,
		}
	}

	systemInfo := types.SystemInfo{
		Name:            info.Name,
		ServerVersion:   info.ServerVersion,
		OSType:          info.OSType,
		OperatingSystem: info.OperatingSystem,
		Driver:          info.Driver,
		RegistryConfig:  registryConfig,
	}

	// Cache the registry config for mirror support
	c.registryConfig = registryConfig

	return systemInfo, nil
}

// GetServerVersion returns version information from the Docker daemon.
//
// Returns:
//   - types.VersionInfo: Version information.
//   - error: Non-nil if retrieval fails, nil on success.
func (c *client) GetServerVersion() (types.VersionInfo, error) {
	ctx := context.Background()

	version, err := c.api.ServerVersion(ctx)
	if err != nil {
		logrus.WithError(err).Debug("Failed to get server version")

		return types.VersionInfo{}, fmt.Errorf("failed to get server version: %w", err)
	}

	// Convert to VersionInfo struct
	versionInfo := types.VersionInfo{
		Version:       version.Version,
		APIVersion:    version.APIVersion,
		MinAPIVersion: version.MinAPIVersion,
		GitCommit:     version.GitCommit,
		GoVersion:     version.GoVersion,
		Os:            version.Os,
		Arch:          version.Arch,
		KernelVersion: version.KernelVersion,
		Experimental:  version.Experimental,
		BuildTime:     version.BuildTime,
	}

	return versionInfo, nil
}

// GetDiskUsage returns disk usage information from the Docker daemon.
//
// Returns:
//   - types.DiskUsage: Disk usage statistics.
//   - error: Non-nil if retrieval fails, nil on success.
func (c *client) GetDiskUsage() (types.DiskUsage, error) {
	ctx := context.Background()

	usage, err := c.api.DiskUsage(ctx, dockerTypes.DiskUsageOptions{})
	if err != nil {
		logrus.WithError(err).Debug("Failed to get disk usage")

		return types.DiskUsage{}, fmt.Errorf("failed to get disk usage: %w", err)
	}

	// Convert to types.DiskUsage
	diskUsage := types.DiskUsage{
		LayersSize: usage.LayersSize,
	}

	// Convert images
	if usage.Images != nil {
		diskUsage.Images = make([]types.ImageSummary, len(usage.Images))
		for i, img := range usage.Images {
			diskUsage.Images[i] = types.ImageSummary{
				ID:          img.ID,
				ParentID:    img.ParentID,
				RepoTags:    img.RepoTags,
				RepoDigests: img.RepoDigests,
				Created:     img.Created,
				Size:        img.Size,
				SharedSize:  img.SharedSize,
				VirtualSize: img.Size, // Use Size instead of deprecated VirtualSize
				Labels:      img.Labels,
				Containers:  img.Containers,
			}
		}
	}

	// Convert containers
	if usage.Containers != nil {
		diskUsage.Containers = make([]types.ContainerSummary, len(usage.Containers))
		for i, cont := range usage.Containers {
			diskUsage.Containers[i] = types.ContainerSummary{
				ID:         cont.ID,
				Names:      cont.Names,
				Image:      cont.Image,
				ImageID:    cont.ImageID,
				Command:    cont.Command,
				Created:    cont.Created,
				SizeRw:     cont.SizeRw,
				SizeRootFs: cont.SizeRootFs,
				Labels:     cont.Labels,
				State:      cont.State,
				Status:     cont.Status,
			}
			// Convert ports
			if cont.Ports != nil {
				diskUsage.Containers[i].Ports = make([]types.Port, len(cont.Ports))
				for j, port := range cont.Ports {
					diskUsage.Containers[i].Ports[j] = types.Port{
						IP:          port.IP,
						PrivatePort: port.PrivatePort,
						PublicPort:  port.PublicPort,
						Type:        port.Type,
					}
				}
			}
		}
	}

	// Convert volumes
	if usage.Volumes != nil {
		diskUsage.Volumes = make([]types.VolumeSummary, len(usage.Volumes))
		for i, vol := range usage.Volumes {
			diskUsage.Volumes[i] = types.VolumeSummary{
				Name:       vol.Name,
				Driver:     vol.Driver,
				Mountpoint: vol.Mountpoint,
				CreatedAt:  vol.CreatedAt,
				Status:     vol.Status,
				Labels:     vol.Labels,
				Scope:      vol.Scope,
				Options:    vol.Options,
			}
			if vol.UsageData != nil {
				diskUsage.Volumes[i].UsageData = &types.VolumeUsageData{
					Size:     vol.UsageData.Size,
					RefCount: vol.UsageData.RefCount,
				}
			}
		}
	}

	return diskUsage, nil
}

// GetTotalDiskUsage returns the total disk usage from the Docker daemon.
//
// Returns:
//   - int64: Total disk usage size.
//   - error: Non-nil if retrieval fails, nil on success.
func (c *client) GetTotalDiskUsage() (int64, error) {
	diskUsage, err := c.GetDiskUsage()
	if err != nil {
		return 0, err
	}

	// Calculate total disk usage including all components
	totalSize := diskUsage.LayersSize

	// Add container disk usage
	for _, container := range diskUsage.Containers {
		totalSize += container.SizeRw + container.SizeRootFs
	}

	// Add volume disk usage
	for _, volume := range diskUsage.Volumes {
		if volume.UsageData != nil {
			totalSize += volume.UsageData.Size
		}
	}

	// Add build cache disk usage
	for _, cache := range diskUsage.BuildCache {
		totalSize += cache.Size
	}

	return totalSize, nil
}

// detectPodman determines if the container runtime is Podman using multiple detection methods.
//
// Returns:
//   - bool: True if Podman is detected, false otherwise.
//   - error: Non-nil if detection fails, nil on success.
func (c *client) detectPodman() (bool, error) {
	// Check for Podman marker file
	if _, err := c.Fs.Stat("/run/.containerenv"); err == nil {
		logrus.Debug("Detected Podman via marker file /run/.containerenv")

		return true, nil
	}

	// Check for Docker marker file (ensure we're not in Docker)
	if _, err := c.Fs.Stat("/.dockerenv"); err == nil {
		logrus.Debug("Detected Docker via marker file /.dockerenv")

		return false, nil
	}

	// Check CONTAINER environment variable
	if container := os.Getenv("CONTAINER"); container == "podman" || container == "oci" {
		logrus.Debug("Detected Podman via CONTAINER environment variable")

		return true, nil
	}

	// Fallback to API-based detection
	info, err := c.GetInfo()
	if err != nil {
		logrus.WithError(err).
			Debug("Failed to get system info for Podman detection, assuming Docker")

		return false, err
	}

	if info.Name == "podman" {
		logrus.Debug("Detected Podman via API Name field")

		return true, nil
	} else if strings.Contains(strings.ToLower(info.ServerVersion), "podman") {
		logrus.Debug("Detected Podman via API ServerVersion field")

		return true, nil
	}

	logrus.Debug("No Podman detection criteria met, assuming Docker")

	return false, nil
}

// getPodmanFlag determines if Podman detection is needed and performs it.
//
// Returns:
//   - bool: True if Podman is detected, false otherwise.
func (c *client) getPodmanFlag() bool {
	// Only perform detection in auto mode; otherwise, assume Docker
	if c.CPUCopyMode != CPUCopyModeAuto {
		return false
	}

	// Attempt to detect Podman using various methods (marker files, env vars, API info)
	isPodman, err := c.detectPodman()
	if err != nil {
		// On detection failure, fall back to assuming Docker
		logrus.WithError(err).Debug("Failed to detect container runtime, falling back to Docker")

		return false
	}

	// Return the detection result
	return isPodman
}

// WaitForContainerHealthy waits for a container to become healthy or times out.
//
// Parameters:
//   - containerID: ID of the container to wait for.
//   - timeout: Maximum duration to wait for health.
//
// Returns:
//   - error: Non-nil if timeout is reached or inspection fails, nil if healthy or no health check.
func (c *client) WaitForContainerHealthy(
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
