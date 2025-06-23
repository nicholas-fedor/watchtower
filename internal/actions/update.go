// Package actions provides core logic for Watchtower’s container update operations.
package actions

import (
	"errors"
	"fmt"
	"strings"

	"github.com/distribution/reference"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/lifecycle"
	"github.com/nicholas-fedor/watchtower/pkg/session"
	"github.com/nicholas-fedor/watchtower/pkg/sorter"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for update operations.
var (
	// errSortDependenciesFailed indicates a failure to sort containers by dependencies.
	errSortDependenciesFailed = errors.New("failed to sort containers by dependencies")
	// errVerifyConfigFailed indicates a failure to verify container configuration for recreation.
	errVerifyConfigFailed = errors.New("failed to verify container configuration")
	// errPreUpdateFailed indicates a failure in the pre-update lifecycle command execution.
	errPreUpdateFailed = errors.New("pre-update command failed")
	// errSkipUpdate signals that a container update should be skipped due to a specific condition (e.g., EX_TEMPFAIL).
	errSkipUpdate = errors.New(
		"skipping container due to pre-update command exit code 75 (EX_TEMPFAIL)",
	)
	// errRenameWatchtowerFailed indicates a failure to rename the Watchtower container before restarting.
	errRenameWatchtowerFailed = errors.New("failed to rename Watchtower container")
	// errStopContainerFailed indicates a failure to stop a container during the update process.
	errStopContainerFailed = errors.New("failed to stop container")
	// errStartContainerFailed indicates a failure to start a container after an update.
	errStartContainerFailed = errors.New("failed to start container")
	// errParseImageReference indicates a failure to parse a container’s image reference.
	errParseImageReference = errors.New("failed to parse image reference")
)

// Update scans and updates containers based on parameters.
//
// It checks container staleness, sorts by dependencies, and updates or restarts containers as needed,
// collecting image IDs for cleanup. Non-stale linked containers are restarted but not marked as updated.
// Containers with pinned images (referenced by digest) are skipped to preserve immutability.
//
// Parameters:
//   - client: Container client for interacting with Docker API.
//   - params: Update options specifying behavior like cleanup, restart, and filtering.
//
// Returns:
//   - types.Report: Session report summarizing scanned, updated, and failed containers.
//   - map[types.ImageID]bool: Set of image IDs to clean up after updates.
//   - error: Non-nil if listing or sorting fails, nil on success.
func Update(
	client container.Client,
	params types.UpdateParams,
) (types.Report, map[types.ImageID]bool, error) {
	logrus.Debug("Starting container update check")

	progress := &session.Progress{}
	staleCount := 0
	cleanupImageIDs := make(map[types.ImageID]bool)

	// Execute pre-check lifecycle hooks if enabled to validate the environment.
	if params.LifecycleHooks {
		logrus.Debug("Executing pre-check lifecycle hooks")
		lifecycle.ExecutePreChecks(client, params)
	}

	// Retrieve the list of containers matching the filter.
	containers, err := client.ListContainers(params.Filter)
	if err != nil {
		logrus.WithError(err).Debug("Failed to list containers")

		return nil, nil, fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

	// Log container details for debugging.
	containerNames := make([]string, len(containers))
	for i, c := range containers {
		containerNames[i] = fmt.Sprintf("%s (%s)", c.Name(), c.ImageName())
	}

	logrus.WithFields(logrus.Fields{
		"count":      len(containers),
		"containers": containerNames,
		"filter":     fmt.Sprintf("%T", params.Filter),
	}).Debug("Retrieved containers for update check")

	staleCheckFailed := 0

	// Check each container for staleness and prepare for updates.
	for i, sourceContainer := range containers {
		fields := logrus.Fields{
			"container": sourceContainer.Name(),
			"image":     sourceContainer.ImageName(),
		}
		clog := logrus.WithFields(fields)

		// Check if the image is pinned by digest.
		isPinned, err := isPinned(sourceContainer, progress)
		if err != nil {
			clog.WithError(err).Debug("Failed to check pinned image, skipping container")
			progress.AddSkipped(sourceContainer, fmt.Errorf("%w: %w", errParseImageReference, err))

			staleCheckFailed++

			continue
		}

		if isPinned {
			clog.Debug("Skipping staleness check for pinned image")

			continue
		}

		stale, newestImage, err := client.IsContainerStale(sourceContainer, params)
		shouldUpdate := stale && !params.NoRestart && !sourceContainer.IsMonitorOnly(params)

		// Verify configuration for containers that will be updated.
		if err == nil && shouldUpdate {
			err = sourceContainer.VerifyConfiguration()
			if err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"container_name": sourceContainer.Name(),
					"container_id":   sourceContainer.ID().ShortID(),
					"image_name":     sourceContainer.ImageName(),
					"image_id":       sourceContainer.ID().ShortID(),
				}).Debug("Failed to verify container configuration")
			}
		}

		// Handle staleness check results, logging skips or adding to report.
		if err != nil {
			clog.WithError(err).Debug("Cannot update container, skipping")

			stale = false
			staleCheckFailed++

			progress.AddSkipped(sourceContainer, err)
		} else {
			clog.WithFields(logrus.Fields{
				"stale":        stale,
				"newest_image": newestImage,
			}).Debug("Checked container staleness")
			progress.AddScanned(sourceContainer, newestImage)
		}

		containers[i].SetStale(stale)

		if stale {
			staleCount++
		}
	}

	logrus.WithFields(logrus.Fields{
		"total":  len(containers),
		"stale":  staleCount,
		"failed": staleCheckFailed,
	}).Debug("Completed container staleness check")

	// Sort containers to respect dependency order for updates and restarts.
	containers, err = sorter.SortByDependencies(containers)
	if err != nil {
		logrus.WithError(err).Debug("Failed to sort containers by dependencies")

		return nil, nil, fmt.Errorf("%w: %w", errSortDependenciesFailed, err)
	}

	// Mark containers linked to restarting ones for restart, not update.
	UpdateImplicitRestart(containers)

	// Split containers into those to update (stale) and those to restart (linked).
	var containersToUpdate []types.Container

	var containersToRestart []types.Container

	for _, c := range containers {
		if c.IsStale() && !c.IsMonitorOnly(params) {
			containersToUpdate = append(containersToUpdate, c)
			progress.MarkForUpdate(c.ID())
		} else if c.ToRestart() && !c.IsMonitorOnly(params) {
			containersToRestart = append(containersToRestart, c)
		}
	}

	logrus.WithField("update_count", len(containersToUpdate)).
		Debug("Prepared containers for update")
	logrus.WithField("restart_count", len(containersToRestart)).
		Debug("Prepared containers for restart")

	// Perform updates and restarts based on the rolling restart setting.
	var failedStop map[types.ContainerID]error

	var stoppedImages map[types.ImageID]bool

	var failedStart map[types.ContainerID]error

	if params.RollingRestart {
		progress.UpdateFailed(
			performRollingRestart(containersToUpdate, client, params, cleanupImageIDs),
		)
		progress.UpdateFailed(
			performRollingRestart(containersToRestart, client, params, cleanupImageIDs),
		)
	} else {
		failedStop, stoppedImages = stopContainersInReversedOrder(containersToUpdate, client, params)
		progress.UpdateFailed(failedStop)

		failedStart = restartContainersInSortedOrder(containersToUpdate, client, params, stoppedImages, cleanupImageIDs)
		progress.UpdateFailed(failedStart)

		failedStop, stoppedImages = stopContainersInReversedOrder(containersToRestart, client, params)
		progress.UpdateFailed(failedStop)

		failedStart = restartContainersInSortedOrder(containersToRestart, client, params, stoppedImages, cleanupImageIDs)
		progress.UpdateFailed(failedStart)
	}

	// Execute post-check lifecycle hooks if enabled to finalize the update process.
	if params.LifecycleHooks {
		logrus.Debug("Executing post-check lifecycle hooks")
		lifecycle.ExecutePostChecks(client, params)
	}

	return progress.Report(), cleanupImageIDs, nil
}

