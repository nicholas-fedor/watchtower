// Package lifecycle manages the execution of lifecycle hooks for Watchtower containers.
// It provides functions to run pre-check, post-check, pre-update, and post-update commands
// as part of the container update process.
package lifecycle

import (
	"fmt"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/sirupsen/logrus"
)

// ExecutePreChecks runs pre-check lifecycle hooks for all containers matching the provided filter.
// It retrieves the list of containers using the client and executes the pre-check command for each.
// If listing containers fails, it logs the error and returns silently, ensuring no further action is taken.
func ExecutePreChecks(client container.Client, params types.UpdateParams) {
	containers, err := client.ListContainers(params.Filter)
	if err != nil {
		logrus.Errorf("Failed to list containers for pre-checks: %v", err)

		return
	}

	for _, currentContainer := range containers {
		ExecutePreCheckCommand(client, currentContainer)
	}
}

// ExecutePostChecks runs post-check lifecycle hooks for all containers matching the provided filter.
// It retrieves the list of containers using the client and executes the post-check command for each.
// If listing containers fails, it logs the error and returns silently, ensuring no further action is taken.
func ExecutePostChecks(client container.Client, params types.UpdateParams) {
	containers, err := client.ListContainers(params.Filter)
	if err != nil {
		logrus.Errorf("Failed to list containers for post-checks: %v", err)

		return
	}

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

	clog.Debug("Executing pre-check command")

	_, err := client.ExecuteCommand(container.ID(), command, 1)
	if err != nil {
		clog.Errorf("Pre-check command failed: %v", err)
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

	clog.Debug("Executing post-check command")

	_, err := client.ExecuteCommand(container.ID(), command, 1)
	if err != nil {
		clog.Errorf("Post-check command failed: %v", err)
	}
}

// ExecutePreUpdateCommand executes the pre-update lifecycle hook for a single container.
// It retrieves the pre-update command and timeout from the container’s configuration and runs the command.
// If no command is specified or the container isn’t running, it skips execution and returns false with no error.
// Returns true if the command was executed (successfully or not), and an error if execution fails.
func ExecutePreUpdateCommand(client container.Client, container types.Container) (bool, error) {
	timeout := container.PreUpdateTimeout()
	command := container.GetLifecyclePreUpdateCommand()
	clog := logrus.WithField("container", container.Name())

	if len(command) == 0 {
		clog.Debug("No pre-update command supplied. Skipping")

		return false, nil
	}

	if !container.IsRunning() || container.IsRestarting() {
		clog.Debug("Container is not running. Skipping pre-update command")

		return false, nil
	}

	clog.Debug("Executing pre-update command")

	success, err := client.ExecuteCommand(container.ID(), command, timeout)
	if err != nil {
		clog.Errorf("Pre-update command failed: %v", err)

		return true, fmt.Errorf("pre-update command execution failed for container %s: %w", container.Name(), err)
	}

	return success, nil
}

// ExecutePostUpdateCommand executes the post-update lifecycle hook for a single container.
// It retrieves the container by ID, gets the post-update command and timeout, and runs the command.
// If the container cannot be retrieved or no command is specified, it logs an error or debug message and skips execution.
// Errors from command execution are logged with context but do not affect the return, as this is a post-action.
func ExecutePostUpdateCommand(client container.Client, newContainerID types.ContainerID) {
	newContainer, err := client.GetContainer(newContainerID)
	if err != nil {
		logrus.WithField("containerID", newContainerID.ShortID()).Errorf("Failed to get container for post-update: %v", err)

		return
	}

	timeout := newContainer.PostUpdateTimeout()
	clog := logrus.WithField("container", newContainer.Name())
	command := newContainer.GetLifecyclePostUpdateCommand()

	if len(command) == 0 {
		clog.Debug("No post-update command supplied. Skipping")

		return
	}

	clog.Debug("Executing post-update command")

	_, err = client.ExecuteCommand(newContainerID, command, timeout)
	if err != nil {
		clog.Errorf("Post-update command failed for container %s (ID: %s): %v", newContainer.Name(), newContainerID.ShortID(), err)
	}
}
