package container

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/moby/moby/api/types/container"
	dockerClient "github.com/moby/moby/client"
	dockerAPIVersion "github.com/moby/moby/client/pkg/versions"

	"github.com/nicholas-fedor/watchtower/internal/container"
	containerImage "github.com/nicholas-fedor/watchtower/internal/container/image"
	"github.com/nicholas-fedor/watchtower/internal/container/network"
	"github.com/nicholas-fedor/watchtower/internal/container/oci"
	"github.com/nicholas-fedor/watchtower/internal/container/recreate"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	registryConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/registry"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Constants for CPUCopyMode values.
const (
	// cpuCopyModeAuto indicates automatic detection of container runtime for
	// CPU copying behavior.
	cpuCopyModeAuto = "auto"

	// daemonInitTimeout is the default timeout for Docker daemon initialization operations.
	// This timeout applies to initial connection, ping, API version negotiation, and
	// server version retrieval during client initialization.
	// The value is 30 seconds, which should be sufficient for most Docker daemon
	// initialization scenarios while preventing indefinite hangs.
	daemonInitTimeout = 30 * time.Second

	// maxListRetries is the maximum number of retry attempts for transient Docker
	// connection failures when listing containers.
	maxListRetries = 3
)

// baseListRetryDelay is the base delay for exponential backoff between retries.
// Actual delays are baseListRetryDelay * 2^attempt (5s, 10s).
var baseListRetryDelay = 5 * time.Second

// Errors for container health operations.
var (
	// errHealthCheckTimeout indicates that waiting for a container to become healthy timed out.
	errHealthCheckTimeout = errors.New("timeout waiting for container to become healthy")
	// errHealthCheckCanceled indicates that waiting for a container to become healthy was canceled.
	errHealthCheckCanceled = errors.New("context canceled while waiting for container to become healthy")
	// errHealthCheckFailed indicates that a container's health check failed.
	errHealthCheckFailed = errors.New("container health check failed")
	// errRecreateDockerClient indicates failure recreating the Docker client after API version fallback.
	errRecreateDockerClient = errors.New("failed to recreate Docker client")
)

// nopLog returns a fresh discarded logger for optional construction and fallbacks.
// Each call allocates a new Nop instance.
func nopLog() *zerolog.Logger {
	n := zerolog.Nop()

	return &n
}

