// Package actions provides core logic for Watchtower’s container update operations.
package actions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/sirupsen/logrus"

	dockerContainer "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/lifecycle"
	"github.com/nicholas-fedor/watchtower/pkg/session"
	"github.com/nicholas-fedor/watchtower/pkg/sorter"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// defaultPullFailureDelay defines the default delay duration for failed Watchtower self-update pulls.
const defaultPullFailureDelay = 5 * time.Minute

// defaultHealthCheckTimeout defines the default timeout for waiting for container health checks.
const defaultHealthCheckTimeout = 5 * time.Minute

// Update scans and updates containers based on parameters.
//
// It checks container staleness, sorts by dependencies, and updates or restarts containers as needed,
// collecting cleaned image info for cleanup. Non-stale linked containers are restarted but not marked as updated.
// Containers with pinned images (referenced by digest) are skipped to preserve immutability.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for interacting with Docker API.
//   - config: UpdateConfig specifying behavior like cleanup, restart, and filtering.
//
// Returns:
//   - types.Report: Session report summarizing scanned, updated, and failed containers.
//   - []types.CleanedImageInfo: Slice of cleaned image info to clean up after updates.
//   - error: Non-nil if listing or sorting fails, nil on success.
func Update(
	ctx context.Context,
	client container.Client,
	config UpdateConfig,
) (types.Report, []types.CleanedImageInfo, error) {
	// Check for context cancellation early
	select {
	case <-ctx.Done():
		return nil, nil, fmt.Errorf("update cancelled: %w", ctx.Err())
	default:
	}

	// Initialize logging for the update process start.
	logrus.Debug("Starting container update check")

	// Create a progress tracker for reporting scanned, updated, and skipped containers.
	progress := &session.Progress{}
	// Track the number of stale containers for logging.
	staleCount := 0
	// Initialize a slice to collect cleaned image info for cleanup after updates.
	cleanupImageInfos := []types.CleanedImageInfo{}
	// Track if Watchtower self-update pull failed to add safeguard delay.
	watchtowerPullFailed := false

	// Map UpdateConfig to types.UpdateParams for internal use.
	params := types.UpdateParams{
		Filter:             config.Filter,
		Cleanup:            config.Cleanup,
		NoRestart:          config.NoRestart,
		Timeout:            config.Timeout,
		MonitorOnly:        config.MonitorOnly,
		LifecycleHooks:     config.LifecycleHooks,
		RollingRestart:     config.RollingRestart,
		LabelPrecedence:    config.LabelPrecedence,
		NoPull:             config.NoPull,
		PullFailureDelay:   config.PullFailureDelay,
		LifecycleUID:       config.LifecycleUID,
		LifecycleGID:       config.LifecycleGID,
		CPUCopyMode:        config.CPUCopyMode,
		RunOnce:            config.RunOnce,
		SkipSelfUpdate:     config.SkipSelfUpdate,
		CurrentContainerID: config.CurrentContainerID,
	}

	// Run pre-check lifecycle hooks if enabled to validate the environment before updates.
	if params.LifecycleHooks {
		logrus.Debug("Executing pre-check lifecycle hooks")
		lifecycle.ExecutePreChecks(client, params)
	}

	// Fetch the list of containers based on the provided filter (e.g., all, specific names).
	containers, err := client.ListContainers(params.Filter)
	if err != nil {
		// Log and return an error if container listing fails.
		logrus.WithError(err).Debug("Failed to list containers")

		return nil, nil, fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

	// Fetch all containers to find linked dependencies that may not be in the filtered list.
	allContainers, err := client.ListContainers(filters.NoFilter)
	if err != nil {
		// Log and return an error if listing all containers fails.
		logrus.WithError(err).Debug("Failed to list all containers")

		return nil, nil, fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

	// Prepare a list of container names and images for detailed debugging output.
	containerNames := make([]string, len(containers))
	for i, c := range containers {
		containerNames[i] = fmt.Sprintf("%s (%s)", c.Name(), c.ImageName())
	}
	// Log the retrieved containers and filter details.
	logrus.WithFields(logrus.Fields{
		"count":      len(containers),
		"containers": containerNames,
		"filter":     fmt.Sprintf("%T", params.Filter),
	}).Debug("Retrieved containers for update check")

	// Detect circular dependencies and mark affected containers as skipped.
	cycles := container.DetectCycles(containers)
	for _, c := range containers {
		if cycles[container.ResolveContainerIdentifier(c)] {
			progress.AddSkipped(c, errCircularDependency, params)
			logrus.Warnf(
				"Skipping container update (circular dependency): %s (%s)",
				c.Name(),
				c.ID().ShortID(),
			)
		}
	}

	// Track containers that fail staleness checks for reporting.
	staleCheckFailed := 0

	// Iterate through containers to check staleness and prepare for updates or restarts.
	for i, sourceContainer := range containers {
		// Check for context cancellation to enable faster shutdown during long update cycles.
		select {
		case <-ctx.Done():
			return progress.Report(), cleanupImageInfos, ctx.Err()
		default:
		}

		// Skip containers already processed (e.g., skipped due to circular dependencies).
		if _, exists := (*progress)[sourceContainer.ID()]; exists {
			continue
		}

		// Set up logging fields for the current container.
		fields := logrus.Fields{
			"container": sourceContainer.Name(),
			"image":     sourceContainer.ImageName(),
		}
		clog := logrus.WithFields(fields)

		// Check if the container uses a pinned (digest-based) image to skip updates.
		isPinned, err := isPinned(sourceContainer, progress, params)
		if err != nil {
			// Log and skip containers with unparsable image references, marking as skipped.
			clog.WithError(err).Debug("Failed to check pinned image, skipping container")
			progress.AddSkipped(
				sourceContainer,
				fmt.Errorf("%w: %w", errParseImageReference, err),
				params,
			)

			staleCheckFailed++

			continue
		}

		if isPinned {
			// Skip staleness checks for pinned images and mark as scanned.
			clog.Debug("Skipping staleness check for pinned image")

			continue
		}

		// Check if the container’s image is stale (outdated) and get the newest image ID.
		stale, newestImage, err := client.IsContainerStale(sourceContainer, params)

		// Determine if the container should be updated based on staleness and params.
		shouldUpdate := shouldUpdateContainer(stale, sourceContainer, params)

		// Log when skipping Watchtower self-update in run-once mode
		if stale && sourceContainer.IsWatchtower() && params.RunOnce {
			clog.Info("Skipping Watchtower self-update in run-once mode")
		}

		// Track old image ID before update for cleanup notifications.
		if shouldUpdate {
			if c, ok := containers[i].(*container.Container); ok {
				c.OldImageID = sourceContainer.ImageID()
			}
		}

		// Verify the container’s configuration if it’s slated for update to ensure recreation is possible.
		if err == nil && shouldUpdate {
			err = sourceContainer.VerifyConfiguration()
			if err != nil {
				// Log configuration verification failure with detailed context.
				logrus.WithError(err).WithFields(logrus.Fields{
					"container_name": sourceContainer.Name(),
					"container_id":   sourceContainer.ID().ShortID(),
					"image_name":     sourceContainer.ImageName(),
					"image_id":       sourceContainer.ImageID().ShortID(),
				}).Debug("Failed to verify container configuration")
			}
		}

		// Handle staleness check results, logging skips or adding to the progress report.
		if err != nil {
			// Skip containers with staleness check errors, marking them as skipped.
			clog.WithError(err).Debug("Cannot update container, skipping")

			stale = false
			staleCheckFailed++

			progress.AddSkipped(sourceContainer, err, params)

			// Track if Watchtower self-update pull failed for safeguard.
			if sourceContainer.IsWatchtower() {
				watchtowerPullFailed = true
			}
		} else {
			// For fresh containers, set newestImage to current image ID for proper categorization
			if !stale {
				newestImage = sourceContainer.ImageID()
			}

			// Log successful staleness check and add to scanned containers.
			clog.WithFields(logrus.Fields{
				"stale":        stale,
				"newest_image": newestImage,
			}).Debug("Checked container staleness")
			progress.AddScanned(sourceContainer, newestImage, params)
		}

		// Update the container’s stale status for dependency sorting.
		// Only mark as stale if the container should actually be updated.
		containers[i].SetStale(stale && shouldUpdate)

		// Increment stale count for logging summary.
		if stale {
			staleCount++
		}
	}

	// Log the summary of staleness checks, including total, stale, and failed counts.
	logrus.WithFields(logrus.Fields{
		"total":  len(containers),
		"stale":  staleCount,
		"failed": staleCheckFailed,
	}).Debug("Completed container staleness check")

	// Build a map for a lookup of containers by ID.
	containerByID := make(map[types.ContainerID]types.Container, len(allContainers))
	for _, ac := range allContainers {
		containerByID[ac.ID()] = ac
	}

	// Propagate stale status to allContainers since they are different instances.
	for _, c := range containers {
		if c.IsStale() {
			if ac, ok := containerByID[c.ID()]; ok {
				ac.SetStale(true)
			}
		}
	}

	// Sort containers by dependencies to ensure correct update and restart order.
	err = sorter.SortByDependencies(containers)
	if err != nil {
		if errors.Is(err, sorter.ErrCircularReference) {
			var circularErr sorter.CircularReferenceError
			if errors.As(err, &circularErr) {
				circularName := circularErr.ContainerName
				// Find the container and mark as skipped.
				for _, c := range containers {
					if c.Name() == circularName {
						// Only add if not already skipped (e.g., from initial cycle detection)
						if _, exists := (*progress)[c.ID()]; !exists {
							progress.AddSkipped(c, errCircularDependency, params)
							logrus.Warnf(
								"Skipping container update (circular dependency): %s (%s)",
								c.Name(),
								c.ID().ShortID(),
							)
						}

						break
					}
				}
			}
			// Skip UpdateImplicitRestart to avoid potential issues with circular dependencies.
		} else {
			// Log and return an error if dependency sorting fails for other reasons.
			logrus.WithError(err).Debug("Failed to sort containers by dependencies")

			return nil, []types.CleanedImageInfo{}, fmt.Errorf(
				"%w: %w",
				errSortDependenciesFailed,
				err,
			)
		}
	} else {
		// Mark containers linked to restarting ones for restart without updating.
		UpdateImplicitRestart(containers, allContainers)
	}

	// Collect all containers to restart (updates and implicit restarts)
	var allContainersToRestart []types.Container

	for _, c := range containers {
		if c.ToRestart() && !c.IsMonitorOnly(params) {
			allContainersToRestart = append(allContainersToRestart, c)
		}
	}

	// Sort containers to restart by dependencies to ensure correct update and restart order.
	err = sorter.SortByDependencies(allContainersToRestart)
	if err != nil {
		logrus.WithError(err).Debug("Failed to sort all containers to restart by dependencies")

		return nil, []types.CleanedImageInfo{}, fmt.Errorf(
			"%w: %w",
			errSortDependenciesFailed,
			err,
		)
	}

	// Log the number of containers prepared for restart.
	logrus.WithField("restart_count", len(allContainersToRestart)).
		Debug("Prepared containers for restart")

	// Perform updates and restarts, either with rolling restarts or in batches.
	var (
		failedStop    map[types.ContainerID]error
		stoppedImages []types.CleanedImageInfo
		failedStart   map[types.ContainerID]error
	)

	if params.RollingRestart {
		// Apply rolling restarts for all containers in dependency order.
		progress.UpdateFailed(
			performRollingRestart(
				allContainersToRestart,
				client,
				params,
				&cleanupImageInfos,
				progress,
			),
		)
	} else {
		// Mark containers to update for update in progress
		for _, c := range allContainersToRestart {
			if c.IsStale() {
				progress.MarkForUpdate(c.ID())
			}
		}

		// Stop and restart containers in batches, respecting dependency order.
		failedStop, stoppedImages = stopContainersInReversedOrder(allContainersToRestart, client, params)
		progress.UpdateFailed(failedStop)

		failedStart = restartContainersInSortedOrder(allContainersToRestart, client, params, stoppedImages, &cleanupImageInfos, progress)
		progress.UpdateFailed(failedStart)
	}

	// Run post-check lifecycle hooks if enabled to finalize the update process.
	if params.LifecycleHooks {
		logrus.Debug("Executing post-check lifecycle hooks")
		lifecycle.ExecutePostChecks(client, params)
	}

	// Add safeguard delay if Watchtower self-update pull failed to prevent rapid restarts.
	if watchtowerPullFailed {
		delay := params.PullFailureDelay
		if delay == 0 {
			delay = defaultPullFailureDelay // Default delay
		}

		logrus.WithField("delay", delay).
			Info("Watchtower self-update pull failed, sleeping to prevent rapid restarts")
		time.Sleep(delay)
	}

	// Return the final report summarizing the session and the cleanup image infos.
	return progress.Report(), cleanupImageInfos, nil
}

// UpdateImplicitRestart marks containers linked to restarting ones.
//
// It checks each container's links, marking those dependent on restarting containers to ensure
// they are restarted in the correct order without being marked as updated.
//
// Parameters:
//   - containers: List of containers to update.
//   - allContainers: List of all containers to search for linked dependencies.
func UpdateImplicitRestart(containers []types.Container, allContainers []types.Container) {
	logrus.Debug("Starting UpdateImplicitRestart")

	byID := make(map[types.ContainerID]types.Container, len(allContainers))

	restartByIdent := make(map[string]bool, len(allContainers))
	for _, c := range allContainers {
		byID[c.ID()] = c
		restartByIdent[container.ResolveContainerIdentifier(c)] = c.ToRestart()
	}

	markedContainers := []string{}

	for i, c := range containers {
		if c.ToRestart() {
			continue // Skip already marked containers.
		}

		// c.Links() already returns normalized container names
		links := c.Links()
		logrus.WithFields(logrus.Fields{
			"container": c.Name(),
			"links":     links,
		}).Debug("Checking links for container")

		if link := linkedIdentifierMarkedForRestart(links, restartByIdent); link != "" {
			logrus.WithFields(logrus.Fields{
				"container":  c.Name(),
				"restarting": link,
			}).Debug("Marked container as linked to restarting")
			containers[i].SetLinkedToRestarting(true)

			if ac, ok := byID[c.ID()]; ok {
				ac.SetLinkedToRestarting(true)
				restartByIdent[container.ResolveContainerIdentifier(ac)] = true
			}

			markedContainers = append(markedContainers, c.Name())
		}
	}

	logrus.WithField("marked_containers", markedContainers).Debug("Completed UpdateImplicitRestart")
}

// shouldUpdateContainer determines if a container should be updated based on its staleness and update parameters.
//
// It checks multiple conditions:
// - The container must be stale (outdated image)
// - The container must not be monitor-only
// - Updates are allowed unless NoRestart is true and it's not a Watchtower container
// - Watchtower containers are skipped in run-once mode
// - Watchtower self-updates are skipped if SkipSelfUpdate is true
//
// Parameters:
//   - stale: Whether the container's image is outdated.
//   - container: The container to check.
//   - params: Update parameters controlling update behavior.
//
// Returns:
//   - bool: True if the container should be updated, false otherwise.
func shouldUpdateContainer(stale bool, container types.Container, params types.UpdateParams) bool {
	// Must be stale to update
	if !stale {
		return false
	}

	// Skip monitor-only containers
	if container.IsMonitorOnly(params) {
		return false
	}

	// Allow update if NoRestart is false, or if it's a Watchtower container (which can update even with NoRestart)
	if params.NoRestart && !container.IsWatchtower() {
		return false
	}

	// Skip Watchtower self-update in run-once mode
	if params.RunOnce && container.IsWatchtower() {
		return false
	}

	// Skip Watchtower self-update if SkipSelfUpdate is true
	if params.SkipSelfUpdate && container.IsWatchtower() {
		return false
	}

	// Skip other Watchtower containers from self-updates
	if container.IsWatchtower() && params.CurrentContainerID != "" &&
		container.ID() != params.CurrentContainerID {
		return false
	}

	return true
}

// linkedIdentifierMarkedForRestart finds a restarting linked container by identifier.
//
// It searches for a container identifier in the links list that is marked for restart, returning its identifier.
//
// Parameters:
//   - links: List of linked container identifiers.
//   - restartByIdent: Map of container identifiers to restart status.
//
// Returns:
//   - string: Identifier of restarting linked container, or empty if none.
func linkedIdentifierMarkedForRestart(links []string, restartByIdent map[string]bool) string {
	for _, ident := range links {
		if restartByIdent[ident] {
			return ident
		}
	}

	return ""
}

// parseReference parses a Docker image reference with logging.
// Logs the parsing result or error, including image details and reference type.
func parseReference(
	imageName, configImage, fallbackImage string,
	container types.Container,
) (reference.Reference, error) {
	// Set up logging with container and image details.
	clog := logrus.WithFields(logrus.Fields{
		"container": container.Name(),
		"image":     imageName,
	})

	// Parse the image reference using the Docker reference library.
	normalizedRef, err := reference.ParseDockerRef(imageName)
	if err != nil {
		clog.WithError(err).
			WithField("image_name", imageName).
			Debug("Failed to parse image reference")

		return nil, fmt.Errorf("failed to parse image reference %s: %w", imageName, err)
	}

	// Log successful parsing with reference type and context.
	clog.WithFields(logrus.Fields{
		"image_name":     imageName,
		"config_image":   configImage,
		"fallback_image": fallbackImage,
		"ref_type":       fmt.Sprintf("%T", normalizedRef),
	}).Debug("Parsed image reference")

	return normalizedRef, nil
}

// isPinned checks if a container’s image is pinned by a digest reference.
//
// It selects a valid image name from ImageName(), Config.Image,
// or a fallback (imageInfo.ID or container name),
// parsing it to detect digest-based references (e.g., @sha256:...).
// If pinned, it marks the container as scanned in the progress report
// to skip updates, preserving immutability.
//
// Parameters:
//   - container: The container to check for a pinned image.
//   - progress: The progress tracker to update for scanned or skipped containers.
//   - params: Update parameters for monitor-only check.
//
// Returns:
//   - bool: True if the image is pinned by digest, false otherwise.
//   - error: Non-nil if no valid image reference can be parsed, nil on success.
func isPinned(
	container types.Container,
	progress *session.Progress,
	params types.UpdateParams,
) (bool, error) {
	// Set up logging with container and image details for debugging.
	clog := logrus.WithFields(logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	})

	// Get initial image name and configuration.
	imageName := container.ImageName()
	configImage := container.ContainerInfo().Config.Image
	fallbackImage := getFallbackImage(container)

	// Check if ImageName is invalid and fall back to Config.Image or a derived name.
	if isInvalidImageName(imageName) {
		clog.WithField("invalid_image", imageName).Debug("Invalid ImageName detected")

		if configImage != "" && !isInvalidImageName(configImage) {
			imageName = configImage
			clog.WithField("config_image", configImage).Debug("Using Config.Image as fallback")
		} else {
			imageName = fallbackImage
			clog.WithField("fallback_image", fallbackImage).Debug("Using derived fallback image")
		}
	}

	// If the final imageName is still invalid, skip the container.
	if isInvalidImageName(imageName) {
		return false, errInvalidImageReference
	}

	// Parse the selected image name to check for a digest-based reference.
	normalizedRef, err := parseReference(imageName, configImage, fallbackImage, container)
	if err != nil && imageName != fallbackImage {
		// Retry parsing with the fallback image if the initial attempt failed.
		clog.Debug("Retrying with fallback image")

		normalizedRef, err = parseReference(fallbackImage, configImage, fallbackImage, container)
	}

	if err != nil {
		return false, err
	}

	// Check if the parsed reference is digest-based (pinned).
	_, isDigested := normalizedRef.(reference.Digested)
	if isDigested {
		// Mark the container as scanned to skip updates for pinned images.
		clog.WithField("is_digested", isDigested).Debug("Pinned image detected, marking as scanned")
		progress.AddScanned(container, container.SafeImageID(), params)
	}

	return isDigested, nil
}

// getFallbackImage derives a fallback image name from container info.
// Uses the container name with ":latest" as the fallback image.
func getFallbackImage(container types.Container) string {
	return container.Name() + ":latest"
}

// isInvalidImageName checks if an image name is invalid.
// Returns true if the name is empty, ":latest", or starts with ":".
func isInvalidImageName(name string) bool {
	return name == "" || name == ":latest" || strings.HasPrefix(name, ":")
}

// performRollingRestart updates containers with rolling restarts.
//
// It processes containers sequentially in forward order, stopping and restarting each as needed,
// collecting cleaned image info for stale containers only to ensure proper cleanup.
//
// Parameters:
//   - containers: List of containers to update or restart.
//   - client: Container client for Docker operations.
//   - params: Update options controlling restart behavior.
//   - cleanupImageInfos: Pointer to slice to collect cleaned image info for deferred cleanup.
//   - progress: Progress tracker to update with new container IDs.
//
// Returns:
//   - map[types.ContainerID]error: Map of container IDs to errors for failed updates.
func performRollingRestart(
	containers []types.Container,
	client container.Client,
	params types.UpdateParams,
	cleanupImageInfos *[]types.CleanedImageInfo,
	progress *session.Progress,
) map[types.ContainerID]error {
	failed := make(map[types.ContainerID]error, len(containers))

	containerNames := make([]string, len(containers))
	for i, c := range containers {
		containerNames[i] = c.Name()
	}

	logrus.WithField("processing_order", containerNames).Debug("Starting performRollingRestart")

	// Process containers in forward order to respect dependency chains.
	for i := range containers {
		c := containers[i]
		if !c.ToRestart() {
			continue
		}

		fields := logrus.Fields{
			"container": c.Name(),
			"image":     c.ImageName(),
		}

		logrus.WithFields(fields).Debug("Processing container for rolling restart")

		// Mark for update if stale
		if c.IsStale() && progress != nil {
			progress.MarkForUpdate(c.ID())
		}

		// Stop the container, handling any errors.
		if err := stopStaleContainer(c, client, params); err != nil {
			failed[c.ID()] = err
		} else {
			newContainerID, renamed, err := restartStaleContainer(c, client, params)
			if err != nil {
				failed[c.ID()] = err
			} else {
				// Set the new container ID in progress
				if progress != nil {
					if status, exists := (*progress)[c.ID()]; exists {
						status.SetNewContainerID(newContainerID)
						// Mark as restarted if not stale (not updated)
						if !c.IsStale() {
							progress.MarkRestarted(c.ID())
						}
					}
				}

				// Wait for the container to become healthy if it has a health check
				if waitErr := client.WaitForContainerHealthy(newContainerID, defaultHealthCheckTimeout); waitErr != nil {
					logrus.WithFields(fields).WithError(waitErr).Warn("Failed to wait for container to become healthy")
					// Don't fail the update, just log the warning
				}

				if c.IsStale() && !renamed {
					// Only collect cleaned image info for stale containers that were not renamed, as renamed
					// containers (Watchtower self-updates) are cleaned up by CheckForMultipleWatchtowerInstances
					// in the new container.
					addCleanupImageInfo(cleanupImageInfos, c.ImageID(), c.ImageName(), c.Name(), c.ID())

					logrus.WithFields(fields).Debug("Updated container")
				}
			}
		}
	}

	return failed
}

// stopContainersInReversedOrder stops containers in reverse order.
//
// It stops each container, tracking stopped images and errors, to prepare for restarts while
// respecting dependency order.
//
// Parameters:
//   - containers: List of containers to stop.
//   - client: Container client for Docker operations.
//   - params: Update options specifying stop timeout and other behaviors.
//
// Returns:
//   - map[types.ContainerID]error: Map of container IDs to errors for failed stops.
//   - []types.CleanedImageInfo: Slice of cleaned image info for stopped containers.
func stopContainersInReversedOrder(
	containers []types.Container,
	client container.Client,
	params types.UpdateParams,
) (map[types.ContainerID]error, []types.CleanedImageInfo) {
	failed := make(map[types.ContainerID]error, len(containers))
	stopped := make([]types.CleanedImageInfo, 0, len(containers))

	// Stop containers in reverse order to avoid breaking dependencies.
	for i := len(containers) - 1; i >= 0; i-- {
		c := containers[i]
		fields := logrus.Fields{
			"container": c.Name(),
			"image":     c.ImageName(),
		}

		if err := stopStaleContainer(c, client, params); err != nil {
			failed[c.ID()] = err
		} else {
			stopped = append(stopped, types.CleanedImageInfo{ImageID: c.SafeImageID(), ContainerID: c.ID(), ImageName: c.ImageName(), ContainerName: c.Name()})

			logrus.WithFields(fields).Debug("Stopped container")
		}
	}

	return failed, stopped
}

// stopStaleContainer stops a stale container if eligible.
//
// It skips Watchtower containers or those not marked for restart, runs pre-update hooks if enabled,
// and stops the container with the specified timeout.
//
// Parameters:
//   - container: Container to stop.
//   - client: Container client for Docker operations.
//   - params: Update options specifying stop timeout and lifecycle hooks.
//
// Returns:
//   - error: Non-nil if stop fails, nil on success or if skipped.
func stopStaleContainer(
	container types.Container,
	client container.Client,
	params types.UpdateParams,
) error {
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	// Skip Watchtower containers to avoid self-interruption.
	if container.IsWatchtower() {
		logrus.WithFields(fields).Debug("Skipping Watchtower container")

		return nil
	}

	// Skip containers not marked for restart (e.g., not stale or linked).
	if !container.ToRestart() {
		return nil
	}

	logrus.WithFields(fields).Debug("Stopping container for restart")

	// Verify configuration for linked containers to ensure restart compatibility.
	if container.IsLinkedToRestarting() {
		if err := container.VerifyConfiguration(); err != nil {
			logrus.WithFields(fields).
				WithError(err).
				Debug("Failed to verify container configuration")

			return fmt.Errorf("%w: %w", errVerifyConfigFailed, err)
		}
	}

	// Execute pre-update lifecycle hooks if enabled, checking for skip conditions.
	if params.LifecycleHooks {
		skipUpdate, err := lifecycle.ExecutePreUpdateCommand(
			client,
			container,
			params.LifecycleUID,
			params.LifecycleGID,
		)
		if err != nil {
			logrus.WithFields(fields).WithError(err).Debug("Pre-update command execution failed")

			return fmt.Errorf("%w: %w", errPreUpdateFailed, err)
		}

		if skipUpdate {
			logrus.WithFields(fields).Debug("Skipping container due to pre-update exit code 75")

			return errSkipUpdate
		}
	}

	// Stop the container with the configured timeout.
	if err := client.StopAndRemoveContainer(container, params.Timeout); err != nil {
		logrus.WithFields(fields).WithError(err).Error("Failed to stop container")

		return fmt.Errorf("%w: %w", errStopContainerFailed, err)
	}

	return nil
}

// restartContainersInSortedOrder restarts stopped containers.
//
// It restarts containers in dependency order, collecting cleaned image info for stale containers that were not
// renamed during a self-update, and tracking any restart failures.
//
// Parameters:
//   - containers: List of containers to restart.
//   - client: Container client for Docker operations.
//   - params: Update options controlling restart behavior.
//   - stoppedImages: Slice of cleaned image info for previously stopped containers.
//   - cleanupImageInfos: Pointer to slice to collect cleaned image info for deferred cleanup.
//   - progress: Progress tracker to update with new container IDs.
//
// Returns:
//   - map[types.ContainerID]error: Map of container IDs to errors for failed restarts.
func restartContainersInSortedOrder(
	containers []types.Container,
	client container.Client,
	params types.UpdateParams,
	stoppedImages []types.CleanedImageInfo,
	cleanupImageInfos *[]types.CleanedImageInfo,
	progress *session.Progress,
) map[types.ContainerID]error {
	failed := make(map[types.ContainerID]error, len(containers))
	// Track renamed containers to skip cleanup.
	renamedContainers := make(map[types.ContainerID]bool)

	// Restart containers in sorted order to respect dependency chains.
	for _, c := range containers {
		if !c.ToRestart() {
			continue
		}

		fields := logrus.Fields{
			"container": c.Name(),
			"image":     c.ImageName(),
		}

		// Check if container was previously stopped by looking in stoppedImages slice.
		wasStopped := false

		for _, stopped := range stoppedImages {
			if stopped.ImageID == c.SafeImageID() {
				wasStopped = true

				break
			}
		}

		// Skip other Watchtower containers from self-updates
		if c.IsWatchtower() && params.CurrentContainerID != "" &&
			c.ID() != params.CurrentContainerID {
			continue
		}

		// Restart Watchtower containers regardless of stoppedImages, as they are renamed.
		// Otherwise, restart only containers that were previously stopped.
		if c.IsWatchtower() || wasStopped {
			newContainerID, renamed, err := restartStaleContainer(c, client, params)
			if err != nil {
				failed[c.ID()] = err
			} else {
				// Set the new container ID in progress
				if progress != nil {
					if status, exists := (*progress)[c.ID()]; exists {
						status.SetNewContainerID(newContainerID)
						// Mark as restarted if not stale (not updated)
						if !c.IsStale() {
							progress.MarkRestarted(c.ID())
						}
					}
				}

				logrus.WithFields(fields).Debug("Restarted container")

				if renamed {
					renamedContainers[c.ID()] = true
				}
				// Only collect cleaned image info for stale containers that were not renamed, as renamed
				// containers (Watchtower self-updates) are cleaned up by CheckForMultipleWatchtowerInstances
				// in the new container.
				if c.IsStale() && !renamedContainers[c.ID()] {
					addCleanupImageInfo(cleanupImageInfos, c.ImageID(), c.ImageName(), c.Name(), c.ID())
				}
			}
		}
	}

	return failed
}

// addCleanupImageInfo adds cleanup info if not already present.
//
// Parameters:
//   - cleanupImageInfos: Pointer to slice to collect cleaned image info.
//   - imageID: ID of the image to clean up.
//   - imageName: Name of the image.
//   - containerName: Name of the container.
//   - containerID: ID of the container (optional, pass empty string if not available).
func addCleanupImageInfo(
	cleanupImageInfos *[]types.CleanedImageInfo,
	imageID types.ImageID,
	imageName, containerName string,
	containerID types.ContainerID,
) {
	for _, existing := range *cleanupImageInfos {
		if existing.ImageID == imageID {
			return
		}
	}

	*cleanupImageInfos = append(*cleanupImageInfos, types.CleanedImageInfo{
		ImageID:       imageID,
		ContainerID:   containerID,
		ImageName:     imageName,
		ContainerName: containerName,
	})
}

// restartStaleContainer restarts a stale container.
//
// It renames Watchtower containers if applicable,
// starts a new container, and runs post-update hooks,
// handling errors for each step.
//
// Parameters:
//   - container: Container to restart.
//   - client: Container client for Docker operations.
//   - params: Update options controlling restart and lifecycle hooks.
//
// Returns:
//   - types.ContainerID: ID of the new container if started, original ID if renamed only, empty otherwise.
//   - bool: True if the container was renamed, false otherwise.
//   - error: Non-nil if restart fails, nil on success.
func restartStaleContainer(
	container types.Container,
	client container.Client,
	params types.UpdateParams,
) (types.ContainerID, bool, error) {
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	renamed := false
	newContainerID := container.ID() // Default to original ID

	// Rename Watchtower containers regardless of NoRestart flag, but skip in run-once mode
	// as there's no need to avoid conflicts with a continuously running instance.
	if container.IsWatchtower() && !params.RunOnce {
		newName := util.RandName()
		if err := client.RenameContainer(container, newName); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"container": container.Name(),
				"new_name":  newName,
			}).Debug("Failed to rename Watchtower container")

			return "", false, fmt.Errorf("%w: %w", errRenameWatchtowerFailed, err)
		}

		logrus.WithFields(fields).
			WithField("new_name", newName).
			Debug("Renamed Watchtower container")

		renamed = true
	}

	// Start the new container unless restarts are disabled (but always start for Watchtower).
	if !params.NoRestart || container.IsWatchtower() {
		logrus.WithFields(fields).Debug("Starting container after update/restart")

		var err error

		newContainerID, err = client.StartContainer(container)
		if err != nil {
			logrus.WithFields(fields).WithError(err).Debug("Failed to start container")
			// Clean up renamed Watchtower container on failure
			if renamed && container.IsWatchtower() {
				logrus.WithFields(fields).Debug("Cleaning up failed Watchtower container")

				if cleanupErr := client.StopAndRemoveContainer(container, params.Timeout); cleanupErr != nil {
					logrus.WithError(cleanupErr).
						WithFields(fields).
						Debug("Failed to stop failed Watchtower container")
				}
			}

			return "", renamed, fmt.Errorf("%w: %w", errStartContainerFailed, err)
		}

		// Run post-update lifecycle hooks for restarting containers if enabled.
		if container.ToRestart() && params.LifecycleHooks {
			logrus.WithFields(fields).Debug("Executing post-update command")
			lifecycle.ExecutePostUpdateCommand(
				client,
				newContainerID,
				params.LifecycleUID,
				params.LifecycleGID,
			)
		}
	}

	// For renamed Watchtower containers, update restart policy to "no" and stop the old container
	if renamed && container.IsWatchtower() {
		logrus.WithFields(fields).
			Debug("Updating restart policy and stopping old Watchtower container")

		// Update restart policy to "no"
		updateConfig := dockerContainer.UpdateConfig{
			RestartPolicy: dockerContainer.RestartPolicy{
				Name: "no",
			},
		}
		if err := client.UpdateContainer(container, updateConfig); err != nil {
			logrus.WithError(err).
				WithFields(fields).
				Warn("Failed to update restart policy for old Watchtower container")

			// Continue with stopping even if update fails
		}

		// Attempt to stop and remove the old Watchtower container gracefully.
		if err := client.StopAndRemoveContainer(container, params.Timeout); err != nil {
			logrus.WithError(err).
				WithFields(fields).
				Debug("Failed to stop and remove old Watchtower container")

			// Don't fail the update, just log the warning
		} else {
			logrus.WithFields(fields).Debug("Attempted to stop and remove old Watchtower container")

			// If the container is still running, force remove it
			// This is a backup redundancy to help mitigate race conditions
			// and timing anomalies in production environments resulting in
			// orphaned containers.
			if err := client.RemoveContainer(container); err != nil {
				logrus.WithError(err).WithFields(fields).Debug("Failed to remove old Watchtower container")
			} else {
				logrus.WithFields(fields).Debug("Removed old Watchtower container")
			}
		}
	}

	return newContainerID, renamed, nil
}
