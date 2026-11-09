package client

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v7"
	"github.com/rs/zerolog"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/moby/moby/api/types/container"
	dockerClient "github.com/moby/moby/client"
	dockerAPIVersion "github.com/moby/moby/client/pkg/versions"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/copyfile"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	containerImage "github.com/nicholas-fedor/watchtower/internal/container/image"
	"github.com/nicholas-fedor/watchtower/internal/container/network"
	"github.com/nicholas-fedor/watchtower/internal/container/oci"
	"github.com/nicholas-fedor/watchtower/internal/container/recreate"
	containerRuntime "github.com/nicholas-fedor/watchtower/internal/container/runtime"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	registryConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/registry"
	updateConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/update"
)

// Constants for CPUCopyMode values.
const (
	// cpuCopyModeAuto indicates automatic detection of container runtime for
	// CPU copying behavior.
	cpuCopyModeAuto = "auto"

	// daemonStartupTimeout bounds the wait for a Docker daemon that is not
	// serving its API yet. dockerd starts every container that has a restart
	// policy before it serves its API. Watchtower is one of them, and the
	// restore can take minutes on a host with many containers.
	daemonStartupTimeout = 3 * time.Minute

	// daemonWaitNotice is how long the first ping may go unanswered before
	// Watchtower logs that it is waiting for the daemon.
	daemonWaitNotice = 10 * time.Second

	// daemonRetryDelay is the first delay before pinging again after a ping
	// failed without an answer from the daemon.
	daemonRetryDelay = time.Second

	// daemonMaxRetryDelay caps the delay between pings.
	daemonMaxRetryDelay = 10 * time.Second

	// daemonRetryBackoff multiplies the delay between pings after each
	// failed ping.
	daemonRetryBackoff = 2

	// maxListRetries is the maximum number of retry attempts for transient Docker
	// connection failures when listing containers.
	maxListRetries = 3

	// currentContainerInspectAttempts is how many times
	// [Client.GetCurrentWatchtowerContainer] inspects the container before it
	// gives up.
	//
	// A daemon that has just started serving its API can be slow to answer.
	currentContainerInspectAttempts = 3

	// currentContainerInspectTimeout bounds each of those inspects.
	currentContainerInspectTimeout = 5 * time.Second

	// currentContainerInspectRetryDelay is the pause between those inspects.
	currentContainerInspectRetryDelay = 2 * time.Second
)

// baseListRetryDelay is the base delay for exponential backoff between retries.
// Actual delays are baseListRetryDelay * 2^attempt (5s, 10s).
var baseListRetryDelay = 5 * time.Second

// Errors for container health and update operations.
var (
	// errHealthCheckTimeout indicates that waiting for a container to become healthy timed out.
	errHealthCheckTimeout = errors.New("timeout waiting for container to become healthy")
	// errHealthCheckCanceled indicates that waiting for a container to become healthy was canceled.
	errHealthCheckCanceled = errors.New("context canceled while waiting for container to become healthy")
	// errHealthCheckFailed indicates that a container's health check failed.
	errHealthCheckFailed = errors.New("container health check failed")
	// errRecreateDockerClient indicates failure recreating the Docker client after API version fallback.
	errRecreateDockerClient = errors.New("failed to recreate Docker client")
	// errSetRestartPolicyFailed indicates a failure to update a container's restart policy.
	errSetRestartPolicyFailed = errors.New("failed to set restart policy")
)

// nopLog returns a fresh discarded logger for optional construction and fallbacks.
// Each call allocates a new Nop instance.
func nopLog() *zerolog.Logger {
	n := zerolog.Nop()

	return &n
}

// Client is Watchtower's Docker client. It manages containers and images and
// runs commands inside containers.
//
// It wraps the Docker API client and applies custom behavior via ClientOptions.
type Client struct {
	api dockerClient.APIClient
	log *zerolog.Logger

	// copyFiles holds labeled file snapshots between stop and create of a recreation.
	copyFiles copyfile.Pending

	ClientOptions

	// runtimeMu protects the cached runtime detection result.
	runtimeMu sync.Mutex
	// runtimeDetected is true once the container runtime was detected successfully.
	runtimeDetected bool
	// isPodman caches the result of Podman runtime detection.
	isPodman bool
}