// Client defines the interface for interacting with the Docker API within Watchtower.
//
// It provides methods for managing containers, images, and executing commands,
// abstracting the underlying Docker client operations.
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

	// GetCurrentWatchtowerContainer retrieves minimal container information
	// for the specified container ID, skipping image inspection.
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

	// StopAndRemoveContainer stops and removes a specified container,
	// respecting the given timeout.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - container: Container to stop and remove.
	//   - timeout: Duration to wait before forcing stop.
	//
	// Returns:
	//   - error: Non-nil if stop/removal fails, nil on success.
	StopAndRemoveContainer(ctx context.Context, container types.Container, timeout time.Duration) error

	// CreateContainer creates a new container based on the provided
	// container's configuration, but does not start it.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - container: Source container to replicate.
	//
	// Returns:
	//   - types.ContainerID: ID of the new container.
	//   - error: Non-nil if creation fails, nil on success.
	CreateContainer(ctx context.Context, container types.Container) (types.ContainerID, error)

	// StartContainer creates and starts a new container based on the provided
	// container's configuration.
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

	// IsContainerStale checks if a container's image is outdated compared to
	// the latest available version.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - container: Container to check.
	//   - params: Update parameters for staleness check.
	//
	// Returns:
	//   - bool: True if image is stale, false otherwise.
	//   - types.ImageID: Latest image ID.
	//   - string: Latest registry manifest digest (empty if unavailable).
	//   - error: Non-nil if check fails, nil on success.
	IsContainerStale(
		ctx context.Context,
		container types.Container,
		params types.UpdateParams,
	) (bool, types.ImageID, string, error)

	// CheckContainerUpdate reports whether a newer image is available without
	// pulling image layers. When NoPull is active it inspects the local cache
	// only. Otherwise, it compares registry digests. Cooldown is not applied.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - container: Container to check.
	//   - params: Update parameters (NoPull, LabelPrecedence).
	//
	// Returns:
	//   - bool: True if an update is available, false otherwise.
	//   - types.ImageID: Latest local image ID when known (empty for remote-only mismatch).
	//   - string: Latest registry manifest digest (empty if unavailable).
	//   - error: Non-nil if the check fails, nil on success.
	CheckContainerUpdate(
		ctx context.Context,
		container types.Container,
		params types.UpdateParams,
	) (bool, types.ImageID, string, error)

	// GetImageAnnotations reads the OCI annotations of an image by reference.
	//
	// The read is a local Docker daemon inspect, so it never counts against a
	// registry rate limit. It resolves the new image's annotations after a pull,
	// which the running container's inspect cannot provide. An unreadable image
	// yields empty annotations rather than an error, because metadata must never
	// fail a session.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - imageRef: Image reference, tag or digest, to inspect.
	//
	// Returns:
	//   - oci.Annotations: Annotations from the image config labels, or empty.
	GetImageAnnotations(ctx context.Context, imageRef string) oci.Annotations

	// ExecuteCommand runs a command inside a container and returns whether
	// to skip updates based on the result.
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

	// GetVersion returns the client's API version.
	//
	// Returns:
	//   - string: Docker API version (e.g., "1.44").
	GetVersion() string

	// GetImageDiskUsage returns Docker image storage usage from the daemon.
	//
	// It queries GET /system/df for images only and returns the daemon's
	// aggregated totals. Shared layers are not double-counted.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//
	// Returns:
	//   - types.ImageDiskUsage: Aggregated image usage.
	//   - error: Non-nil if retrieval fails, nil on success.
	GetImageDiskUsage(ctx context.Context) (types.ImageDiskUsage, error)

	// GetInfo returns system information from the Docker daemon.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//
	// Returns:
	//   - types.SystemInfo: Daemon identity fields used for runtime detection.
	//   - error: Non-nil if retrieval fails, nil on success.
	GetInfo(ctx context.Context) (types.SystemInfo, error)

	// Ping verifies connectivity to the Docker daemon.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//
	// Returns:
	//   - error: Non-nil if the daemon is unreachable, nil on success.
	Ping(ctx context.Context) error

	// WaitForContainerHealthy waits for a container to become healthy or times out.
	//
	// The timeout parameter controls the maximum wait duration:
	//   - timeout > 0: Wait up to the specified duration for the container to become healthy.
	//   - timeout <= 0: Wait indefinitely until the container becomes healthy, unhealthy,
	//     or the context is cancelled.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - containerID: ID of the container to wait for.
	//   - timeout: Maximum duration to wait for health. Zero or negative values cause
	//     indefinite waiting.
	//
	// Returns:
	//   - error: Non-nil if timeout is reached, container becomes unhealthy, or inspection
	//     fails. nil if healthy or no health check is configured.
	WaitForContainerHealthy(ctx context.Context, containerID types.ContainerID, timeout time.Duration) error

	// SetRestartPolicy updates a container's restart policy.
	//
	// An empty policy name is treated as no automatic restart (Docker "no").
	// MaximumRetryCount is cleared in that case because retry counts apply only
	// to on-failure policies. Only the restart policy is sent to the Engine.
	// Resource limits are left unchanged. Failures are logged and do not abort
	// the caller.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - container: Container whose restart policy should be updated.
	//   - policy: Restart policy to apply. Empty Name disables automatic restart.
	SetRestartPolicy(
		ctx context.Context,
		container types.Container,
		policy dockerContainer.RestartPolicy,
	)

	// RemoveContainer removes a container from the Docker host.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - container: Container to remove.
	//
	// Returns:
	//   - error: Non-nil if removal fails, nil on success.
	RemoveContainer(ctx context.Context, container types.Container) error

	// CreateEphemeralOrchestrator creates a short-lived container that orchestrates
	// the Watchtower self-update transition.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - sourceContainer: The current Watchtower container being replaced.
	//   - newImage: The image reference for the new Watchtower container.
	//   - containerChain: The container chain label for lineage tracking.
	//   - cleanup: When true, the orchestrator removes the old image after handoff.
	//
	// Returns:
	//   - types.ContainerID: ID of the ephemeral orchestrator container.
	//   - error: Non-nil if creation or start fails, nil on success.
	CreateEphemeralOrchestrator(
		ctx context.Context,
		sourceContainer types.Container,
		newImage string,
		containerChain string,
		cleanup bool,
	) (types.ContainerID, error)

	// StartContainerByID starts a container by its ID directly.
	//
	// Unlike StartContainer, this does not check the reviveStopped option or
	// create a new container from a source configuration. It directly starts
	// an existing, already-created container. This is used by the ephemeral
	// orchestrator to start the new container after the old one has been stopped.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - containerID: ID of the container to start.
	//
	// Returns:
	//   - error: Non-nil if starting fails, nil on success.
	StartContainerByID(ctx context.Context, containerID types.ContainerID) error

	// BuildRemoteImage builds an image from a Git URL context.
	//
	// The daemon clones the repository. Watchtower does not.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control.
	//   - remote: Docker Git context URL (https://host/repo.git#commit[:subdir]).
	//   - dockerfile: Dockerfile path relative to that context. Empty means Dockerfile.
	//   - tags: Image tags to apply to the built image.
	//
	// Returns:
	//   - types.ImageID: ID of the built image.
	//   - error: Non-nil if the build fails, nil on success.
	BuildRemoteImage(ctx context.Context, remote, dockerfile string, tags []string) (types.ImageID, error)
}

