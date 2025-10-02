// Package actions provides core logic for Watchtower’s container update operations.
package actions

import (
	"errors"
	"fmt"
	"sort"
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

// Errors for sanity and instance checks.
var (
	// errRollingRestartDependency flags incompatible dependencies for rolling restarts.
	errRollingRestartDependency = errors.New(
		"container has dependencies incompatible with rolling restarts",
	)
	// errStopWatchtowerFailed flags failures in stopping excess Watchtower instances.
	errStopWatchtowerFailed = errors.New("errors occurred while stopping watchtower containers")
	// errListContainersFailed flags failures in listing containers.
	errListContainersFailed = errors.New("failed to list containers")
)

// CheckForSanity validates the environment for updates.
//
// It checks for dependency conflicts when rolling restarts are enabled, ensuring containers with links
// do not cause unexpected behavior during sequential updates.
//
// Parameters:
//   - client: Container client for Docker operations.
//   - filter: Container filter to select relevant containers.
//   - rollingRestarts: Enable rolling restarts if true.
//
// Returns:
//   - error: Non-nil if rolling restarts conflict with dependencies, nil otherwise.
func CheckForSanity(client container.Client, filter types.Filter, rollingRestarts bool) error {
	logrus.Debug("Performing pre-update sanity checks")

	// Skip checks if rolling restarts are disabled, as dependencies are irrelevant.
	if !rollingRestarts {
		return nil
	}

	// List containers to inspect for dependency links.
	containers, err := client.ListContainers(filter)
	if err != nil {
		logrus.WithError(err).Debug("Failed to list containers")

		return fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

	// Check each container for links, which are incompatible with rolling restarts.
	for _, c := range containers {
		if links := c.Links(); len(links) > 0 {
			logrus.WithFields(logrus.Fields{
				"container": c.Name(),
				"links":     links,
			}).Debug("Found dependencies incompatible with rolling restarts")

			return fmt.Errorf("%w: %q depends on %v", errRollingRestartDependency, c.Name(), links)
		}
	}

	logrus.WithField("container_count", len(containers)).
		Debug("Sanity check passed, no dependencies found")

	return nil
}

// CheckForMultipleWatchtowerInstances ensures a single Watchtower instance within the same scope.
//
// It identifies multiple Watchtower containers within the same scope, stops all but the newest,
// and collects image IDs for deferred cleanup if enabled, preventing conflicts from concurrent instances.
// Scoped instances only clean up other instances in the same scope, allowing coexistence with different scopes.
// Cleanup operations respect scope boundaries to prevent cross-scope interference.
//
// Parameters:
//   - client: Container client for Docker operations.
//   - cleanup: Remove images if true.
//   - scope: Scope UID to filter Watchtower instances.
//   - cleanupImageIDs: Set of image IDs to clean up after stopping excess instances.
//
// Returns:
//   - error: Non-nil if cleanup fails, nil if single instance or successful cleanup.
func CheckForMultipleWatchtowerInstances(
	client container.Client,
	cleanup bool,
	scope string,
	cleanupImageIDs map[types.ImageID]bool,
) error {
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

		return fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

	// No action needed if one or fewer instances exist.
	if len(containers) <= 1 {
		logrus.WithField("count", len(containers)).Debug("No additional Watchtower instances found")

		return nil
	}

	logrus.WithField("count", len(containers)).
		Info("Detected multiple Watchtower instances, initiating cleanup")

	return cleanupExcessWatchtowers(containers, client, cleanup, cleanupImageIDs)
}

// cleanupExcessWatchtowers removes all but the latest Watchtower instance.
//
// It sorts containers by creation time, stops older instances, and collects their image IDs for
// deferred cleanup, ensuring only the newest instance remains active.
//
// Parameters:
//   - containers: List of Watchtower container instances.
//   - client: Container client for Docker operations.
//   - cleanup: Remove images if true.
//   - cleanupImageIDs: Set of image IDs to clean up after stopping excess instances.
//
// Returns:
//   - error: Non-nil if stopping fails, nil on success.
func cleanupExcessWatchtowers(
	containers []types.Container,
	client container.Client,
	cleanup bool,
	cleanupImageIDs map[types.ImageID]bool,
) error {
	// Sort containers by creation time to identify the newest instance.
	sort.Sort(sorter.ByCreated(containers))
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

	// Stop each excess container and collect image IDs for cleanup.
	for _, c := range excessContainers {
		if err := client.StopContainer(c, stopContainerTimeout); err != nil {
			logrus.WithError(err).
				WithField("container", c.Name()).
				Debug("Failed to stop Watchtower instance")

			stopErrors = append(stopErrors, err)

			continue
		}

		logrus.WithField("container", c.Name()).Debug("Stopped Watchtower instance")
		// Skip cleanup if the image is used by the newest container.
		if cleanup && c.SafeImageID() != newestImageID {
			cleanupImageIDs[c.SafeImageID()] = true
		}
	}

	// Perform deferred cleanup of collected image IDs if enabled.
	if cleanup {
		CleanupImages(client, cleanupImageIDs)
	}

	// Report any stop errors encountered during the process.
	if len(stopErrors) > 0 {
		logrus.WithField("error_count", len(stopErrors)).
			Debug("Encountered errors during Watchtower cleanup")

		return fmt.Errorf(
			"%w: %d instances failed to stop",
			errStopWatchtowerFailed,
			len(stopErrors),
		)
	}

	logrus.Info("Successfully cleaned up excess Watchtower instances")

	return nil
}

// CleanupImages removes specified image IDs.
//
// It iterates through the provided image IDs, attempting to remove each from the Docker environment,
// logging successes or failures for debugging and monitoring. If no image IDs are provided, it returns
// early to avoid unnecessary processing.
//
// Parameters:
//   - client: Container client for Docker operations.
//   - imageIDs: Set of image IDs to remove.
func CleanupImages(client container.Client, imageIDs map[types.ImageID]bool) {
	// Return early if no images need cleanup to optimize performance.
	if len(imageIDs) == 0 {
		logrus.Debug("No image IDs provided for cleanup, skipping")

		return
	}

	for imageID := range imageIDs {
		if imageID == "" {
			continue // Skip empty IDs to avoid invalid operations.
		}

		if err := client.RemoveImageByID(imageID); err != nil {
			// Check if this is a "No such image" error (expected when multiple instances clean up the same image)
			if strings.Contains(err.Error(), "No such image") {
				logrus.WithField("image_id", imageID).Debug("Image already removed")
			} else {
				logrus.WithError(err).WithField("image_id", imageID).Warn("Failed to remove image")
			}
		} else {
			logrus.WithField("image_id", imageID).Debug("Removed image")
		}
	}
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
