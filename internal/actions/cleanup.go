package actions

import (
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/sorter"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// stopContainerTimeout sets the container stop timeout.
const stopContainerTimeout = 10 * time.Minute

// cleanupRetryDelay sets the delay before retrying cleanup operations.
const cleanupRetryDelay = 500 * time.Millisecond

// CheckForMultipleWatchtowerInstances ensures a single Watchtower instance within the same scope.
//
// It identifies multiple Watchtower containers within the same scope, stops all but the newest,
// and collects cleaned images for deferred cleanup if enabled, preventing conflicts from concurrent instances.
// Scoped instances only clean up other instances in the same scope, allowing coexistence with different scopes.
// Cleanup operations respect scope boundaries to prevent cross-scope interference.
//
// Parameters:
//   - client: Container client for Docker operations.
//   - cleanup: Remove images if true.
//   - scope: Scope UID to filter Watchtower instances.
//   - cleanupImageInfos: Pointer to slice of cleaned images to clean up after stopping excess instances.
//
// Returns:
//   - bool: True if cleanup occurred (multiple instances were found and excess ones stopped), false otherwise.
//   - error: Non-nil if cleanup fails, nil if single instance or successful cleanup.
func CheckForMultipleWatchtowerInstances(
	client container.Client,
	cleanup bool,
	scope string,
	cleanupImageInfos *[]types.CleanedImageInfo,
) (bool, error) {
	// Apply scope filter to target specific Watchtower instances, if provided.
	var filter types.Filter

	switch {
	case scope != "": // Scoped instance - filter by scope
		filter = filters.FilterByScope(scope, filters.WatchtowerContainersFilter)
		logrus.WithField("scope", scope).Debug("Applied scope filter for Watchtower instances")
	case scope == "": // Unscoped instance - only unscoped instances
		filter = filters.UnscopedWatchtowerContainersFilter

		logrus.Debug("Applied unscoped filter for Watchtower instances")
	}

	// List all Watchtower containers matching the filter.
	containers, err := client.ListContainers(filter)
	if err != nil {
		logrus.WithError(err).Debug("Failed to list containers")

		return false, fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

	// No action needed if one or fewer instances exist.
	if len(containers) <= 1 {
		logrus.WithField("count", len(containers)).Debug("No additional Watchtower instances found")

		return false, nil
	}

	logrus.WithField("count", len(containers)).
		Info("Detected multiple Watchtower instances - initiating cleanup")

	return cleanupExcessWatchtowers(containers, client, cleanup, cleanupImageInfos)
}

// cleanupExcessWatchtowers removes all but the latest Watchtower instance.
//
// It sorts containers by creation time, stops and removes older instances, and collects cleaned images for
// deferred cleanup, ensuring only the newest instance remains active.
//
// Parameters:
//   - containers: List of Watchtower container instances.
//   - client: Container client for Docker operations.
//   - cleanup: Remove images if true.
//   - cleanupImageInfos: Pointer to slice of cleaned images to clean up after stopping excess instances.
//
// Returns:
//   - bool: Always true since cleanup occurred.
//   - error: Non-nil if stopping fails, nil on success.
func cleanupExcessWatchtowers(
	containers []types.Container,
	client container.Client,
	cleanup bool,
	cleanupImageInfos *[]types.CleanedImageInfo,
) (bool, error) {
	// Sort containers by creation time to identify the newest instance.
	err := sorter.SortByCreated(containers)
	if err != nil {
		logrus.WithError(err).Debug("Failed to sort containers by creation time")

		return false, fmt.Errorf("%w: %w", errStopWatchtowerFailed, err)
	}

	logrus.WithField("containers", containerNames(containers)).
		Debug("Sorted Watchtower instances by creation time")

	// Select all but the most recent container for stopping.
	excessContainers := containers[:len(containers)-1]
	logrus.WithField("excess_containers", containerNames(excessContainers)).
		Debug("Stopping excess Watchtower instances")

	var stopErrors []error

	// Get the newest container’s image ID (kept running).
	newestContainer := containers[len(containers)-1]
	newestImageID := newestContainer.SafeImageID()
	logrus.WithFields(logrus.Fields{
		"newest_container": newestContainer.Name(),
		"newest_image_id":  newestImageID,
	}).Debug("Identified newest container")

	// Stop and remove excess containers.
	for _, c := range excessContainers {
		logrus.WithField("container", c.Name()).
			Debug("Attempting to stop and remove excess Watchtower container")

		// Stop and remove the container with timeout.
		err := client.StopAndRemoveContainer(c, stopContainerTimeout)
		if err != nil {
			// Check if this is a "removal already in progress" error
			// This happens when multiple processes attempt to remove the same container simultaneously
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "already in progress") ||
				strings.Contains(errStr, "removal of container") &&
					strings.Contains(errStr, "is already in progress") {
				logrus.WithField("container", c.Name()).
					Debug("Container removal already in progress by another process, skipping")
				// Don't treat this as an error - another process is handling cleanup
				continue
			}

			// Check for "no such container" errors (container already removed)
			if strings.Contains(errStr, "no such container") ||
				strings.Contains(errStr, "not found") {
				logrus.WithField("container", c.Name()).
					Debug("Container already removed, skipping")
				// Don't treat this as an error - container is already gone
				continue
			}

			// Add a small delay before reporting to allow for transient issues
			// This helps with race conditions and I/O stress scenarios
			time.Sleep(cleanupRetryDelay)

			logrus.WithError(err).
				WithField("container", c.Name()).
				Debug("Failed to stop Watchtower instance")

			// Collect stop errors for reporting.
			stopErrors = append(stopErrors, err)

			continue
		}

		logrus.WithField("container", c.Name()).
			Debug("Successfully stopped and removed excess Watchtower instance")

		// Skip cleanup if the image is used by the newest container.
		if cleanup && c.SafeImageID() != newestImageID {
			*cleanupImageInfos = append(
				*cleanupImageInfos,
				types.CleanedImageInfo{
					ImageID:       c.SafeImageID(),
					ContainerID:   c.ID(),
					ImageName:     c.ImageName(),
					ContainerName: c.Name(),
				},
			)
		}
	}

	// Perform deferred cleanup of collected cleaned images if enabled.
	if cleanup {
		cleaned, err := CleanupImages(client, *cleanupImageInfos)
		if err != nil {
			logrus.WithError(err).Warn("Failed to clean up some images during Watchtower cleanup")
		} else if len(cleaned) > 0 {
			logrus.WithField("cleaned_images", len(cleaned)).
				Debug("Successfully cleaned up images during Watchtower cleanup")
		}
	}

	// Report any stop errors encountered during the process.
	if len(stopErrors) > 0 {
		// Log detailed error information for debugging
		for i, err := range stopErrors {
			logrus.WithError(err).
				WithField("container_index", i).
				WithField("total_errors", len(stopErrors)).
				Debug("Watchtower cleanup error details")
		}

		// Check if we successfully cleaned up any containers
		successCount := len(excessContainers) - len(stopErrors)
		if successCount > 0 {
			logrus.WithFields(logrus.Fields{
				"successful_cleanups": successCount,
				"failed_cleanups":     len(stopErrors),
				"total_containers":    len(excessContainers),
			}).Warn("Partially successful Watchtower cleanup - some containers may remain orphaned")

			// Return success with warning rather than complete failure
			// This allows the new Watchtower instance to continue operating
			return true, nil
		}

		logrus.WithField("error_count", len(stopErrors)).
			Error("All Watchtower cleanup operations failed")

		return true, fmt.Errorf(
			"%w: all %d instances failed to stop",
			errStopWatchtowerFailed,
			len(stopErrors),
		)
	}

	logrus.WithField("cleaned_containers", len(excessContainers)).
		Info("Successfully cleaned up all excess Watchtower instances")

	return true, nil
}

// CleanupImages removes specified cleaned images and returns successfully cleaned ones.
//
// It iterates through the provided cleaned images, attempting to remove each from the Docker environment,
// logging successes or failures for debugging and monitoring. Tracks successfully cleaned image info.
// If no cleaned images are provided, it returns an empty slice and no error.
//
// Parameters:
//   - client: Container client for Docker operations.
//   - cleanedImages: Slice of cleaned images to remove.
//
// Returns:
//   - []CleanedImageInfo: Slice of successfully cleaned image info.
//   - error: Non-nil if any image removal failed, nil otherwise.
func CleanupImages(
	client container.Client,
	cleanedImages []types.CleanedImageInfo,
) ([]types.CleanedImageInfo, error) {
	// Return early if no images need cleanup to optimize performance.
	if len(cleanedImages) == 0 {
		logrus.Debug("No cleaned images provided for cleanup, skipping")

		return []types.CleanedImageInfo{}, nil
	}

	cleaned := []types.CleanedImageInfo{}

	var removalErrors []error

	for _, cleanedImage := range cleanedImages {
		imageID := cleanedImage.ImageID
		if imageID == "" {
			continue // Skip empty IDs to avoid invalid operations.
		}

		err := client.RemoveImageByID(imageID, cleanedImage.ImageName)
		if err != nil {
			// Check if this is a "No such image" error (expected when multiple instances clean up the same image)
			if strings.Contains(err.Error(), "No such image") {
				logrus.WithFields(logrus.Fields{
					"image_id":   imageID,
					"image_name": cleanedImage.ImageName,
				}).Debug("Image already removed")
			} else {
				logrus.WithError(err).WithFields(logrus.Fields{
					"image_id":   imageID,
					"image_name": cleanedImage.ImageName,
				}).Debug("Failed to remove image")
				removalErrors = append(
					removalErrors,
					fmt.Errorf("failed to remove image %s: %w", imageID, err),
				)
			}
		} else {
			logrus.WithFields(logrus.Fields{
				"image_id":   imageID.ShortID(),
				"image_name": cleanedImage.ImageName,
			}).Debug("Cleaned up old image")
			cleaned = append(
				cleaned,
				types.CleanedImageInfo{
					ImageID:       imageID,
					ContainerID:   cleanedImage.ContainerID,
					ImageName:     cleanedImage.ImageName,
					ContainerName: cleanedImage.ContainerName,
				},
			)
		}
	}

	if len(removalErrors) > 0 {
		return cleaned, fmt.Errorf(
			"%w: %d of %d image removals failed",
			errImageCleanupFailed,
			len(removalErrors),
			len(cleanedImages),
		)
	}

	return cleaned, nil
}

// containerNames extracts names from a container list.
//
// It creates a slice of container names for logging or debugging purposes, preserving order.
//
// Parameters:
//   - containers: List of containers.
//
// Returns:
//   - []string: List of container names.
func containerNames(containers []types.Container) []string {
	names := make([]string, len(containers))
	for i, c := range containers {
		names[i] = c.Name()
	}

	return names
}