// ClientOptions configures container management behavior for the Docker client.
type ClientOptions struct {
	// Fs is the filesystem rooted at "/" that holds the container runtime
	// marker files. The host filesystem is used when it is nil.
	Fs fs.FS
	// RateLimiter paces image pulls, digest checks, and image age requests per
	// registry host. NewClient creates one when it is nil.
	RateLimiter      *ratelimit.Limiter
	CPUCopyMode      string
	WarnOnHeadFailed containerImage.WarningStrategy
	// Registry holds the settings for registry digest and image age requests.
	Registry                registryConfig.Registry
	RemoveVolumes           bool
	IncludeStopped          bool
	ReviveStopped           bool
	IncludeRestarting       bool
	DisableMemorySwappiness bool
}

// ImageDiskUsage holds aggregated Docker image storage usage from GET /system/df.
type ImageDiskUsage struct {
	// TotalSize is the number of bytes used by images.
	TotalSize int64
	// Reclaimable is the number of bytes that unused images could free.
	Reclaimable int64
	// TotalCount is the number of images.
	TotalCount int64
	// ActiveCount is the number of images in use by containers.
	ActiveCount int64
}

// NewClient initializes a new Client instance for Docker API interactions.
//
// It configures the client using environment variables (e.g., DOCKER_HOST, DOCKER_API_VERSION)
// and validates the API version, falling back to autonegotiation if necessary.
//
// Parameters:
//   - ctx: Bounds the wait for a daemon that is not serving its API yet. The wait ends after
//     three minutes at most.
//   - log: Process logger.
//   - opts: Options to customize container management behavior.
//
// Returns:
//   - *Client: The initialized client. It is nil when the error is not.
//   - error: Non-nil when the client cannot be built or rebuilt. An unanswered ping is not an
//     error.
func NewClient(ctx context.Context, log *zerolog.Logger, opts ClientOptions) (*Client, error) {
	// Initialize client from environment. FromEnv reads DOCKER_HOST, DOCKER_TLS_VERIFY,
	// DOCKER_CERT_PATH, and DOCKER_API_VERSION. When DOCKER_API_VERSION is set, the client
	// uses that fixed version. Otherwise, it defaults to the maximum supported version
	// with automatic negotiation enabled.
	//
	// Construction does not use the context. It bounds only the ping and API negotiation
	// that follow.
	cli, err := dockerClient.New(dockerClient.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}

	if opts.RateLimiter == nil {
		opts.RateLimiter = ratelimit.New()
	}

	cli, result, err := negotiateDockerAPI(ctx, log, cli)
	if err != nil {
		return nil, err
	}

	log.Debug().
		Str("client_version", cli.ClientVersion()).
		Str("server_version", result.APIVersion).
		Msg("Initialized Docker client")

	return &Client{
		api:           cli,
		log:           log,
		ClientOptions: opts,
	}, nil
}

// negotiateDockerAPI pings the daemon to validate connectivity and negotiate the API version.
// It waits for a daemon that is not serving its API yet until ctx ends, for three minutes at
// most.
//
// When DOCKER_API_VERSION is incompatible with the server, it recreates the client without a
// fixed version so negotiation can select a supported API. A fixed version is incompatible when
// the ping fails with a not-found error, or when it is newer than the API version the daemon
// reports. Other ping failures are non-fatal: the client is returned and version negotiation is
// deferred until the first API call.
//
// Parameters:
//   - ctx: Bounds the wait for the daemon.
//   - log: Process logger.
//   - cli: Docker client created from the environment.
//
// Returns:
//   - *dockerClient.Client: Client to use for subsequent API calls (possibly recreated).
//   - dockerClient.PingResult: Result of a successful ping, or zero when ping failed.
//   - error: Non-nil only when the client cannot be recreated after an incompatible API version.
func negotiateDockerAPI(
	ctx context.Context,
	log *zerolog.Logger,
	cli *dockerClient.Client,
) (*dockerClient.Client, dockerClient.PingResult, error) {
	// Ping triggers API version negotiation, or only pings when DOCKER_API_VERSION fixes the
	// version.
	result, err := pingUntilServing(ctx, log, cli)

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
		result, err = pingUntilServing(ctx, log, cli)
	}

	if err != nil {
		log.Warn().
			Err(err).
			Msg("Ping failed during initialization. Will negotiate on first API call")

		return cli, dockerClient.PingResult{}, nil
	}

	return cli, result, nil
}