// client is the concrete implementation of the Client interface.
//
// It wraps the Docker API client and applies custom behavior via ClientOptions.
type client struct {
	ClientOptions

	api dockerClient.APIClient
	log *zerolog.Logger

	// runtimeMu protects the cached runtime detection result.
	runtimeMu sync.Mutex
	// runtimeDetected is true once the container runtime was detected successfully.
	runtimeDetected bool
	// isPodman caches the result of Podman runtime detection.
	isPodman bool

	// copyFiles holds labeled file snapshots between stop and create of a recreation.
	copyFiles pendingCopyFiles
}

// ClientOptions configures container management behavior for the Docker client.
type ClientOptions struct {
	RemoveVolumes           bool
	IncludeStopped          bool
	ReviveStopped           bool
	IncludeRestarting       bool
	DisableMemorySwappiness bool
	CPUCopyMode             string
	WarnOnHeadFailed        containerImage.WarningStrategy
	// Fs is the filesystem rooted at "/" that holds the container runtime
	// marker files. NewClient uses the host filesystem when it is nil.
	Fs fs.FS
	// Registry holds the settings for registry digest and image age requests.
	Registry registryConfig.Registry
	// RateLimiter paces image pulls, digest checks, and image age requests per
	// registry host. NewClient creates one when it is nil.
	RateLimiter *ratelimit.Limiter
}

// NewClient initializes a new Client instance for Docker API interactions.
//
// It configures the client using environment variables (e.g., DOCKER_HOST, DOCKER_API_VERSION)
// and validates the API version, falling back to autonegotiation if necessary.
//
// Parameters:
//   - opts: Options to customize container management behavior.
//
// Returns:
//   - Client: Initialized client instance (exits on failure).
func NewClient(log *zerolog.Logger, opts ClientOptions) Client {
	// Initialize client from environment. FromEnv reads DOCKER_HOST, DOCKER_TLS_VERIFY,
	// DOCKER_CERT_PATH, and DOCKER_API_VERSION. When DOCKER_API_VERSION is set, the client
	// uses that fixed version. Otherwise, it defaults to the maximum supported version
	// with automatic negotiation enabled.
	//
	// Construction does not use a context. A timeout is applied only to the subsequent
	// ping and API negotiation step.
	cli, err := dockerClient.New(dockerClient.FromEnv)
	if err != nil {
		log.Fatal().
			Err(err).
			Msg("Failed to initialize Docker client")
	}

	// Set default filesystem if not provided.
	if opts.Fs == nil {
		opts.Fs = os.DirFS("/")
	}

	if opts.RateLimiter == nil {
		opts.RateLimiter = ratelimit.New()
	}

	cli, result, err := negotiateDockerAPI(log, cli)
	if err != nil {
		log.Fatal().
			Err(err).
			Msg("Failed to recreate Docker client")
	}

	log.Debug().
		Str("client_version", cli.ClientVersion()).
		Str("server_version", result.APIVersion).
		Msg("Initialized Docker client")

	return &client{
		api:           cli,
		log:           log,
		ClientOptions: opts,
	}
}

