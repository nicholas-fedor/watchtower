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

// ExecutePreChecks runs pre-check lifecycle hooks for all containers matching the provided filter.
// It retrieves the list of containers using the client and executes the pre-check command for each.
// If listing containers fails, it logs the error and returns silently, ensuring no further action is taken.
func ExecutePreChecks(client container.Client, params types.UpdateParams) {
	clog := logrus.WithField(
		"filter",
		fmt.Sprintf("%v", params.Filter),
	) // Simplified filter logging
	clog.Debug("Listing containers for pre-checks")

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

// ExecutePostChecks runs post-check lifecycle hooks for all containers matching the provided filter.
// It retrieves the list of containers using the client and executes the post-check command for each.
// If listing containers fails, it logs the error and returns silently, ensuring no further action is taken.
func ExecutePostChecks(client container.Client, params types.UpdateParams) {
	clog := logrus.WithField("filter", fmt.Sprintf("%v", params.Filter))
	clog.Debug("Listing containers for post-checks")

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

// ExecutePreCheckCommand executes the pre-check lifecycle hook for a single container.
// It retrieves the pre-check command from the container’s configuration and runs it using the client.
// If no command is specified, it logs a debug message and skips execution.
// Errors from command execution are logged but do not halt the process.
func ExecutePreCheckCommand(client container.Client, container types.Container) {
	clog := logrus.WithField("container", container.Name())
	command := container.GetLifecyclePreCheckCommand()

	if len(command) == 0 {
		clog.Debug("No pre-check command supplied. Skipping")

		return
	}

	clog.WithField("command", command).Debug("Executing pre-check command")

	_, err := client.ExecuteCommand(container.ID(), command, 1)
	if err != nil {
		clog.WithError(err).Debug("Pre-check command failed")
	}
}

// ExecutePostCheckCommand executes the post-check lifecycle hook for a single container.
// It retrieves the post-check command from the container’s configuration and runs it using the client.
// If no command is specified, it logs a debug message and skips execution.
// Errors from command execution are logged but do not halt the process.
func ExecutePostCheckCommand(client container.Client, container types.Container) {
	clog := logrus.WithField("container", container.Name())
	command := container.GetLifecyclePostCheckCommand()

	if len(command) == 0 {
		clog.Debug("No post-check command supplied. Skipping")

		return
	}

	clog.WithField("command", command).Debug("Executing post-check command")

	_, err := client.ExecuteCommand(container.ID(), command, 1)
	if err != nil {
		clog.WithError(err).Debug("Post-check command failed")
	}
}

// ExecutePreUpdateCommand executes the pre-update lifecycle hook for a single container.
// It retrieves the pre-update command and timeout from the container’s configuration and runs the command.
// If no command is specified or the container isn’t running, it skips execution and returns false with no error.
// Returns true if the command was executed (successfully or not), and an error if execution fails.
func ExecutePreUpdateCommand(client container.Client, container types.Container) (bool, error) {
	timeout := container.PreUpdateTimeout()
	command := container.GetLifecyclePreUpdateCommand()
	clog := logrus.WithFields(logrus.Fields{
		"container": container.Name(),
		"timeout":   timeout,
	})

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

// ExecutePostUpdateCommand executes the post-update lifecycle hook for a single container.
// It retrieves the container by ID, gets the post-update command and timeout, and runs the command.
// If the container cannot be retrieved or no command is specified, it logs an error or debug message and skips execution.
// Errors from command execution are logged with context but do not affect the return, as this is a post-action.
func ExecutePostUpdateCommand(client container.Client, newContainerID types.ContainerID) {
	clog := logrus.WithField("container_id", newContainerID.ShortID())
	clog.Debug("Retrieving container for post-update")

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

	if len(command) == 0 {
		clog.Debug("No post-update command supplied. Skipping")

		return
	}

	clog.WithField("command", command).Debug("Executing post-update command")

	_, err = client.ExecuteCommand(newContainerID, command, timeout)
	if err != nil {
		clog.WithError(err).WithFields(logrus.Fields{
			"container_id": newContainerID.ShortID(),
		}).Debug("Post-update command failed")
	}
}