// pingUntilServing pings the daemon until it answers, for up to three minutes or until ctx
// ends.
// A daemon that is restoring its containers holds the connection and answers
// the queued ping once it serves its API. A ping that fails without an answer
// is sent again. The wait ends early on an error that a later ping cannot fix,
// such as a TLS error or a 4xx answer.
//
// Parameters:
//   - ctx: Bounds the wait for the daemon.
//   - log: Process logger.
//   - cli: Docker client to ping.
//
// Returns:
//   - dockerClient.PingResult: Result of the first successful ping.
//   - error: The last ping error, nil on success.
func pingUntilServing(
	ctx context.Context,
	log *zerolog.Logger,
	cli *dockerClient.Client,
) (dockerClient.PingResult, error) {
	ctx, cancel := context.WithTimeout(ctx, daemonStartupTimeout)
	defer cancel()

	start := time.Now()

	// The caller's deadline can end the wait before three minutes.
	deadline, _ := ctx.Deadline()
	timeout := deadline.Sub(start).Round(time.Second)

	var (
		retries    int
		noticeOnce sync.Once
	)

	notice := func(err error) {
		noticeOnce.Do(func() {
			log.Info().
				Err(err).
				Str("timeout", timeout.String()).
				Msg("Docker daemon is not answering yet. Waiting for it")
		})
	}

	noticeTimer := time.AfterFunc(daemonWaitNotice, func() { notice(nil) })
	defer noticeTimer.Stop()

	policy := backoff.NewExponentialBackOff()
	// Wait 1 second before the first retry.
	policy.InitialInterval = daemonRetryDelay
	// Double the wait after each retry.
	policy.Multiplier = daemonRetryBackoff
	// Never wait more than 10 seconds.
	policy.MaxInterval = daemonMaxRetryDelay
	// No jitter, since one client has no herd to spread out.
	policy.RandomizationFactor = 0

	result, err := backoff.Retry(ctx, func() (dockerClient.PingResult, error) {
		result, err := cli.Ping(ctx, dockerClient.PingOptions{NegotiateAPIVersion: true})
		if err == nil {
			return result, nil
		}

		err = fmt.Errorf("ping Docker daemon: %w", err)
		if !isTransientError(err) {
			return result, backoff.Permanent(err)
		}

		return result, err
	},
		// Wait between pings as configured above.
		backoff.WithBackOff(policy),
		// Start no wait that would end after three minutes.
		backoff.WithMaxElapsedTime(daemonStartupTimeout),
		// Log each retry, and the first at info.
		backoff.WithNotify(func(err error, _ time.Duration) {
			retries++

			notice(err)
			log.Debug().
				Err(err).
				Int("retry", retries).
				Msg("Docker daemon is still not answering")
		}),
	)
	if err != nil {
		// Report the last ping error rather than the retry loop's summary.
		retryErr := backoff.AsRetryError(err)
		if retryErr != nil {
			return result, retryErr.LastErr
		}

		return result, fmt.Errorf("ping Docker daemon: %w", err)
	}

	if time.Since(start) >= daemonWaitNotice || retries > 0 {
		log.Debug().
			Str("waited", time.Since(start).Round(time.Millisecond).String()).
			Msg("Docker daemon is serving its API")
	}

	return result, nil
}