// negotiateDockerAPI pings the daemon to validate connectivity and negotiate the API version.
//
// When DOCKER_API_VERSION is incompatible with the server, it recreates the client without a
// fixed version so negotiation can select a supported API. A fixed version is incompatible when
// the ping fails with a not-found error, or when it is newer than the API version the daemon
// reports. Other ping failures are non-fatal: the client is returned and version negotiation is
// deferred until the first API call.
//
// Parameters:
//   - log: Process logger.
//   - cli: Docker client created from the environment.
//
// Returns:
//   - *dockerClient.Client: Client to use for subsequent API calls (possibly recreated).
//   - dockerClient.PingResult: Result of a successful ping, or zero when ping failed.
//   - error: Non-nil only when the client cannot be recreated after an incompatible API version.
func negotiateDockerAPI(
	log *zerolog.Logger,
	cli *dockerClient.Client,
) (*dockerClient.Client, dockerClient.PingResult, error) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		daemonInitTimeout,
	)
	defer cancel()

	// Ping triggers API version negotiation, or only pings when DOCKER_API_VERSION fixes the
	// version.
	result, err := cli.Ping(ctx, dockerClient.PingOptions{NegotiateAPIVersion: true})

	// The ping uses the unversioned endpoint, so a daemon older than the fixed version still
	// answers it. A negotiated version never exceeds the daemon's.
	tooNew := err == nil && result.APIVersion != "" &&
		dockerAPIVersion.GreaterThan(cli.ClientVersion(), result.APIVersion)

	if cerrdefs.IsNotFound(err) || tooNew {
		log.Warn().
			Err(err).
			Str("api_version", cli.ClientVersion()).
			Str("server_api_version", result.APIVersion).
			Msg("DOCKER_API_VERSION incompatible with server. Falling back to autonegotiation")

		// FromEnv would read DOCKER_API_VERSION again, so the client is recreated from the
		// host and TLS settings alone.
		recreated, recreateErr := dockerClient.New(
			dockerClient.WithTLSClientConfigFromEnv(),
			dockerClient.WithHostFromEnv(),
		)
		if recreateErr != nil {
			return cli, dockerClient.PingResult{}, fmt.Errorf("%w: %w", errRecreateDockerClient, recreateErr)
		}

		cli = recreated
		result, err = cli.Ping(ctx, dockerClient.PingOptions{NegotiateAPIVersion: true})
	}

	if err != nil {
		log.Warn().
			Err(err).
			Msg("Ping failed during initialization. Will negotiate on first API call")

		return cli, dockerClient.PingResult{}, nil
	}

	return cli, result, nil
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
func (c *client) ListContainers(ctx context.Context, filter ...types.Filter) ([]types.Container, error) {
	// Podman has no restarting status, so the runtime only matters when
	// restarting containers are included.
	isPodman := c.IncludeRestarting && c.getRuntime(ctx)

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

	// Attempt to list source containers with retry for transient Docker connection failures.
	// The Docker daemon may become temporarily unreachable (e.g., during host maintenance),
	// so retrying with exponential backoff (5s then 10s between attempts) improves resilience.
	var (
		containers []types.Container
		err        error
	)

	for attempt := range maxListRetries {
		containers, err = listSourceContainers(c.logger(),
			ctx,
			c.api,
			c.ClientOptions,
			containerFilter,
			isPodman,
		)
		if err == nil {
			break
		}

		// Only retry for transient connection errors. Fail immediately for others.
		if !isDaemonConnectionError(err) {
			break
		}

		// Don't sleep after the last attempt.
		if attempt == maxListRetries-1 {
			break
		}

		delay := baseListRetryDelay * time.Duration(1<<attempt)

		c.logger().Warn().
			Int("attempt", attempt+1).
			Int("max", maxListRetries).
			Dur("delay", delay).
			Msg("Docker daemon unavailable, retrying container list...")

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, fmt.Errorf("context canceled during container list retry: %w", ctx.Err())
		}
	}

	if err != nil {
		c.logger().Debug().
			Err(err).
			Msg("Failed to list containers")

		return nil, err
	}

	c.logger().Debug().
		Int("count", len(containers)).
		Msg("Listed containers")

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
func (c *client) GetContainer(ctx context.Context, containerID types.ContainerID) (types.Container, error) {
	// Retrieve container details using helper function.
	container, err := getSourceContainer(c.logger(), ctx, c.api, containerID, nil)
	if err != nil {
		c.logger().Debug().
			Err(err).
			Str("container_id", string(containerID)).
			Msg("Failed to get container")

		return nil, err
	}

	if !container.HasImageInfo() {
		c.logger().Warn().
			Str("container", container.Name()).
			Str("container_id", string(containerID)).
			Str("image", container.ImageName()).
			Msg("Failed to retrieve image info")
	}

	c.logger().Debug().
		Str("container_id", string(containerID)).
		Msg("Retrieved container details")

	return container, nil
}

// GetCurrentWatchtowerContainer retrieves container information for the
// specified container ID, skipping image inspection.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - containerID: ID of the container to retrieve.
//
// Returns:
//   - types.Container: Container with imageInfo set to nil.
//   - error: Non-nil if inspection fails, nil on success.
func (c *client) GetCurrentWatchtowerContainer(
	ctx context.Context,
	containerID types.ContainerID,
) (types.Container, error) {
	clogVal := c.logger().With().
		Str("container_id", string(containerID)).
		Logger()
	clog := &clogVal

	clog.Debug().Msg("Inspecting current Watchtower container")

	containerInfo, err := c.api.ContainerInspect(
		ctx,
		string(containerID),
		dockerClient.ContainerInspectOptions{},
	)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to inspect current Watchtower container")

		return nil, fmt.Errorf("%w: %w", errInspectContainerFailed, err)
	}

	clog.Debug().Msg("Retrieved minimal container info")

	return container.NewContainer(c.logger(), &containerInfo.Container, nil), nil
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
func (c *client) StopContainer(ctx context.Context, container types.Container, timeout time.Duration) error {
	// Stop container using helper function.
	err := stopSourceContainer(c.logger(), ctx, c.api, container, timeout)
	if err != nil {
		c.logger().Debug().
			Err(err).
			Str("container", container.Name()).
			Str("image", container.ImageName()).
			Msg("Failed to stop container")

		return err
	}

	c.logger().Debug().
		Str("container", container.Name()).
		Str("image", container.ImageName()).
		Msg("Stopped container")

	return nil
}

