// Package actions provides core logic for Watchtower’s container update operations.
package actions

import (
	"fmt"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/container"
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
//   - client: Container client for interacting with Docker API.
//   - config: UpdateConfig specifying behavior like cleanup, restart, and filtering.
//
// Returns:
//   - types.Report: Session report summarizing scanned, updated, and failed containers.
//   - []types.CleanedImageInfo: Slice of cleaned image info to clean up after updates.
//   - error: Non-nil if listing or sorting fails, nil on success.
func Update(
	client container.Client,
	config UpdateConfig,
) (types.Report, []types.CleanedImageInfo, error) {
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
		Filter:           config.Filter,
		Cleanup:          config.Cleanup,
		NoRestart:        config.NoRestart,
		Timeout:          config.Timeout,
		MonitorOnly:      config.MonitorOnly,
		LifecycleHooks:   config.LifecycleHooks,
		RollingRestart:   config.RollingRestart,
		LabelPrecedence:  config.LabelPrecedence,
		NoPull:           config.NoPull,
		PullFailureDelay: config.PullFailureDelay,
		LifecycleUID:     config.LifecycleUID,
		LifecycleGID:     config.LifecycleGID,
		CPUCopyMode:      config.CPUCopyMode,
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

		return nil, []types.CleanedImageInfo{}, fmt.Errorf("%w: %w", errListContainersFailed, err)
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

	// Track containers that fail staleness checks for reporting.
	staleCheckFailed := 0

	// Iterate through containers to check staleness and prepare for updates or restarts.
	for i, sourceContainer := range containers {
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
		shouldUpdate := stale && !params.NoRestart && !sourceContainer.IsMonitorOnly(params)

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
			// Log successful staleness check and add to scanned containers.
			clog.WithFields(logrus.Fields{
				"stale":        stale,
				"newest_image": newestImage,
			}).Debug("Checked container staleness")
			progress.AddScanned(sourceContainer, newestImage, params)
		}

		// Update the container’s stale status for dependency sorting.
		containers[i].SetStale(stale)

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

	// Sort containers by dependencies to ensure correct update and restart order.
	containers, err = sorter.SortByDependencies(containers)
	if err != nil {
		// Log and return an error if dependency sorting fails.
		logrus.WithError(err).Debug("Failed to sort containers by dependencies")

		return nil, []types.CleanedImageInfo{}, fmt.Errorf(
			"%w: %w",
			errSortDependenciesFailed,
			err,
		)
	}

	// Mark containers linked to restarting ones for restart without updating.
	UpdateImplicitRestart(containers)

	// Separate containers into those to update (stale) and those to restart (linked).
	var (
		containersToUpdate  []types.Container
		containersToRestart []types.Container
	)

	for _, c := range containers {
		if c.IsStale() && !c.IsMonitorOnly(params) {
			// Add stale containers to the update list and mark for update in progress.
			containersToUpdate = append(containersToUpdate, c)
			progress.MarkForUpdate(c.ID())
		} else if c.ToRestart() && !c.IsMonitorOnly(params) {
			// Add linked containers to the restart list.
			containersToRestart = append(containersToRestart, c)
		}
	}

	// Log the number of containers prepared for update and restart.
	logrus.WithField("update_count", len(containersToUpdate)).
		Debug("Prepared containers for update")
	logrus.WithField("restart_count", len(containersToRestart)).
		Debug("Prepared containers for restart")

	// Perform updates and restarts, either with rolling restarts or in batches.
	var (
		failedStop    map[types.ContainerID]error
		stoppedImages []types.CleanedImageInfo
		failedStart   map[types.ContainerID]error
	)

	if params.RollingRestart {
		// Apply rolling restarts for updates and linked containers sequentially.
		progress.UpdateFailed(
			performRollingRestart(containersToUpdate, client, params, &cleanupImageInfos),
		)
		progress.UpdateFailed(
			performRollingRestart(containersToRestart, client, params, &cleanupImageInfos),
		)
	} else {
		// Stop and restart containers in batches, respecting dependency order.
		failedStop, stoppedImages = stopContainersInReversedOrder(containersToUpdate, client, params)
		progress.UpdateFailed(failedStop)

		failedStart = restartContainersInSortedOrder(containersToUpdate, client, params, stoppedImages, &cleanupImageInfos)
		progress.UpdateFailed(failedStart)

		failedStop, stoppedImages = stopContainersInReversedOrder(containersToRestart, client, params)
		progress.UpdateFailed(failedStop)

		failedStart = restartContainersInSortedOrder(containersToRestart, client, params, stoppedImages, &cleanupImageInfos)
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

// isInvalidImageName checks if an image name is invalid.
// Returns true if the name is empty, ":latest", or starts with ":".
func isInvalidImageName(name string) bool {
	return name == "" || name == ":latest" || strings.HasPrefix(name, ":")
}

// getFallbackImage derives a fallback image name from container info.
// Uses sanitized imageInfo.ID if it contains a tag, otherwise uses sanitized container name with ":latest".
func getFallbackImage(container types.Container) string {
	if container.HasImageInfo() {
		fallback := strings.TrimPrefix(container.ImageInfo().ID, "sha256:")
		if !strings.Contains(fallback, ":") {
			return strings.TrimPrefix(container.Name(), "/") + ":latest"
		}

		return fallback
	}

	return strings.TrimPrefix(container.Name(), "/") + ":latest"
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
// It selects a valid image name from ImageName(), Config.Image, or a fallback (imageInfo.ID or container name),
// parsing it to detect digest-based references (e.g., @sha256:...). If pinned, it marks the container as scanned
// in the progress report to skip updates, preserving immutability.
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

		if configImage != "" {
			imageName = configImage
			clog.WithField("config_image", configImage).Debug("Using Config.Image as fallback")
		} else {
			imageName = fallbackImage
			clog.WithField("fallback_image", fallbackImage).Debug("Using derived fallback image")
		}
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

// performRollingRestart updates containers with rolling restarts.
//
// It processes containers sequentially in reverse order, stopping and restarting each as needed,
// collecting cleaned image info for stale containers only to ensure proper cleanup.
//
// Parameters:
//   - containers: List of containers to update or restart.
//   - client: Container client for Docker operations.
//   - params: Update options controlling restart behavior.
//   - cleanupImageInfos: Pointer to slice to collect cleaned image info for deferred cleanup.
//
// Returns:
//   - map[types.ContainerID]error: Map of container IDs to errors for failed updates.
func performRollingRestart(
	containers []types.Container,
	client container.Client,
	params types.UpdateParams,
	cleanupImageInfos *[]types.CleanedImageInfo,
) map[types.ContainerID]error {
	failed := make(map[types.ContainerID]error, len(containers))

	// Process containers in reverse order to respect dependency chains.
	for i := len(containers) - 1; i >= 0; i-- {
		c := containers[i]
		if !c.ToRestart() {
			continue
		}

		fields := logrus.Fields{
			"container": c.Name(),
			"image":     c.ImageName(),
		}

		logrus.WithFields(fields).Debug("Processing container for rolling restart")

		// Stop the container, handling any errors.
		if err := stopStaleContainer(c, client, params); err != nil {
			failed[c.ID()] = err
		} else {
			newContainerID, renamed, err := restartStaleContainer(c, client, params)
			if err != nil {
				failed[c.ID()] = err
			} else {
				// Wait for the container to become healthy if it has a health check
				if waitErr := client.WaitForContainerHealthy(newContainerID, defaultHealthCheckTimeout); waitErr != nil {
					logrus.WithFields(fields).WithError(waitErr).Warn("Failed to wait for container to become healthy")
					// Don't fail the update, just log the warning
				}

				if c.IsStale() && !renamed {
					// Only collect cleaned image info for stale containers that were not renamed, as renamed
					// containers (Watchtower self-updates) are cleaned up by CheckForMultipleWatchtowerInstances
					// in the new container.
					addCleanupImageInfo(cleanupImageInfos, c.ImageID(), c.ImageName(), c.Name())

					logrus.WithFields(fields).Info("Updated container")
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
			stopped = append(stopped, types.CleanedImageInfo{ImageID: c.SafeImageID(), ImageName: c.ImageName(), ContainerName: c.Name()})

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
	if err := client.StopContainer(container, params.Timeout); err != nil {
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
//
// Returns:
//   - map[types.ContainerID]error: Map of container IDs to errors for failed restarts.
func restartContainersInSortedOrder(
	containers []types.Container,
	client container.Client,
	params types.UpdateParams,
	stoppedImages []types.CleanedImageInfo,
	cleanupImageInfos *[]types.CleanedImageInfo,
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

		// Restart Watchtower containers regardless of stoppedImages, as they are renamed.
		// Otherwise, restart only containers that were previously stopped.
		if c.IsWatchtower() || wasStopped {
			_, renamed, err := restartStaleContainer(c, client, params)
			if err != nil {
				failed[c.ID()] = err
			} else {
				logrus.WithFields(fields).Debug("Restarted container")

				if renamed {
					renamedContainers[c.ID()] = true
				}
				// Only collect cleaned image info for stale containers that were not renamed, as renamed
				// containers (Watchtower self-updates) are cleaned up by CheckForMultipleWatchtowerInstances
				// in the new container.
				if c.IsStale() && !renamedContainers[c.ID()] {
					addCleanupImageInfo(cleanupImageInfos, c.ImageID(), c.ImageName(), c.Name())
				}
			}
		}
	}

	return failed
}

// addCleanupImageInfo adds cleanup info if not already present.
func addCleanupImageInfo(
	cleanupImageInfos *[]types.CleanedImageInfo,
	imageID types.ImageID,
	imageName, containerName string,
) {
	for _, existing := range *cleanupImageInfos {
		if existing.ImageID == imageID {
			return
		}
	}

	*cleanupImageInfos = append(*cleanupImageInfos, types.CleanedImageInfo{
		ImageID:       imageID,
		ImageName:     imageName,
		ContainerName: containerName,
	})
}

// restartStaleContainer restarts a stale container.
//
// It renames Watchtower containers if applicable, starts a new container, and runs post-update hooks,
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
	// Rename Watchtower containers only if restarts are enabled.
	if container.IsWatchtower() && !params.NoRestart {
		// Check pull success before renaming
		stale, _, err := client.IsContainerStale(container, params)
		if err != nil || !stale {
			logrus.WithFields(fields).
				WithError(err).
				Debug("Skipping Watchtower self-update due to pull failure or non-stale image")

			return container.ID(), false, nil // Skip self-update without error
		}

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

	// Start the new container unless restarts are disabled.
	if !params.NoRestart {
		var err error

		newContainerID, err = client.StartContainer(container)
		if err != nil {
			logrus.WithFields(fields).WithError(err).Debug("Failed to start container")
			// Clean up renamed Watchtower container on failure
			if renamed && container.IsWatchtower() {
				logrus.WithFields(fields).Debug("Cleaning up failed Watchtower container")

				if cleanupErr := client.StopContainer(container, params.Timeout); cleanupErr != nil {
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

	return newContainerID, renamed, nil
}

// UpdateImplicitRestart marks containers linked to restarting ones.
//
// It checks each container's links, marking those dependent on restarting containers to ensure
// they are restarted in the correct order without being marked as updated.
//
// Parameters:
//   - containers: List of containers to update.
func UpdateImplicitRestart(containers []types.Container) {
	for i, c := range containers {
		if c.ToRestart() {
			continue // Skip already marked containers.
		}

		if link := linkedContainerMarkedForRestart(c.Links(), containers); link != "" {
			logrus.WithFields(logrus.Fields{
				"container":  c.Name(),
				"restarting": link,
			}).Debug("Marked container as linked to restarting")
			containers[i].SetLinkedToRestarting(true)
		}
	}
}

// linkedContainerMarkedForRestart finds a restarting linked container.
//
// It searches for a container in the links list that is marked for restart, returning its name.
//
// Parameters:
//   - links: List of linked container names.
//   - containers: List of containers to check against.
//
// Returns:
//   - string: Name of restarting linked container, or empty if none.
func linkedContainerMarkedForRestart(links []string, containers []types.Container) string {
	for _, linkName := range links {
		for _, candidate := range containers {
			if strings.TrimPrefix(candidate.Name(), "/") == strings.TrimPrefix(linkName, "/") &&
				candidate.ToRestart() {
				return linkName
			}
		}
	}

	return ""
}