// isPinned checks if a container’s image is pinned by a digest reference.
//
// It parses the image reference, handling invalid ImageName() outputs and falling back to Config.Image
// or a default name based on the container name. If the image is pinned (digest-based), it updates
// the progress report and returns true.
//
// Parameters:
//   - container: The container to check for a pinned image.
//   - progress: The progress tracker to update for scanned or skipped containers.
//
// Returns:
//   - bool: True if the image is pinned by digest, false otherwise.
//   - error: Non-nil if no valid image reference can be parsed, nil on success.
func isPinned(container types.Container, progress *session.Progress) (bool, error) {
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}
	clog := logrus.WithFields(fields)

	imageName := container.ImageName()
	configImage := container.ContainerInfo().Config.Image

	var fallbackImage string
	// Try to derive a fallback from imageInfo.ID or container name.
	if container.HasImageInfo() {
		fallbackImage = container.ImageInfo().ID
		// Sanitize imageInfo.ID, removing sha256: prefix and ensuring a tag.
		fallbackImage = strings.TrimPrefix(fallbackImage, "sha256:")
		if !strings.Contains(fallbackImage, ":") {
			fallbackImage = container.Name() + ":latest"
		}
	} else {
		fallbackImage = container.Name() + ":latest"
	}

	if strings.HasPrefix(imageName, ":") || imageName == ":latest" || imageName == "" {
		clog.WithField("invalid_image", imageName).
			Debug("Invalid ImageName detected, trying Config.Image")

		if configImage != "" {
			imageName = configImage
		} else {
			clog.WithField("config_image", configImage).Debug("Config.Image is empty, using fallback image")

			imageName = fallbackImage
		}
	}

	normalizedRef, err := reference.ParseDockerRef(imageName)
	if err != nil {
		clog.WithError(err).
			WithField("image_name", imageName).
			Debug("Failed to parse image reference, trying fallback")

		if imageName != fallbackImage {
			imageName = fallbackImage
			normalizedRef, err = reference.ParseDockerRef(imageName)
			if err != nil {
				clog.WithError(err).
					WithField("image_name", imageName).
					Debug("Failed to parse fallback image reference")

				return false, fmt.Errorf("failed to parse image reference %s: %w", imageName, err)
			}
		} else {
			clog.WithError(err).WithField("image_name", imageName).Debug("No valid fallback available")

			return false, fmt.Errorf("failed to parse image reference %s: %w", imageName, err)
		}
	}

	_, isDigested := normalizedRef.(reference.Digested)
	clog.WithFields(logrus.Fields{
		"image_name":     imageName,
		"config_image":   configImage,
		"fallback_image": fallbackImage,
		"ref_type":       fmt.Sprintf("%T", normalizedRef),
		"is_digested":    isDigested,
	}).Debug("Parsed image reference")

	if isDigested {
		progress.AddScanned(container, container.SafeImageID())
	}

	return isDigested, nil
}

