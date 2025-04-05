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

// stopContainerTimeout defines the timeout duration for stopping containers.
// It ensures consistent timing in cleanup operations.
const stopContainerTimeout = 10 * time.Minute

// Errors for sanity and instance checks.
var (
	// errRollingRestartDependency indicates a container has dependencies incompatible with rolling restarts.
	errRollingRestartDependency = errors.New(
		"container has dependencies incompatible with rolling restarts",
	)
	// errStopWatchtowerFailed indicates a failure to stop excess Watchtower instances.
	errStopWatchtowerFailed = errors.New("errors occurred while stopping watchtower containers")
	// errListContainersFailed indicates a failure to list containers during checks.
	errListContainersFailed = errors.New("failed to list containers")
)

// CheckForSanity ensures the environment is suitable before starting updates.
// It verifies that rolling restarts are not used with dependent containers,
// returning an error if the configuration is invalid.
func CheckForSanity(client container.Client, filter types.Filter, rollingRestarts bool) error {
	logrus.Debug("Performing pre-update sanity checks")

	if !rollingRestarts {
		return nil // No further checks needed if rolling restarts are disabled.
	}

	containers, err := client.ListContainers(filter)
	if err != nil {
		logrus.WithError(err).Debug("Failed to list containers")

		return fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

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

// CheckForMultipleWatchtowerInstances ensures only one Watchtower instance runs at a time.
// It stops and optionally removes all but the most recently started Watchtower container,
// unless a scope UID is provided to bypass this check.
func CheckForMultipleWatchtowerInstances(
	client container.Client,
	cleanup bool,
	scope string,
) error {
	filter := filters.WatchtowerContainersFilter
	if scope != "" {
		filter = filters.FilterByScope(scope, filter)
		logrus.WithField("scope", scope).Debug("Applied scope filter for Watchtower instances")
	}

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

// cleanupExcessWatchtowers stops and optionally removes all but the latest Watchtower container.
// It sorts containers by creation time, processes all except the most recent, and reports any stop errors.
func cleanupExcessWatchtowers(
	containers []types.Container,
	client container.Client,
	cleanup bool,
) error {
	sort.Sort(sorter.ByCreated(containers))
	logrus.WithField("containers", containerNames(containers)).
		Debug("Sorted Watchtower instances by creation time")

	excessContainers := containers[:len(containers)-1] // All but the most recent
	logrus.WithField("containers", containerNames(excessContainers)).
		Info("Stopping excess Watchtower instances")

	var stopErrors []error

	for _, c := range excessContainers {
		if err := client.StopContainer(c, stopContainerTimeout); err != nil {
			logrus.WithError(err).
				WithField("container", c.Name()).
				Debug("Failed to stop Watchtower instance")

			stopErrors = append(stopErrors, err)

			continue
		}

		logrus.WithField("container", c.Name()).Info("Stopped Watchtower instance")

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

// containerNames extracts container names from a slice for logging purposes.
// It aids in providing readable log output without excessive verbosity.
func containerNames(containers []types.Container) []string {
	names := make([]string, len(containers))
	for i, c := range containers {
		names[i] = c.Name()
	}

	return names
}
