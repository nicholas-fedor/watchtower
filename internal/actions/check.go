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

// errRollingRestartDependency indicates a container has dependencies incompatible with rolling restarts.
// It is used in CheckForSanity to report configuration errors.
var errRollingRestartDependency = errors.New("container has dependencies incompatible with rolling restarts")

// errStopWatchtowerFailed indicates a failure to stop excess Watchtower instances.
// It is used in cleanupExcessWatchtowers to report aggregated stop errors.
var errStopWatchtowerFailed = errors.New("errors occurred while stopping watchtower containers")

// stopContainerTimeout defines the timeout duration for stopping containers.
// It ensures consistent timing in cleanup operations.
const stopContainerTimeout = 10 * time.Minute

// CheckForSanity ensures the environment is suitable before starting updates.
// It verifies that rolling restarts are not used with dependent containers,
// returning an error if the configuration is invalid.
func CheckForSanity(client container.Client, filter types.Filter, rollingRestarts bool) error {
	logrus.Debug("Making sure everything is sane before starting")

	if rollingRestarts {
		containers, err := client.ListContainers(filter)
		if err != nil {
			return fmt.Errorf("%w: %w", errListContainersFailed, err)
		}

		for _, c := range containers {
			if len(c.Links()) > 0 {
				return fmt.Errorf("%w: %q depends on at least one other container", errRollingRestartDependency, c.Name())
			}
		}
	}

	return nil
}

// CheckForMultipleWatchtowerInstances ensures only one Watchtower instance runs at a time.
// It stops and optionally removes all but the most recently started Watchtower container,
// unless a scope UID is provided to bypass this check.
func CheckForMultipleWatchtowerInstances(client container.Client, cleanup bool, scope string) error {
	filter := filters.WatchtowerContainersFilter
	if scope != "" {
		filter = filters.FilterByScope(scope, filter)
	}

	containers, err := client.ListContainers(filter)
	if err != nil {
		return fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

	if len(containers) <= 1 {
		logrus.Debug("There are no additional watchtower containers")

		return nil
	}

	logrus.Info("Found multiple running watchtower instances. Cleaning up.")

	return cleanupExcessWatchtowers(containers, client, cleanup)
}

// cleanupExcessWatchtowers stops and optionally removes all but the latest Watchtower container.
// It sorts containers by creation time, processes all except the most recent, and reports any stop errors.
func cleanupExcessWatchtowers(containers []types.Container, client container.Client, cleanup bool) error {
	var stopErrors int

	sort.Sort(sorter.ByCreated(containers))
	logrus.Debugf("Sorted containers: %v", containers) // Log sorted order
	allContainersExceptLast := containers[0 : len(containers)-1]
	logrus.Debugf("Stopping containers: %v", allContainersExceptLast) // Log what’s being stopped

	for _, c := range allContainersExceptLast {
		if err := client.StopContainer(c, stopContainerTimeout); err != nil {
			logrus.WithError(err).Error("Could not stop a previous watchtower instance.")

			stopErrors++

			continue
		}

		if cleanup {
			if err := client.RemoveImageByID(c.ImageID()); err != nil {
				logrus.WithError(err).Warning("Could not cleanup watchtower images, possibly because of other watchtowers instances in other scopes.")
			}
		}
	}

	if stopErrors > 0 {
		return fmt.Errorf("%w: %d instances failed to stop", errStopWatchtowerFailed, stopErrors)
	}

	return nil
}
