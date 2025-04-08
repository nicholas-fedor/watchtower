// Package actions provides core logic for Watchtower’s container update operations.
package actions

import (
	"errors"
	"fmt"
	"sort"
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
// Parameters:
//   - client: Container client.
//   - filter: Container filter.
//   - rollingRestarts: Enable rolling restarts if true.
//
// Returns:
//   - error: Non-nil if rolling restarts conflict with dependencies, nil otherwise.
func CheckForSanity(client container.Client, filter types.Filter, rollingRestarts bool) error {
	logrus.Debug("Performing pre-update sanity checks")

	if !rollingRestarts {
		return nil // No further checks needed if rolling restarts are disabled.
	}

	// List containers to check dependencies.
	containers, err := client.ListContainers(filter)
	if err != nil {
		logrus.WithError(err).Debug("Failed to list containers")

		return fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

	// Check for dependencies.
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

// CheckForMultipleWatchtowerInstances ensures a single Watchtower instance.
//
// Parameters:
//   - client: Container client.
//   - cleanup: Remove images if true.
//   - scope: Scope UID to filter instances.
//
// Returns:
//   - error: Non-nil if cleanup fails, nil if single instance or successful cleanup.
func CheckForMultipleWatchtowerInstances(
	client container.Client,
	cleanup bool,
	scope string,
) error {
	// Apply scope filter if provided.
	filter := filters.WatchtowerContainersFilter
	if scope != "" {
		filter = filters.FilterByScope(scope, filter)
		logrus.WithField("scope", scope).Debug("Applied scope filter for Watchtower instances")
	}

	// List Watchtower instances.
	containers, err := client.ListContainers(filter)
	if err != nil {
		logrus.WithError(err).Debug("Failed to list containers")

		return fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

	if len(containers) <= 1 {
		logrus.WithField("count", len(containers)).Debug("No additional Watchtower instances found")

		return nil
	}

	logrus.WithField("count", len(containers)).
		Info("Detected multiple Watchtower instances, initiating cleanup")

	return cleanupExcessWatchtowers(containers, client, cleanup)
}

// cleanupExcessWatchtowers removes all but the latest Watchtower instance.
//
// Parameters:
//   - containers: List of Watchtower instances.
//   - client: Container client.
//   - cleanup: Remove images if true.
//
// Returns:
//   - error: Non-nil if stopping fails, nil on success.
func cleanupExcessWatchtowers(
	containers []types.Container,
	client container.Client,
	cleanup bool,
) error {
	// Sort by creation time, keep newest.
	sort.Sort(sorter.ByCreated(containers))
	logrus.WithField("containers", containerNames(containers)).
		Debug("Sorted Watchtower instances by creation time")

	excessContainers := containers[:len(containers)-1] // All but the most recent
	logrus.WithField("containers", containerNames(excessContainers)).
		Info("Stopping excess Watchtower instances")

	var stopErrors []error

	for _, c := range excessContainers {
		// Stop excess container.
		if err := client.StopContainer(c, stopContainerTimeout); err != nil {
			logrus.WithError(err).
				WithField("container", c.Name()).
				Debug("Failed to stop Watchtower instance")

			stopErrors = append(stopErrors, err)

			continue
		}

		logrus.WithField("container", c.Name()).Info("Stopped Watchtower instance")

		// Remove image if cleanup enabled.
		if cleanup {
			if err := client.RemoveImageByID(c.ImageID()); err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"container": c.Name(),
					"image_id":  c.ImageID(),
				}).Warn("Failed to remove Watchtower image")
			} else {
				logrus.WithFields(logrus.Fields{
					"container": c.Name(),
					"image_id":  c.ImageID(),
				}).Debug("Removed Watchtower image")
			}
		}
	}

	// Report stop errors if any.
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

// containerNames extracts names from a container list.
//
// Parameters:
//   - containers: List of containers.
//
// Returns:
//   - []string: List of names.
func containerNames(containers []types.Container) []string {
	names := make([]string, len(containers))
	for i, c := range containers {
		names[i] = c.Name()
	}

	return names
}