// StopAndRemoveContainer stops and removes a specified container.
//
// AutoRemove containers that were running are left for Docker to delete after
// stop. Non-running AutoRemove containers are removed explicitly so the name
// is available for recreation.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - container: Container to stop and remove.
//   - timeout: Duration to wait before forcing stop.
//
// Returns:
//   - error: Non-nil if stop/removal fails, nil on success.
func (c *client) StopAndRemoveContainer(ctx context.Context, container types.Container, timeout time.Duration) error {
	// Stop and remove container using helper function with volume option.
	err := stopAndRemoveSourceContainer(c.logger(),
		ctx,
		c.api,
		container,
		timeout,
		c.RemoveVolumes,
	)
	if err != nil {
		c.logger().Debug().
			Err(err).
			Str("container", container.Name()).
			Str("image", container.ImageName()).
			Msg("Failed to stop and remove container")

		return err
	}

	c.logger().Debug().
		Str("container", container.Name()).
		Str("image", container.ImageName()).
		Msg("Stopped and removed container")

	return nil
}

// CreateContainer creates a new container based on the provided
// container's configuration, but does not start it.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - container: Source container to replicate.
//
// Returns:
//   - types.ContainerID: ID of the new container.
//   - error: Non-nil if creation fails, nil on success.
func (c *client) CreateContainer(ctx context.Context, container types.Container) (types.ContainerID, error) {
	defer c.DiscardCopyFiles(container.ID())

	clogVal := c.logger().With().
		Str("container", container.Name()).
		Str("image", container.ImageName()).
		Logger()
	clog := &clogVal
	// Only the auto CPU copy mode depends on the runtime.
	isPodman := c.CPUCopyMode == cpuCopyModeAuto && c.getRuntime(ctx)

	clientVersion := c.GetVersion()

	clog.Debug().
		Str("client_version", clientVersion).
		Msg("Obtaining source container network configuration")

	networkConfig := network.FromContainer(c.logger(), container, clientVersion)

	newID, err := recreate.CreateTarget(c.logger(),
		ctx,
		c.api,
		container,
		networkConfig,
		clientVersion,
		c.DisableMemorySwappiness,
		c.CPUCopyMode,
		isPodman,
	)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to create new container")

		return "", fmt.Errorf("create replacement container: %w", err)
	}

	err = c.injectStoredCopyFiles(ctx, container, newID)
	if err != nil {
		clog.Debug().
			Err(err).
			Str("new_id", newID.ShortID()).
			Msg("Failed to copy files into new container")

		cleanupFailedStartContainer(ctx, c.api, clog, newID)

		return "", err
	}

	clog.Debug().
		Str("new_id", newID.ShortID()).
		Msg("Created new container")

	return newID, nil
}

// StartContainer creates and starts a new container based on an
// existing container's configuration.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - container: Source container to replicate.
//
// Returns:
//   - types.ContainerID: ID of the new container.
//   - error: Non-nil if creation/start fails, nil on success.
func (c *client) StartContainer(ctx context.Context, container types.Container) (types.ContainerID, error) {
	clogVal := c.logger().With().
		Str("container", container.Name()).
		Str("image", container.ImageName()).
		Logger()
	clog := &clogVal

	newID, err := c.CreateContainer(ctx, container)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to start new container")

		return "", err
	}

	err = startTargetContainer(ctx, c.api, clog, container, newID, c.ReviveStopped)
	if err != nil {
		return "", err
	}

	return newID, nil
}

// StartContainerByID starts a container by its ID directly.
//
// Unlike StartContainer, this does not create a new container from a source
// configuration. It directly starts an existing, already-created container
// using the Docker API's ContainerStart method. Callers are responsible for
// checking whether the container should be started (e.g., the ReviveStopped
// and NoRestart checks in restartStaleContainer).
//
// Callers use it to start a created replacement, to recover an orphaned
// Watchtower container, and, in the ephemeral orchestrator, to start the new
// container or restart the old one when a self-update fails.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - containerID: ID of the container to start.
//
// Returns:
//   - error: Non-nil if starting fails, nil on success.
func (c *client) StartContainerByID(
	ctx context.Context,
	containerID types.ContainerID,
) error {
	clogVal := c.logger().With().
		Str("container_id", containerID.ShortID()).
		Logger()
	clog := &clogVal

	clog.Debug().Msg("Starting container by ID")

	_, err := c.api.ContainerStart(
		ctx,
		string(containerID),
		dockerClient.ContainerStartOptions{},
	)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to start container by ID")

		return fmt.Errorf("failed to start container %s: %w", containerID.ShortID(), err)
	}

	clog.Debug().Msg("Container started successfully")

	return nil
}

