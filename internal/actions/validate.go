package actions

import (
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// ValidateRollingRestartDependencies validates the environment for rolling restart updates.
//
// It iterates through the filtered containers and returns an error if any
// container has a linked dependency, which is incompatible with the use of
// a rolling restart update policy.
//
// Parameters:
//   - client: Container client for Docker operations.
//   - filter: Container filter to select relevant containers.
//
// Returns:
//   - error: Non-nil if dependencies conflict with rolling restarts, nil otherwise.
func ValidateRollingRestartDependencies(client container.Client, filter types.Filter) error {
	logrus.Debug("Performing pre-update rolling restart dependency validation")

	// Obtain the list of filtered containers.
	containers, err := client.ListContainers(filter)
	// Handle errors obtaining the list of containers.
	if err != nil {
		logrus.WithError(err).Debug("Failed to list containers")

		return fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

	// If there's no containers, then log and return nil.
	if len(containers) == 0 {
		logrus.Debug("No containers found")

		return nil
	}

	// Check each container for links.
	for _, c := range containers {
		// If a container has any links, then return an error.
		if links := c.Links(); len(links) > 0 {
			logrus.WithFields(logrus.Fields{
				"container": c.Name(),
				"links":     links,
			}).Debug("Found dependencies incompatible with rolling restarts")

			return fmt.Errorf("%w: %q depends on %v", errRollingRestartDependency, c.Name(), links)
		}
	}

	logrus.WithField("container_count", len(containers)).
		Debug("Rolling restart dependency validation passed - no dependencies found")

	return nil
}