// performRollingRestart updates containers with rolling restarts.
//
// It processes containers sequentially in reverse order, stopping and restarting each as needed,
// collecting image IDs for stale containers only to ensure proper cleanup.
//
// Parameters:
//   - containers: List of containers to update or restart.
//   - client: Container client for Docker operations.
//   - params: Update options controlling restart behavior.
//   - cleanupImageIDs: Map to collect image IDs for deferred cleanup.
//
// Returns:
//   - map[types.ContainerID]error: Map of container IDs to errors for failed updates.
func performRollingRestart(
	containers []types.Container,
	client container.Client,
	params types.UpdateParams,
	cleanupImageIDs map[types.ImageID]bool,
) map[types.ContainerID]error {
	failed := make(map[types.ContainerID]error, len(containers))
	// Track renamed containers to skip cleanup.
	renamedContainers := make(map[types.ContainerID]bool)

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
			renamed, err := restartStaleContainer(c, client, params)
			if err != nil {
				failed[c.ID()] = err
			} else if c.IsStale() && !renamed {
				// Only collect image IDs for stale containers that were not renamed, as renamed
				// containers (Watchtower self-updates) are cleaned up by CheckForMultipleWatchtowerInstances
				// in the new container.
				cleanupImageIDs[c.ImageID()] = true

				logrus.WithFields(fields).Info("Updated container")
			}

			if renamed {
				renamedContainers[c.ID()] = true
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
//   - map[types.ImageID]bool: Set of image IDs for stopped containers.
func stopContainersInReversedOrder(
	containers []types.Container,
	client container.Client,
	params types.UpdateParams,
) (map[types.ContainerID]error, map[types.ImageID]bool) {
	failed := make(map[types.ContainerID]error, len(containers))
	stopped := make(map[types.ImageID]bool, len(containers))

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
			stopped[c.SafeImageID()] = true

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
		skipUpdate, err := lifecycle.ExecutePreUpdateCommand(client, container)
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
// It restarts containers in dependency order, collecting image IDs for stale containers that were not
// renamed during a self-update, and tracking any restart failures.
//
// Parameters:
//   - containers: List of containers to restart.
//   - client: Container client for Docker operations.
//   - params: Update options controlling restart behavior.
//   - stoppedImages: Set of image IDs for previously stopped containers.
//   - cleanupImageIDs: Map to collect image IDs for deferred cleanup.
//
// Returns:
//   - map[types.ContainerID]error: Map of container IDs to errors for failed restarts.
func restartContainersInSortedOrder(
	containers []types.Container,
	client container.Client,
	params types.UpdateParams,
	stoppedImages map[types.ImageID]bool,
	cleanupImageIDs map[types.ImageID]bool,
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

		// Restart Watchtower containers regardless of stoppedImages, as they are renamed.
		// Otherwise, restart only containers that were previously stopped.
		if stoppedImages[c.SafeImageID()] {
			renamed, err := restartStaleContainer(c, client, params)
			if err != nil {
				failed[c.ID()] = err
			} else {
				logrus.WithFields(fields).Debug("Restarted container")

				if renamed {
					renamedContainers[c.ID()] = true
				}
				// Only collect image IDs for stale containers that were not renamed, as renamed
				// containers (Watchtower self-updates) are cleaned up by CheckForMultipleWatchtowerInstances
				// in the new container.
				if c.IsStale() && !renamedContainers[c.ID()] {
					cleanupImageIDs[c.ImageID()] = true
				}
			}
		}
	}

	return failed
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
//   - bool: True if the container was renamed, false otherwise.
//   - error: Non-nil if restart fails, nil on success.
func restartStaleContainer(
	container types.Container,
	client container.Client,
	params types.UpdateParams,
) (bool, error) {
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	renamed := false
	// Rename Watchtower containers only if restarts are enabled.
	if container.IsWatchtower() && !params.NoRestart {
		newName := util.RandName()
		if err := client.RenameContainer(container, newName); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"container": container.Name(),
				"new_name":  newName,
			}).Debug("Failed to rename Watchtower container")

			return false, fmt.Errorf("%w: %w", errRenameWatchtowerFailed, err)
		}

		logrus.WithFields(fields).
			WithField("new_name", newName).
			Debug("Renamed Watchtower container")

		renamed = true
	}

	// Start the new container unless restarts are disabled.
	if !params.NoRestart {
		newContainerID, err := client.StartContainer(container)
		if err != nil {
			logrus.WithFields(fields).WithError(err).Debug("Failed to start container")

			return renamed, fmt.Errorf("%w: %w", errStartContainerFailed, err)
		}

		// Run post-update lifecycle hooks for restarting containers if enabled.
		if container.ToRestart() && params.LifecycleHooks {
			logrus.WithFields(fields).Debug("Executing post-update command")
			lifecycle.ExecutePostUpdateCommand(client, newContainerID)
		}
	}

	return renamed, nil
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