// SetRestartPolicy updates a container's restart policy.
//
// An empty policy name is treated as no automatic restart (Docker "no").
// MaximumRetryCount is cleared in that case because retry counts apply only
// to on-failure policies. Only the restart policy is sent to the Engine.
// Resource limits are left unchanged. Failures are logged and do not abort
// the caller.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - container: Container whose restart policy should be updated.
//   - policy: Restart policy to apply. Empty Name disables automatic restart.
func (c *client) SetRestartPolicy(
	ctx context.Context,
	container types.Container,
	policy dockerContainer.RestartPolicy,
) {
	if container == nil {
		return
	}

	if policy.Name == "" {
		policy.Name = dockerContainer.RestartPolicyDisabled
		policy.MaximumRetryCount = 0
	}

	clogVal := c.logger().With().
		Str("container_id", string(container.ID())).
		Str("restart_policy", string(policy.Name)).
		Logger()
	clog := &clogVal

	clog.Debug().Msg("Setting container restart policy")

	_, err := c.api.ContainerUpdate(
		ctx,
		string(container.ID()),
		dockerClient.ContainerUpdateOptions{
			RestartPolicy: &policy,
		},
	)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to set container restart policy")
	}
}

// RemoveContainer removes a container from the Docker host.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - container: Container to remove.
//
// Returns:
//   - error: Non-nil if removal fails, nil on success.
func (c *client) RemoveContainer(ctx context.Context, container types.Container) error {
	clogVal := c.logger().With().
		Str("container", container.Name()).
		Str("id", container.ID().ShortID()).
		Logger()
	clog := &clogVal

	clog.Debug().Msg("Removing container")

	_, err := c.api.ContainerRemove(
		ctx,
		string(container.ID()),
		dockerClient.ContainerRemoveOptions{
			Force: true,
		},
	)
	if err != nil && !cerrdefs.IsNotFound(err) {
		clog.Debug().
			Err(err).
			Msg("Failed to remove container")

		return fmt.Errorf("%w: %w", errRemoveContainerFailed, err)
	}

	if cerrdefs.IsNotFound(err) {
		clog.Debug().Msg("Container already removed")

		return nil
	}

	clog.Debug().Msg("Container removed")

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
func (c *client) RenameContainer(ctx context.Context, container types.Container, newName string) error {
	// Perform rename using helper function.
	err := recreate.RenameTarget(c.logger(), ctx, c.api, container, newName)
	if err != nil {
		c.logger().Debug().
			Err(err).
			Str("container", container.Name()).
			Str("new_name", newName).
			Msg("Failed to rename container")

		return fmt.Errorf("rename container: %w", err)
	}

	c.logger().Debug().
		Str("container", container.Name()).
		Str("new_name", newName).
		Msg("Renamed container")

	return nil
}

// IsContainerStale checks if a container's image is outdated.
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
	ctx context.Context,
	container types.Container,
	params types.UpdateParams,
) (bool, types.ImageID, string, error) {
	// Use image client to perform staleness check.
	imgClient := containerImage.NewClient(c.api, c.logger(), c.Registry, c.RateLimiter)

	stale, newestImage, latestDigest, err := imgClient.IsContainerStale(
		ctx,
		container,
		params,
		c.WarnOnHeadFailed,
	)
	if err != nil {
		c.logger().Debug().
			Err(err).
			Str("container", container.Name()).
			Str("image", container.ImageName()).
			Msg("Failed to check container staleness")

		return stale, newestImage, latestDigest, fmt.Errorf("check container staleness: %w", err)
	}

	c.logger().Debug().
		Str("container", container.Name()).
		Str("image", container.ImageName()).
		Bool("stale", stale).
		Str("newest_image", string(newestImage)).
		Msg("Checked container staleness")

	return stale, newestImage, latestDigest, nil
}

// CheckContainerUpdate reports whether a newer image is available without pulling.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - container: Container to check.
//   - params: Update parameters (NoPull, LabelPrecedence).
//
// Returns:
//   - bool: True if an update is available, false otherwise.
//   - types.ImageID: Latest local image ID when known.
//   - string: Latest registry manifest digest (empty if unavailable).
//   - error: Non-nil if the check fails, nil on success.
func (c *client) CheckContainerUpdate(
	ctx context.Context,
	container types.Container,
	params types.UpdateParams,
) (bool, types.ImageID, string, error) {
	imgClient := containerImage.NewClient(c.api, c.logger(), c.Registry, c.RateLimiter)

	available, newestImage, latestDigest, err := imgClient.CheckContainerUpdate(
		ctx,
		container,
		params,
	)
	if err != nil {
		c.logger().Debug().
			Err(err).
			Str("container", container.Name()).
			Str("image", container.ImageName()).
			Msg("Failed to check container for updates")

		return available, newestImage, latestDigest, fmt.Errorf("check container for updates: %w", err)
	}

	c.logger().Debug().
		Str("container", container.Name()).
		Str("image", container.ImageName()).
		Bool("update_available", available).
		Str("latest_image_id", string(newestImage)).
		Str("latest_digest", latestDigest).
		Msg("Checked container for updates")

	return available, newestImage, latestDigest, nil
}