// isTransientError reports whether a Docker API error may pass on a later
// attempt. That happens once the daemon or a proxy in front of it serves its
// API.
//
// It accepts the lost connections that the container list retries, such as the
// Docker client's "Cannot connect" error and an unexpected end of the answer.
// It also accepts any transport failure, which the Docker client reports as a
// connection failure.
// Examples are a name that does not resolve yet, a refused connection, and a
// missing socket. A 5xx answer counts too, since a socket proxy answers that
// way while its daemon is down, and so does a request that timed out. TLS and
// certificate errors and other answers never pass.
//
// Parameters:
//   - err: Error from a Docker API call.
//
// Returns:
//   - bool: True when the call may succeed if it is sent again.
func isTransientError(err error) bool {
	if isTLSSetupError(err) {
		return false
	}

	return errors.Is(err, context.DeadlineExceeded) ||
		isDaemonConnectionError(err) ||
		dockerClient.IsErrConnectionFailed(err) ||
		cerrdefs.IsInternal(err) ||
		cerrdefs.IsUnavailable(err)
}

// isTLSSetupError reports whether err comes from a TLS setup that cannot
// connect to the daemon. Examples are an untrusted certificate, a rejected
// client certificate, and a server that does not speak TLS.
//
// Parameters:
//   - err: Error to check.
//
// Returns:
//   - bool: True when err is a TLS setup error.
func isTLSSetupError(err error) bool {
	var (
		recordErr tls.RecordHeaderError
		alertErr  tls.AlertError
		verifyErr *tls.CertificateVerificationError
	)

	return errors.Is(err, http.ErrSchemeMismatch) ||
		errors.As(err, &recordErr) ||
		errors.As(err, &alertErr) ||
		errors.As(err, &verifyErr)
}

// Events streams the Docker daemon's events (see percontainer.EventStream).
// Container schedules type-assert for it, so consumers' client interfaces
// and their mocks need not declare it.
//
// Parameters:
//   - ctx: Ends the stream when canceled.
//   - options: Which events to stream.
//
// Returns:
//   - dockerClient.EventsResult: The event and error channels.
func (c *Client) Events(ctx context.Context, options dockerClient.EventsListOptions) dockerClient.EventsResult {
	return c.api.Events(ctx, options)
}

