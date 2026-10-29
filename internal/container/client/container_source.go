package client

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"
	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/recreate"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// defaultStopSignal is the default signal for stopping containers ("SIGTERM").
const defaultStopSignal = "SIGTERM"

// Errors for listing, inspecting, stopping, and removing source containers.
var (
	// errListContainersFailed indicates a failure to list containers from the Docker host.
	errListContainersFailed = errors.New("failed to list containers")
	// errInspectContainerFailed indicates a failure to inspect a container's details.
	errInspectContainerFailed = errors.New("failed to inspect container")
	// errStopContainerFailed indicates a failure to stop a container with a signal.
	errStopContainerFailed = errors.New("failed to stop container")
	// errRemoveContainerFailed indicates a failure to remove a container from the host.
	errRemoveContainerFailed = errors.New("failed to remove container")
)

// listSourceContainers retrieves a list of containers from the container runtime host.
//
// It filters containers based on options and a provided filter function.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - api: Docker API client.
//   - opts: Client options for filtering.
//   - filter: Function to filter containers.
//   - isPodmanOptional: Optional variadic flag indicating Podman runtime (defaults to false).
//
// Returns:
//   - []types.Container: Filtered list of containers.
//   - error: Non-nil if listing fails, nil on success.
func listSourceContainers(log *zerolog.Logger,
	ctx context.Context,
	api dockerClient.APIClient,
	opts ClientOptions,
	filter types.Filter,
	isPodmanOptional ...bool,
) ([]types.Container, error) {
	clogVal := log.With().
		Bool("include_stopped", opts.IncludeStopped).
		Bool("include_restarting", opts.IncludeRestarting).
		Logger()
	clog := &clogVal

	clog.Debug().Msg("Retrieving container list")

	// Determine if the container runtime is Podman.
	// Default to false (Docker) if not specified.
	isPodman := false
	if len(isPodmanOptional) > 0 {
		isPodman = isPodmanOptional[0]
	}

	// Build filter arguments for container states.
	filterArgs := buildListFilterArgs(opts, isPodman)
	clog.Debug().
		Bool("custom_filter_provided", filter != nil).
		Interface("filter_args", filterArgs).
		Msg("Built filter arguments")

	// Fetch containers with status filters always applied based on ClientOptions.
	listOptions := dockerClient.ContainerListOptions{Filters: filterArgs}

	clog.Debug().Msg("API status filters applied based on ClientOptions")

	containers, err := api.ContainerList(ctx, listOptions)
	if err != nil {
		// Log detailed error information for debugging
		clog.Debug().
			Err(err).
			Str("error_type", fmt.Sprintf("%T", err)).
			Str("endpoint", "/containers/json").
			Str("api_version", strings.Trim(api.ClientVersion(), "\"")).
			Str("docker_host", os.Getenv("DOCKER_HOST")).
			Str("list_options", fmt.Sprintf("%+v", listOptions)).
			Msg("ContainerList API call failed")

		// Check for 404 responses and return an empty container list instead of failing.
		if cerrdefs.IsNotFound(err) {
			clog.Warn().
				Err(err).
				Str("endpoint", "/containers/json").
				Str("api_version", strings.Trim(api.ClientVersion(), "\"")).
				Str("docker_host", os.Getenv("DOCKER_HOST")).
				Msg("Docker API returned 404 for container list. Treating as empty list")

			return []types.Container{}, nil
		}

		clog.Debug().
			Err(err).
			Msg("Failed to list containers")

		return nil, fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

	// Convert and filter containers.
	hostContainers := make([]types.Container, 0, len(containers.Items))
	// Reuse ImageInspect results when multiple containers share an image ID.
	imageCache := make(map[string]*dockerImage.InspectResponse, len(containers.Items))

	for _, runningContainer := range containers.Items {
		container, err := getSourceContainer(log, ctx, api, types.ContainerID(runningContainer.ID), imageCache)
		if err != nil {
			// Log detailed error information for debugging container inspect failures
			log.Debug().
				Str("container_id", runningContainer.ID).
				Err(err).
				Str("error_type", fmt.Sprintf("%T", err)).
				Str("api_version", strings.Trim(api.ClientVersion(), "\"")).
				Str("docker_host", os.Getenv("DOCKER_HOST")).
				Msg("Failed to inspect individual container during list")

			// Handle race condition where containers disappear between API calls
			if cerrdefs.IsNotFound(err) {
				log.Debug().
					Str("container_id", runningContainer.ID).
					Msg("Container no longer exists")

				continue
			}

			return nil, err // Logged above and in getSourceContainer
		}

		if filter == nil || filter(container) {
			if !container.HasImageInfo() {
				log.Warn().
					Str("container", container.Name()).
					Str("container_id", runningContainer.ID).
					Str("image", container.ImageName()).
					Msg("Failed to retrieve image info")
			}

			hostContainers = append(hostContainers, container)
		}
	}

	clog.Debug().
		Int("count", len(hostContainers)).
		Msg("Filtered container list")

	return hostContainers, nil
}

// getSourceContainer inspects a container and optionally reuses ImageInspect results.
//
// It resolves the network mode when it references another container.
//
// Parameters:
//   - log: Logger for debug output.
//   - ctx: Context for cancellation and timeout control.
//   - api: Docker API client.
//   - containerID: ID of the container to inspect.
//   - imageCache: Optional per-list cache of ImageInspect results keyed by image ID.
//
// Returns:
//   - types.Container: Container object if successful. It has no image info when only the image inspect fails.
//   - error: Non-nil if the container inspect fails or the image inspect is canceled or times out.
func getSourceContainer(log *zerolog.Logger,
	ctx context.Context,
	api dockerClient.APIClient,
	containerID types.ContainerID,
	imageCache map[string]*dockerImage.InspectResponse,
) (types.Container, error) {
	clogVal := log.With().
		Str("container_id", string(containerID)).
		Logger()
	clog := &clogVal

	clog.Debug().Msg("Inspecting container")

	// Inspect the container to get its details.
	containerResult, err := api.ContainerInspect(
		ctx,
		string(containerID),
		dockerClient.ContainerInspectOptions{},
	)
	if err != nil {
		// Log detailed error information for debugging
		clog.Debug().
			Err(err).
			Str("error_type", fmt.Sprintf("%T", err)).
			Str("api_version", strings.Trim(api.ClientVersion(), "\"")).
			Str("docker_host", os.Getenv("DOCKER_HOST")).
			Msg("ContainerInspect API call failed")

		clog.Debug().
			Err(err).
			Msg("Failed to inspect container")

		return nil, fmt.Errorf("%w: %w", errInspectContainerFailed, err)
	}

	containerInfo := &containerResult.Container

	// Resolve network mode if it references another container.
	if containerInfo.HostConfig != nil {
		netType, netContainerID, found := strings.Cut(string(containerInfo.HostConfig.NetworkMode), ":")
		if found && netType == "container" {
			parentResult, err := api.ContainerInspect(ctx,
				netContainerID,
				dockerClient.ContainerInspectOptions{},
			)
			if err != nil {
				clog.Warn().
					Err(err).
					Str("container", container.NormalizeContainerName(containerInfo.Name)).
					Str("network_container", netContainerID).
					Msg("Unable to resolve network container")
			} else {
				containerInfo.HostConfig.NetworkMode = dockerContainer.NetworkMode(
					"container:" + parentResult.Container.Name,
				)
				clog.Debug().
					Str("container", container.NormalizeContainerName(containerInfo.Name)).
					Str("network_container", container.NormalizeContainerName(parentResult.Container.Name)).
					Msg("Resolved network container name")
			}
		}

		// Rewrite volumes-from IDs to names while the source still exists.
		recreate.ResolveVolumesFrom(ctx, api, clog, containerInfo)
	}

	imageInfo, err := resolveImageInspect(ctx, api, containerInfo.Image, imageCache)
	if err != nil {
		// A canceled or timed-out inspect ends the listing. Other failures, such
		// as a removed image, leave the container without image info.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}

		clog.Debug().
			Err(err).
			Str("container", container.NormalizeContainerName(containerInfo.Name)).
			Str("image", containerInfo.Image).
			Msg("Failed to retrieve image info")

		return container.NewContainer(log, containerInfo, nil), nil
	}

	clog.Debug().
		Str("image", containerInfo.Image).
		Msg("Retrieved container and image info")

	return container.NewContainer(log, containerInfo, imageInfo), nil
}

// resolveImageInspect returns image inspect metadata, using imageCache when set.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - api: Docker API client.
//   - imageID: Image ID or name to inspect.
//   - imageCache: Optional per-list cache of ImageInspect results.
//
// Returns:
//   - *dockerImage.InspectResponse: Isolated inspect metadata, or nil on inspect failure.
//   - error: Non-nil if ImageInspect fails.
func resolveImageInspect(
	ctx context.Context,
	api dockerClient.APIClient,
	imageID string,
	imageCache map[string]*dockerImage.InspectResponse,
) (*dockerImage.InspectResponse, error) {
	if cached, ok := imageCache[imageID]; ok {
		return cloneImageInspect(cached), nil
	}

	imageResult, err := api.ImageInspect(ctx, imageID)
	if err != nil {
		return nil, fmt.Errorf("inspect image: %w", err)
	}

	imageInfo := &imageResult.InspectResponse
	if imageCache != nil {
		imageCache[imageID] = imageInfo

		return cloneImageInspect(imageInfo), nil
	}

	return imageInfo, nil
}

// cloneImageInspect returns an independent copy of image inspect metadata.
//
// Slice and nested pointer fields are duplicated so the cached original stays
// read-only for other containers.
//
// Parameters:
//   - src: Cached inspect response, or nil.
//
// Returns:
//   - *dockerImage.InspectResponse: Isolated copy, or nil when src is nil.
func cloneImageInspect(src *dockerImage.InspectResponse) *dockerImage.InspectResponse {
	if src == nil {
		return nil
	}

	dst := *src
	dst.RepoTags = slices.Clone(src.RepoTags)
	dst.RepoDigests = slices.Clone(src.RepoDigests)
	dst.RootFS.Layers = slices.Clone(src.RootFS.Layers)
	dst.Manifests = slices.Clone(src.Manifests)

	if src.Config != nil {
		cfg := *src.Config
		cfg.Env = slices.Clone(src.Config.Env)
		cfg.Cmd = slices.Clone(src.Config.Cmd)
		cfg.Entrypoint = slices.Clone(src.Config.Entrypoint)
		cfg.OnBuild = slices.Clone(src.Config.OnBuild)
		cfg.Shell = slices.Clone(src.Config.Shell)
		cfg.Labels = maps.Clone(src.Config.Labels)
		cfg.Volumes = maps.Clone(src.Config.Volumes)
		cfg.ExposedPorts = maps.Clone(src.Config.ExposedPorts)

		if src.Config.Healthcheck != nil {
			health := *src.Config.Healthcheck
			health.Test = slices.Clone(src.Config.Healthcheck.Test)
			cfg.Healthcheck = &health
		}

		dst.Config = &cfg
	}

	if src.GraphDriver != nil {
		driver := *src.GraphDriver
		driver.Data = maps.Clone(src.GraphDriver.Data)
		dst.GraphDriver = &driver
	}

	if src.Descriptor != nil {
		desc := *src.Descriptor
		dst.Descriptor = &desc
	}

	if src.Identity != nil {
		identity := *src.Identity
		dst.Identity = &identity
	}

	return &dst
}

// stopSourceContainer stops the specified container using the Docker API's StopContainer method.
//
// It checks if the container is running, sends the configured stop signal (defaulting to SIGTERM if unset),
// and waits for the specified timeout for graceful shutdown, forcing termination with SIGKILL if necessary.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - api: Docker API client for interacting with the Docker daemon.
//   - sourceContainer: Container object to stop, containing metadata like name and ID.
//   - timeout: Duration to wait for graceful shutdown before forcing termination with SIGKILL.
//
// Returns:
//   - error: Non-nil if stopping the container fails, including a not-found error when the container no
//     longer exists. Nil on successful completion.
func stopSourceContainer(log *zerolog.Logger,
	ctx context.Context,
	api dockerClient.APIClient,
	sourceContainer types.Container,
	timeout time.Duration,
) error {
	clogVal := log.With().
		Str("container", sourceContainer.Name()).
		Str("id", sourceContainer.ID().ShortID()).
		Logger()
	clog := &clogVal

	// Check if the container is running to determine if a stop operation is needed.
	if !sourceContainer.IsRunning() {
		// Log that the container is not running, so no stop attempt is required.
		clog.Debug().Msg("Container is not running, no stop operation needed")

		return nil
	}

	// A stop timeout set on the container takes precedence over the passed one.
	// A timeout of 0 is valid in Docker (means no grace period - immediate SIGKILL after stop signal).
	effectiveTimeout := sourceContainer.StopTimeout(timeout)

	// Retrieve the container's configured stop signal, falling back to SIGTERM if not specified.
	signal := sourceContainer.StopSignal()
	if signal == "" {
		// Use default SIGTERM signal if no custom signal is provided.
		signal = defaultStopSignal
	}

	// Log the stop attempt with the signal and configured timeout for clarity.
	message := "Stopping container"
	if sourceContainer.IsLinkedToRestarting() {
		message = "Stopping linked container"
	}

	clog.Info().
		Str("signal", signal).
		Dur("timeout", effectiveTimeout).
		Msg(message)

	// Record the start time to measure elapsed duration for the stop operation.
	startTime := time.Now()

	// Convert timeout from time.Duration to seconds for Docker API's StopContainer.
	timeoutSeconds := int(effectiveTimeout / time.Second)

	// Call the Docker API's StopContainer with the stop signal and timeout in seconds.
	// The API sends the signal (SIGTERM or custom), waits for the timeout, and sends SIGKILL if needed.
	_, err := api.ContainerStop(
		ctx,
		string(sourceContainer.ID()),
		dockerClient.ContainerStopOptions{
			Signal:  signal,
			Timeout: &timeoutSeconds,
		},
	)
	if err != nil {
		// The container was removed before it could be stopped. Report that
		// so callers can tell it apart from a container this call stopped.
		if cerrdefs.IsNotFound(err) {
			clog.Debug().
				Dur("elapsed", time.Since(startTime)).
				Msg("Container not found during stop")

			return fmt.Errorf("%w: %w", errStopContainerFailed, err)
		}

		// Log the failure with elapsed time and error details for debugging.
		clog.Error().
			Err(err).
			Dur("elapsed", time.Since(startTime)).
			Msg("Failed to stop container")
		// Wrap the error with a specific Watchtower error type for consistent error handling.
		return fmt.Errorf("%w: %w", errStopContainerFailed, err)
	}

	// Log successful stop with elapsed time to confirm the operation's duration.
	clog.Debug().
		Dur("elapsed", time.Since(startTime)).
		Msg("Container stopped successfully")

	return nil
}

// stopAndRemoveSourceContainer stops and removes the specified container using the Docker API.
//
// It first stops the container if running, then removes it with optional volume cleanup.
// When HostConfig.AutoRemove is set, Docker only removes the container after a started
// instance exits. Explicit removal is therefore skipped only when the container was
// running before stop. Non-running containers (for example created but never started)
// are always removed explicitly so the name is freed for recreation.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - api: Docker API client for interacting with the Docker daemon.
//   - sourceContainer: Container object to stop and remove, containing metadata like name and ID.
//   - timeout: Duration to wait for graceful shutdown before forcing termination with SIGKILL.
//   - removeVolumes: Boolean indicating whether to remove associated volumes during container removal.
//
// Returns:
//   - error: Non-nil if stopping or removing the container fails. A not-found error means the container was
//     gone before this call stopped or removed it. Nil on successful completion, including a container that
//     disappeared after this call stopped it.
func stopAndRemoveSourceContainer(log *zerolog.Logger,
	ctx context.Context,
	api dockerClient.APIClient,
	sourceContainer types.Container,
	timeout time.Duration,
	removeVolumes bool,
) error {
	clogVal := log.With().
		Str("container", sourceContainer.Name()).
		Str("id", sourceContainer.ID().ShortID()).
		Logger()
	clog := &clogVal

	// Capture running state before stop. AutoRemove only applies after a started container exits.
	wasRunning := sourceContainer.IsRunning()

	// Stop the container first.
	err := stopSourceContainer(log, ctx, api, sourceContainer, timeout)
	if err != nil {
		return err
	}

	// HostConfig.AutoRemove removes a container after it stops. That only applies
	// when the container was running and Docker handles cleanup after stop.
	info := sourceContainer.ContainerInfo()
	autoRemove := info != nil && info.HostConfig != nil && info.HostConfig.AutoRemove

	// Skip explicit removal only when AutoRemove will run after stop. Never-started
	// or already-stopped AutoRemove containers remain until removed explicitly and
	// would otherwise block name reuse on recreate.
	if autoRemove && wasRunning {
		// Log that the container is skipped due to AutoRemove configuration.
		clog.Debug().Msg("Skipping container removal due to AutoRemove configuration")

		return nil
	}

	if autoRemove && !wasRunning {
		clog.Debug().Msg("Removing non-running AutoRemove container explicitly")
	}

	// Proceed with explicit container removal, including volumes if specified.
	// Log the start of the removal process for tracking.
	clog.Debug().Msg("Initiating container removal")

	// Record start time for the removal operation to track its duration.
	startTime := time.Now()

	// Call the Docker API's ContainerRemove to delete the container, forcing termination of any
	// lingering processes (via SIGKILL if needed) and removing volumes if specified.
	_, err = api.ContainerRemove(
		ctx,
		string(sourceContainer.ID()),
		dockerClient.ContainerRemoveOptions{
			Force:         true,          // Ensure any lingering processes are terminated before removal.
			RemoveVolumes: removeVolumes, // Remove associated volumes if the parameter is true.
		},
	)
	if err != nil && !cerrdefs.IsNotFound(err) {
		// Log removal failure with elapsed time and error details, excluding cases where the container
		// was already removed by another process.
		clog.Error().
			Err(err).
			Dur("elapsed", time.Since(startTime)).
			Msg("Failed to remove container")
		// Wrap the error with a specific Watchtower error type for consistent error handling.
		return fmt.Errorf("%w: %w", errRemoveContainerFailed, err)
	}

	if cerrdefs.IsNotFound(err) {
		// A container this call stopped may already be gone, removed by another
		// process. A container that was not running was gone before this call
		// acted on it, so report that.
		if !wasRunning {
			clog.Debug().
				Dur("elapsed", time.Since(startTime)).
				Msg("Container not found during removal")

			return fmt.Errorf("%w: %w", errRemoveContainerFailed, err)
		}

		clog.Debug().
			Dur("elapsed", time.Since(startTime)).
			Msg("Container already removed by another process")

		return nil
	}

	// Log successful removal with elapsed time to confirm the operation's duration.
	clog.Debug().
		Dur("elapsed", time.Since(startTime)).
		Msg("Container removed successfully")

	return nil
}

// buildListFilterArgs builds filter arguments for retrieving container states.
//
// It uses client options to determine which statuses to include.
//
// Parameters:
//   - opts: Client options for filtering.
//   - isPodman: Whether the runtime is Podman.
//
// Returns:
//   - dockerClient.Filters: Arguments for filtering Docker containers
func buildListFilterArgs(opts ClientOptions, isPodman bool) dockerClient.Filters {
	filterArgs := make(dockerClient.Filters)

	filterArgs.Add("status", "running")

	if opts.IncludeStopped {
		filterArgs.Add("status", "created")
		filterArgs.Add("status", "exited")
	}

	// Podman doesn't have the "restarting" status
	if opts.IncludeRestarting && !isPodman {
		filterArgs.Add("status", "restarting")
	}

	return filterArgs
}