// GetImageAnnotations reads the OCI annotations of an image by reference.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - imageRef: Image reference, tag or digest, to inspect.
//
// Returns:
//   - oci.Annotations: Annotations from the image config labels, or empty.
func (c *client) GetImageAnnotations(ctx context.Context, imageRef string) oci.Annotations {
	imgClient := containerImage.NewClient(c.api, c.logger(), c.Registry, c.RateLimiter)

	return imgClient.GetImageAnnotations(ctx, imageRef)
}

// RemoveImageByID deletes an image from the Docker host by its ID.
//
// Parameters:
//   - imageID: ID of the image to remove.
//   - imageName: Name of the image to remove (for logging purposes).
//
// Returns:
//   - error: Non-nil if removal fails, nil on success.
func (c *client) RemoveImageByID(
	ctx context.Context,
	imageID types.ImageID,
	imageName string,
) error {
	// Use image client to remove the image.
	imgClient := containerImage.NewClient(c.api, c.logger(), c.Registry, c.RateLimiter)

	err := imgClient.RemoveImageByID(ctx, imageID, imageName)
	if err != nil {
		c.logger().Debug().
			Err(err).
			Str("image_id", string(imageID)).
			Str("image_name", imageName).
			Msg("Failed to remove image")

		return fmt.Errorf("remove image: %w", err)
	}

	c.logger().Debug().
		Str("image_id", imageID.ShortID()).
		Str("image_name", imageName).
		Msg("Cleaned up old image")

	return nil
}

// GetVersion returns the client's API version.
//
// Returns:
//   - string: Docker API version (e.g., "1.44").
func (c *client) GetVersion() string {
	return strings.Trim(c.api.ClientVersion(), "\"")
}

// Ping verifies connectivity to the Docker daemon.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//
// Returns:
//   - error: Non-nil if the daemon is unreachable, nil on success.
func (c *client) Ping(ctx context.Context) error {
	_, err := c.api.Ping(
		ctx,
		// Version renegotiation is not necessary.
		dockerClient.PingOptions{NegotiateAPIVersion: false},
	)
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	return nil
}

// GetInfo returns system information from the Docker daemon.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//
// Returns:
//   - types.SystemInfo: Daemon identity fields used for runtime detection.
//   - error: Non-nil if retrieval fails, nil on success.
func (c *client) GetInfo(ctx context.Context) (types.SystemInfo, error) {
	info, err := c.api.Info(ctx, dockerClient.InfoOptions{})
	if err != nil {
		c.logger().Debug().
			Err(err).
			Msg("Failed to get system info")

		return types.SystemInfo{}, fmt.Errorf("failed to get system info: %w", err)
	}

	return types.SystemInfo{
		Name:            info.Info.Name,
		ServerVersion:   info.Info.ServerVersion,
		OSType:          info.Info.OSType,
		OperatingSystem: info.Info.OperatingSystem,
		Driver:          info.Info.Driver,
	}, nil
}

// GetImageDiskUsage returns Docker image storage usage from the daemon.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//
// Returns:
//   - types.ImageDiskUsage: Aggregated image usage.
//   - error: Non-nil if retrieval fails, nil on success.
func (c *client) GetImageDiskUsage(ctx context.Context) (types.ImageDiskUsage, error) {
	result, err := c.api.DiskUsage(ctx, dockerClient.DiskUsageOptions{
		Images: true,
	})
	if err != nil {
		c.logger().Debug().
			Err(err).
			Msg("Failed to get image disk usage")

		return types.ImageDiskUsage{}, fmt.Errorf("failed to get image disk usage: %w", err)
	}

	return types.ImageDiskUsage{
		TotalSize:   result.Images.TotalSize,
		Reclaimable: result.Images.Reclaimable,
		TotalCount:  result.Images.TotalCount,
		ActiveCount: result.Images.ActiveCount,
	}, nil
}