// ListContainers retrieves a list of containers, optionally filtered.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - filter: Optional filters to apply to container list. Multiple filters are combined with logical AND.
//
// Returns:
//   - []*container.Container: List of matching containers.
//   - error: Non-nil if listing fails, nil on success.
func (c *Client) ListContainers(ctx context.Context, filter ...filters.Filter) ([]*container.Container, error) {
	// Podman has no restarting status, so the runtime only matters when
	// restarting containers are included.
	isPodman := c.IncludeRestarting && c.getRuntime(ctx)

	var containerFilter filters.Filter

	if len(filter) > 0 {
		if len(filter) == 1 {
			// Single filter: use it directly
			containerFilter = filter[0]
		} else {
			// Multiple filters: combine them with logical AND
			// A container must pass ALL filters to be included
			containerFilter = func(c filters.FilterableContainer) bool {
				for _, f := range filter {
					if !f(c) {
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
		containers []*container.Container
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
//   - *container.Container: Container details if found.
//   - error: Non-nil if retrieval fails, nil on success.
func (c *Client) GetContainer(ctx context.Context, containerID container.ContainerID) (*container.Container, error) {
	// Retrieve target details using helper function.
	target, err := getSourceContainer(c.logger(), ctx, c.api, containerID, nil)
	if err != nil {
		c.logger().Debug().
			Err(err).
			Str("container_id", string(containerID)).
			Msg("Failed to get container")

		return nil, err
	}

	if !target.HasImageInfo() {
		c.logger().Warn().
			Str("container", target.Name()).
			Str("container_id", string(containerID)).
			Str("image", target.ImageName()).
			Msg("Failed to retrieve image info")
	}

	c.logger().Debug().
		Str("container_id", string(containerID)).
		Msg("Retrieved container details")

	return target, nil
}

// GetCurrentWatchtowerContainer retrieves container information for the
// specified container ID, skipping image inspection.
//
// A transient failure, such as a timeout, a refused connection, or a 5xx
// answer, is tried again, so the call can take up to about 20 seconds. ctx can
// end it sooner. Other failures return at once.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control that bounds the retries.
//   - containerID: ID of the container to retrieve.
//
// Returns:
//   - *container.Container: Container with imageInfo set to nil.
//   - error: Non-nil if every inspect fails, nil on success.
func (c *Client) GetCurrentWatchtowerContainer(
	ctx context.Context,
	containerID container.ContainerID,
) (*container.Container, error) {
	clogVal := c.logger().With().
		Str("container_id", string(containerID)).
		Logger()
	clog := &clogVal

	clog.Debug().Msg("Inspecting current Watchtower container")

	containerInfo, err := backoff.Retry(ctx, func() (dockerClient.ContainerInspectResult, error) {
		// Give each inspect 5 seconds.
		attemptCtx, cancel := context.WithTimeout(ctx, currentContainerInspectTimeout)
		defer cancel()

		result, err := c.api.ContainerInspect(
			attemptCtx,
			string(containerID),
			dockerClient.ContainerInspectOptions{},
		)
		if err == nil {
			return result, nil
		}

		err = fmt.Errorf("%w: %w", errInspectContainerFailed, err)
		if !isTransientError(err) {
			return result, backoff.Permanent(err)
		}

		return result, err
	},
		// Wait 2 seconds between inspects.
		backoff.WithBackOff(backoff.NewConstantBackOff(currentContainerInspectRetryDelay)),
		// Inspect at most three times.
		backoff.WithMaxTries(currentContainerInspectAttempts),
		// No time limit beyond the tries and ctx.
		backoff.WithMaxElapsedTime(0),
		// Log each retry at debug.
		backoff.WithNotify(func(err error, _ time.Duration) {
			clog.Debug().Err(err).Msg("Failed to inspect current Watchtower container. Retrying")
		}),
	)
	if err != nil {
		// Report the last inspect error rather than the retry loop's summary.
		retryErr := backoff.AsRetryError(err)
		if retryErr != nil {
			err = retryErr.LastErr
		}

		clog.Debug().
			Err(err).
			Msg("Failed to inspect current Watchtower container")

		return nil, err
	}

	clog.Debug().Msg("Retrieved minimal container info")

	return container.NewContainer(c.logger(), &containerInfo.Container, nil), nil
}

// StopContainer stops a specified container.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - target: Container to stop.
//   - timeout: Duration to wait before forcing stop.
//
// Returns:
//   - error: Non-nil if stop fails, nil on success.
func (c *Client) StopContainer(ctx context.Context, target *container.Container, timeout time.Duration) error {
	// Stop container using helper function.
	err := stopSourceContainer(c.logger(), ctx, c.api, target, timeout)
	if err != nil {
		c.logger().Debug().
			Err(err).
			Str("container", target.Name()).
			Str("image", target.ImageName()).
			Msg("Failed to stop container")

		return err
	}

	c.logger().Debug().
		Str("container", target.Name()).
		Str("image", target.ImageName()).
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
//   - target: Container to stop and remove.
//   - timeout: Duration to wait before forcing stop.
//
// Returns:
//   - error: Non-nil if stop/removal fails, nil on success.
func (c *Client) StopAndRemoveContainer(ctx context.Context, target *container.Container, timeout time.Duration) error {
	// Stop and remove container using helper function with volume option.
	err := stopAndRemoveSourceContainer(c.logger(),
		ctx,
		c.api,
		target,
		timeout,
		c.RemoveVolumes,
	)
	if err != nil {
		c.logger().Debug().
			Err(err).
			Str("container", target.Name()).
			Str("image", target.ImageName()).
			Msg("Failed to stop and remove container")

		return err
	}

	c.logger().Debug().
		Str("container", target.Name()).
		Str("image", target.ImageName()).
		Msg("Stopped and removed container")

	return nil
}

// CreateContainer creates a new container based on the provided
// container's configuration, but does not start it.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - target: Source container to replicate.
//
// Returns:
//   - container.ContainerID: ID of the new container.
//   - error: Non-nil if creation fails, nil on success.
func (c *Client) CreateContainer(ctx context.Context, target *container.Container) (container.ContainerID, error) {
	defer c.DiscardCopyFiles(target.ID())

	clogVal := c.logger().With().
		Str("container", target.Name()).
		Str("image", target.ImageName()).
		Logger()
	clog := &clogVal
	// Only the auto CPU copy mode depends on the runtime.
	isPodman := c.CPUCopyMode == cpuCopyModeAuto && c.getRuntime(ctx)

	clientVersion := c.GetVersion()

	clog.Debug().
		Str("client_version", clientVersion).
		Msg("Obtaining source container network configuration")

	networkConfig := network.FromContainer(c.logger(), target, clientVersion)

	newID, err := recreate.CreateTarget(c.logger(),
		ctx,
		c.api,
		target,
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

	err = c.injectStoredCopyFiles(ctx, target, newID)
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
//   - target: Source container to replicate.
//
// Returns:
//   - container.ContainerID: ID of the new container.
//   - error: Non-nil if creation/start fails, nil on success.
func (c *Client) StartContainer(ctx context.Context, target *container.Container) (container.ContainerID, error) {
	clogVal := c.logger().With().
		Str("container", target.Name()).
		Str("image", target.ImageName()).
		Logger()
	clog := &clogVal

	newID, err := c.CreateContainer(ctx, target)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to start new container")

		return "", err
	}

	err = startTargetContainer(ctx, c.api, clog, target, newID, c.ReviveStopped)
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
// checking whether the container should be started, for example under the
// revive-stopped and no-restart configuration options.
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
func (c *Client) StartContainerByID(
	ctx context.Context,
	containerID container.ContainerID,
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
// Resource limits are left unchanged. An empty containerID does nothing, as
// when Watchtower does not run in a container.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - containerID: ID of the container whose restart policy should be updated.
//   - policy: Restart policy to apply. Empty Name disables automatic restart.
//
// Returns:
//   - error: Non-nil if the Engine did not apply the policy, nil on success or
//     for an empty containerID.
func (c *Client) SetRestartPolicy(
	ctx context.Context,
	containerID container.ContainerID,
	policy dockerContainer.RestartPolicy,
) error {
	if containerID == "" {
		return nil
	}

	if policy.Name == "" {
		policy.Name = dockerContainer.RestartPolicyDisabled
		policy.MaximumRetryCount = 0
	}

	clogVal := c.logger().With().
		Str("container_id", string(containerID)).
		Str("restart_policy", string(policy.Name)).
		Logger()
	clog := &clogVal

	clog.Debug().Msg("Setting container restart policy")

	_, err := c.api.ContainerUpdate(
		ctx,
		string(containerID),
		dockerClient.ContainerUpdateOptions{
			RestartPolicy: &policy,
		},
	)
	if err != nil {
		return fmt.Errorf("%w: %w", errSetRestartPolicyFailed, err)
	}

	return nil
}

// RemoveContainer removes a container from the Docker host.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - target: Container to remove.
//
// Returns:
//   - error: Non-nil if removal fails, nil on success.
func (c *Client) RemoveContainer(ctx context.Context, target *container.Container) error {
	clogVal := c.logger().With().
		Str("container", target.Name()).
		Str("id", target.ID().ShortID()).
		Logger()
	clog := &clogVal

	clog.Debug().Msg("Removing container")

	_, err := c.api.ContainerRemove(
		ctx,
		string(target.ID()),
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
//   - target: Container to rename.
//   - newName: New name for the container.
//
// Returns:
//   - error: Non-nil if rename fails, nil on success.
func (c *Client) RenameContainer(ctx context.Context, target *container.Container, newName string) error {
	// Perform rename using helper function.
	err := recreate.RenameTarget(c.logger(), ctx, c.api, target, newName)
	if err != nil {
		c.logger().Debug().
			Err(err).
			Str("container", target.Name()).
			Str("new_name", newName).
			Msg("Failed to rename container")

		return fmt.Errorf("rename container: %w", err)
	}

	c.logger().Debug().
		Str("container", target.Name()).
		Str("new_name", newName).
		Msg("Renamed container")

	return nil
}

// IsContainerStale checks if a container's image is outdated.
//
// Parameters:
//   - target: Container to check.
//   - policy: Update policy for staleness check.
//
// Returns:
//   - bool: True if stale, false otherwise.
//   - container.ImageID: Latest image ID.
//   - error: Non-nil if check fails, nil on success.
func (c *Client) IsContainerStale(
	ctx context.Context,
	target *container.Container,
	policy updateConfig.Policy,
) (bool, container.ImageID, string, error) {
	// Use image client to perform staleness check.
	imgClient := containerImage.NewClient(c.api, c.logger(), c.Registry, c.RateLimiter)

	stale, newestImage, latestDigest, err := imgClient.IsContainerStale(
		ctx,
		target,
		policy,
		c.WarnOnHeadFailed,
	)
	if err != nil {
		c.logger().Debug().
			Err(err).
			Str("container", target.Name()).
			Str("image", target.ImageName()).
			Msg("Failed to check container staleness")

		return stale, newestImage, latestDigest, err //nolint:wrapcheck // The image client's error already names its failure.
	}

	c.logger().Debug().
		Str("container", target.Name()).
		Str("image", target.ImageName()).
		Bool("stale", stale).
		Str("newest_image", string(newestImage)).
		Msg("Checked container staleness")

	return stale, newestImage, latestDigest, nil
}

// CheckContainerUpdate reports whether a newer image is available without pulling.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - target: Container to check.
//   - policy: Update policy (NoPull, LabelPrecedence).
//
// Returns:
//   - bool: True if an update is available, false otherwise.
//   - container.ImageID: Latest local image ID when known.
//   - string: Latest registry manifest digest (empty if unavailable).
//   - error: Non-nil if the check fails, nil on success.
func (c *Client) CheckContainerUpdate(
	ctx context.Context,
	target *container.Container,
	policy updateConfig.Policy,
) (bool, container.ImageID, string, error) {
	imgClient := containerImage.NewClient(c.api, c.logger(), c.Registry, c.RateLimiter)

	available, newestImage, latestDigest, err := imgClient.CheckContainerUpdate(
		ctx,
		target,
		policy,
	)
	if err != nil {
		c.logger().Debug().
			Err(err).
			Str("container", target.Name()).
			Str("image", target.ImageName()).
			Msg("Failed to check container for updates")

		return available, newestImage, latestDigest, fmt.Errorf("check container for updates: %w", err)
	}

	c.logger().Debug().
		Str("container", target.Name()).
		Str("image", target.ImageName()).
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
func (c *Client) GetImageAnnotations(ctx context.Context, imageRef string) oci.Annotations {
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
func (c *Client) RemoveImageByID(
	ctx context.Context,
	imageID container.ImageID,
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
func (c *Client) GetVersion() string {
	return strings.Trim(c.api.ClientVersion(), "\"")
}

// Ping verifies connectivity to the Docker daemon.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//
// Returns:
//   - error: Non-nil if the daemon is unreachable, nil on success.
func (c *Client) Ping(ctx context.Context) error {
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

// GetImageDiskUsage returns Docker image storage usage from the daemon.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//
// Returns:
//   - ImageDiskUsage: Aggregated image usage.
//   - error: Non-nil if retrieval fails, nil on success.
func (c *Client) GetImageDiskUsage(ctx context.Context) (ImageDiskUsage, error) {
	result, err := c.api.DiskUsage(ctx, dockerClient.DiskUsageOptions{
		Images: true,
	})
	if err != nil {
		c.logger().Debug().
			Err(err).
			Msg("Failed to get image disk usage")

		return ImageDiskUsage{}, fmt.Errorf("failed to get image disk usage: %w", err)
	}

	return ImageDiskUsage{
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
func (c *Client) WaitForContainerHealthy(
	ctx context.Context,
	containerID container.ContainerID,
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
// Tests may construct partial *Client values without log.
func (c *Client) logger() *zerolog.Logger {
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
func (c *Client) getRuntime(ctx context.Context) bool {
	c.runtimeMu.Lock()
	defer c.runtimeMu.Unlock()

	if c.runtimeDetected {
		return c.isPodman
	}

	// Without a configured filesystem, the host's holds the marker files.
	fsys := c.Fs
	if fsys == nil {
		fsys = os.DirFS("/")
	}

	isPodman, err := containerRuntime.IsPodman(c.logger(), ctx, c.api, fsys)
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
