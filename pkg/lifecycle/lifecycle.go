// Package lifecycle manages the execution of lifecycle hooks for Watchtower containers.
// It provides functions to run pre-check, post-check, pre-update, and post-update commands
// as part of the container update process.
package lifecycle

import (
	"errors"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for lifecycle hook execution.
var (
	// errPreUpdateFailed indicates a failure in executing the pre-update command.
	errPreUpdateFailed = errors.New("pre-update command execution failed")
)

// ExecutePreChecks runs pre-check lifecycle hooks for filtered containers.
//
// Parameters:
//   - client: Container client for execution.
//   - params: Update parameters with filter.
func ExecutePreChecks(client container.Client, params types.UpdateParams) {
	clog := logrus.WithField(
		"filter",
		fmt.Sprintf("%v", params.Filter),
	) // Simplified filter logging
	clog.Debug("Listing containers for pre-checks")

	// Fetch containers using the provided filter.
	containers, err := client.ListContainers(params.Filter)
	if err != nil {
		clog.WithError(err).Debug("Failed to list containers for pre-checks")

		return
	}

	clog.WithField("count", len(containers)).Debug("Found containers for pre-checks")

	for _, currentContainer := range containers {
		ExecutePreCheckCommand(client, currentContainer)
	}
}

// ExecutePostChecks runs post-check lifecycle hooks for filtered containers.
//
// Parameters:
//   - client: Container client for execution.
//   - params: Update parameters with filter.
func ExecutePostChecks(client container.Client, params types.UpdateParams) {
	clog := logrus.WithField("filter", fmt.Sprintf("%v", params.Filter))
	clog.Debug("Listing containers for post-checks")

	// Fetch containers using the provided filter.
	containers, err := client.ListContainers(params.Filter)
	if err != nil {
		clog.WithError(err).Debug("Failed to list containers for post-checks")

		return
	}

	clog.WithField("count", len(containers)).Debug("Found containers for post-checks")

	for _, currentContainer := range containers {
		ExecutePostCheckCommand(client, currentContainer)
	}
}

// ExecutePreCheckCommand executes the pre-check hook for a container.
//
// Parameters:
//   - client: Container client for execution.
//   - container: Container to process.
func ExecutePreCheckCommand(client container.Client, container types.Container) {
	clog := logrus.WithField("container", container.Name())
	command := container.GetLifecyclePreCheckCommand()

	// Skip if no command is set.
	if len(command) == 0 {
		clog.Debug("No pre-check command supplied. Skipping")

		return
	}

	// Execute command with default timeout.
	clog.WithField("command", command).Debug("Executing pre-check command")

	_, err := client.ExecuteCommand(container.ID(), command, 1)
	if err != nil {
		clog.WithError(err).Debug("Pre-check command failed")
	}
}

// ExecutePostCheckCommand executes the post-check hook for a container.
//
// Parameters:
//   - client: Container client for execution.
//   - container: Container to process.
func ExecutePostCheckCommand(client container.Client, container types.Container) {
	clog := logrus.WithField("container", container.Name())
	command := container.GetLifecyclePostCheckCommand()

	// Skip if no command is set.
	if len(command) == 0 {
		clog.Debug("No post-check command supplied. Skipping")

		return
	}

	// Execute command with default timeout.
	clog.WithField("command", command).Debug("Executing post-check command")

	_, err := client.ExecuteCommand(container.ID(), command, 1)
	if err != nil {
		clog.WithError(err).Debug("Post-check command failed")
	}
}

// ExecutePreUpdateCommand executes the pre-update hook for a container.
//
// Parameters:
//   - client: Container client for execution.
//   - container: Container to process.
//
// Returns:
//   - bool: True if command ran, false if skipped.
//   - error: Non-nil if execution fails, nil otherwise.
func ExecutePreUpdateCommand(client container.Client, container types.Container) (bool, error) {
	timeout := container.PreUpdateTimeout()
	command := container.GetLifecyclePreUpdateCommand()
	clog := logrus.WithFields(logrus.Fields{
		"container": container.Name(),
		"timeout":   timeout,
	})

	// Skip if no command or container isn’t running.
	if len(command) == 0 {
		clog.Debug("No pre-update command supplied. Skipping")

		return false, nil
	}

	if !container.IsRunning() || container.IsRestarting() {
		clog.WithFields(logrus.Fields{
			"is_running":    container.IsRunning(),
			"is_restarting": container.IsRestarting(),
		}).Debug("Container is not running. Skipping pre-update command")

		return false, nil
	}

	// Execute command with configured timeout.
	clog.WithField("command", command).Debug("Executing pre-update command")

	success, err := client.ExecuteCommand(container.ID(), command, timeout)
	if err != nil {
		clog.WithError(err).Debug("Pre-update command failed")

		return true, fmt.Errorf(
			"%w for container %s: %w",
			errPreUpdateFailed,
			container.Name(),
			err,
		)
	}

	clog.WithField("success", success).Debug("Pre-update command executed")

	return success, nil
}

// ExecutePostUpdateCommand executes the post-update hook for a container.
//
// Parameters:
//   - client: Container client for execution.
//   - newContainerID: ID of the updated container.
func ExecutePostUpdateCommand(client container.Client, newContainerID types.ContainerID) {
	clog := logrus.WithField("container_id", newContainerID.ShortID())
	clog.Debug("Retrieving container for post-update")

	// Retrieve container by ID.
	newContainer, err := client.GetContainer(newContainerID)
	if err != nil {
		clog.WithError(err).Debug("Failed to get container for post-update")

		return
	}

	timeout := newContainer.PostUpdateTimeout()
	clog = logrus.WithFields(logrus.Fields{
		"container": newContainer.Name(),
		"timeout":   timeout,
	})
	command := newContainer.GetLifecyclePostUpdateCommand()

	// Skip if no command is set.
	if len(command) == 0 {
		clog.Debug("No post-update command supplied. Skipping")

		return
	}

	// Execute command with configured timeout.
	clog.WithField("command", command).Debug("Executing post-update command")

	_, err = client.ExecuteCommand(newContainerID, command, timeout)
	if err != nil {
		clog.WithError(err).WithFields(logrus.Fields{
			"container_id": newContainerID.ShortID(),
		}).Debug("Post-update command failed")
	}
}