// WaitForContainerHealthy waits for a container to become healthy or times out.
//
// The timeout parameter controls the maximum wait duration:
//   - timeout > 0: Wait up to the specified duration for the container to become healthy.
//   - timeout <= 0: Wait indefinitely until the container becomes healthy, unhealthy,
//     or the context is cancelled.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - containerID: ID of the container to wait for.
//   - timeout: Maximum duration to wait for health. Zero or negative values cause
//     indefinite waiting.
//
// Returns:
//   - error: Non-nil if timeout is reached, container becomes unhealthy, or inspection
//     fails. nil if healthy, or if no health check is configured or it is disabled.
func (c *client) WaitForContainerHealthy(
	ctx context.Context,
	containerID types.ContainerID,
	timeout time.Duration,
) error {
	// Guard against zero/negative timeouts by using a non-deadline context.
	// This allows the function to poll at least once rather than immediately timing out.
	var cancel context.CancelFunc

	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	defer cancel()

	clogVal := c.logger().With().
		Str("container_id", string(containerID)).
		Logger()
	clog := &clogVal

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Distinguish between timeout and cancellation for clearer diagnostics
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				clog.Warn().Msg("Timeout waiting for container to become healthy")

				return fmt.Errorf("%w: %s", errHealthCheckTimeout, containerID)
			}
			// Context was cancelled (e.g., graceful shutdown, parent cancellation)
			clog.Debug().Msg("Context canceled while waiting for container to become healthy")

			return fmt.Errorf("%w: %s", errHealthCheckCanceled, containerID)
		case <-ticker.C:
			// Inspect the container to check health status
			inspect, err := c.api.ContainerInspect(
				ctx,
				string(containerID),
				dockerClient.ContainerInspectOptions{},
			)
			if err != nil {
				clog.Debug().
					Err(err).
					Msg("Failed to inspect container for health check")

				return fmt.Errorf("failed to inspect container %s: %w", containerID, err)
			}

			// Check if health check is configured
			if inspect.Container.State == nil || inspect.Container.State.Health == nil {
				clog.Debug().Msg("No health check configured for container, proceeding")

				return nil
			}

			// Podman reports a health object for a disabled health check, and
			// its status stays empty because the check never runs. Docker
			// reports no health object for one.
			if healthCheckDisabled(inspect.Container.Config) {
				clog.Debug().Msg("Health check disabled for container, proceeding")

				return nil
			}

			status := inspect.Container.State.Health.Status
			clog.Debug().
				Str("health_status", string(status)).
				Msg("Checked container health status")

			if status == "healthy" {
				clog.Debug().Msg("Container is now healthy")

				return nil
			}

			if status == "unhealthy" {
				clog.Warn().Msg("Container health check failed")

				return fmt.Errorf("%w: %s", errHealthCheckFailed, containerID)
			}

			// Continue polling for "starting" or other statuses
		}
	}
}

// healthCheckDisabled reports whether a container's health check is turned off,
// which Docker and Podman record as a test of only "NONE".
//
// Parameters:
//   - config: Container configuration from an inspect.
//
// Returns:
//   - bool: True when the health check test is ["NONE"].
func healthCheckDisabled(config *dockerContainer.Config) bool {
	return config != nil && config.Healthcheck != nil &&
		len(config.Healthcheck.Test) == 1 && config.Healthcheck.Test[0] == "NONE"
}

// logger returns the client's process logger, or a discarded nop if unset.
// Production paths always set a real logger via NewClient.
// Tests may construct partial *client values without log.
func (c *client) logger() *zerolog.Logger {
	if c != nil && c.log != nil {
		return c.log
	}

	return nopLog()
}

// getRuntime reports whether the container runtime is Podman.
//
// Callers ask only when the answer changes their behavior: the container list
// filter when restarting containers are included, and CPU handling in the auto
// CPU copy mode. A successful detection is cached to avoid repeated API calls
// and filesystem checks. The runtime is assumed to not change during the
// lifetime of the client. A failed or canceled detection is not cached, so the
// next call tries again.
//
// Parameters:
//   - ctx: Context for the detection's API call.
//
// Returns:
//   - bool: True if Podman is detected, false otherwise.
func (c *client) getRuntime(ctx context.Context) bool {
	c.runtimeMu.Lock()
	defer c.runtimeMu.Unlock()

	if c.runtimeDetected {
		return c.isPodman
	}

	isPodman, err := detectRuntime(c.logger(), ctx, c.api, c.Fs)
	if err != nil {
		c.logger().Debug().
			Err(err).
			Msg("Failed to detect container runtime, falling back to Docker")

		return false
	}

	c.isPodman = isPodman
	c.runtimeDetected = true

	return isPodman
}

// isDaemonConnectionError checks if an error is a transient Docker daemon connection error.
// These errors indicate the Docker daemon is temporarily unreachable and may self-heal,
// making them suitable for retry logic.
//
// Parameters:
//   - err: The error to check.
//
// Returns:
//   - bool: True if the error is a Docker daemon connection error, false otherwise.
func isDaemonConnectionError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	return strings.Contains(errMsg, "Cannot connect to the Docker daemon") ||
		strings.Contains(errMsg, "connection refused") ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF)
}
