package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// checkCommandTimeout is the timeout in minutes for check commands (pre/post check hooks).
const checkCommandTimeout = 1

// Errors for lifecycle hook execution.
var (
	// errPreUpdateFailed indicates a failure in executing the pre-update command.
	errPreUpdateFailed = errors.New("pre-update command execution failed")
	// errHostCommandFailed indicates a host lifecycle command exited non-zero.
	errHostCommandFailed = errors.New("host command execution failed")
)

// exTempFail is the exit code (EX_TEMPFAIL) a host pre-update command can return to
// signal that this container's update should be skipped without aborting the cycle.
const exTempFail = 75

// ExecutePreChecks runs pre-check lifecycle hooks for filtered containers.
//
// Parameters:
//   - ctx: Context for cancellation and timeout.
//   - client: Container client for execution.
//   - params: Update parameters with filter.
func ExecutePreChecks(ctx context.Context, client container.Client, params types.UpdateParams) {
	uid := params.LifecycleUID
	gid := params.LifecycleGID
	clog := logrus.WithField(
		"filter",
		fmt.Sprintf("%v", params.Filter),
	) // Simplified filter logging
	clog.Debug("Listing containers for pre-checks")

	// Fetch containers using the provided filter.
	containers, err := client.ListContainers(ctx, params.Filter)
	if err != nil {
		clog.WithError(err).Debug("Failed to list containers for pre-checks")

		return
	}

	clog.WithField("count", len(containers)).Debug("Found containers for pre-checks")

	for _, currentContainer := range containers {
		ExecuteHostPreCheckCommand(ctx, currentContainer)
		ExecutePreCheckCommand(ctx, client, currentContainer, uid, gid)
	}
}

// ExecutePostChecks runs post-check lifecycle hooks for filtered containers.
//
// Parameters:
//   - ctx: Context for cancellation and timeout.
//   - client: Container client for execution.
//   - params: Update parameters with filter.
func ExecutePostChecks(ctx context.Context, client container.Client, params types.UpdateParams) {
	uid := params.LifecycleUID
	gid := params.LifecycleGID
	clog := logrus.WithField("filter", fmt.Sprintf("%v", params.Filter))
	clog.Debug("Listing containers for post-checks")

	// Fetch containers using the provided filter.
	containers, err := client.ListContainers(ctx, params.Filter)
	if err != nil {
		clog.WithError(err).Debug("Failed to list containers for post-checks")

		return
	}

	clog.WithField("count", len(containers)).Debug("Found containers for post-checks")

	for _, currentContainer := range containers {
		ExecuteHostPostCheckCommand(ctx, currentContainer)
		ExecutePostCheckCommand(ctx, client, currentContainer, uid, gid)
	}
}

// ExecutePreCheckCommand executes the pre-check hook for a container.
//
// Parameters:
//   - ctx: Context for cancellation and timeout.
//   - client: Container client for execution.
//   - container: Container to process.
//   - uid: Default UID to run command as.
//   - gid: Default GID to run command as.
func ExecutePreCheckCommand(ctx context.Context, client container.Client, container types.Container, uid, gid int) {
	clog := logrus.WithField("container", container.Name())
	command := container.GetLifecyclePreCheckCommand()

	// Determine effective UID/GID: use container labels if set, otherwise use defaults.
	effectiveUID := uid
	if containerUID, ok := container.GetLifecycleUID(); ok {
		effectiveUID = containerUID
	}

	effectiveGID := gid
	if containerGID, ok := container.GetLifecycleGID(); ok {
		effectiveGID = containerGID
	}

	// Skip if no command is set.
	if len(command) == 0 {
		clog.Debug("No pre-check command supplied. Skipping")

		return
	}

	// Execute command with fixed short timeout (1 minute).
	// Check commands are lightweight health checks that should complete quickly,
	// unlike update commands which may perform complex operations and use configurable timeouts.
	clog.WithField("command", command).Debug("Executing pre-check command")

	_, err := client.ExecuteCommand(ctx, container, command, checkCommandTimeout, effectiveUID, effectiveGID)
	if err != nil {
		clog.WithError(err).Debug("Pre-check command failed")
	}
}

// ExecuteHostPreCheckCommand executes the host pre-check hook for a container.
//
// Unlike ExecutePreCheckCommand, the command runs on the Watchtower host (the
// process running Watchtower) instead of inside the container. The triggering
// container's name, ID, and image are exposed to the command via environment
// variables (WT_CONTAINER_NAME, WT_CONTAINER_ID, WT_CONTAINER_IMAGE_NAME).
//
// Parameters:
//   - ctx: Context for cancellation and timeout.
//   - container: Container to process.
func ExecuteHostPreCheckCommand(ctx context.Context, container types.Container) {
	clog := logrus.WithField("container", container.Name())
	command := container.GetLifecycleHostPreCheckCommand()

	// Skip if no command is set.
	if len(command) == 0 {
		clog.Debug("No host pre-check command supplied. Skipping")

		return
	}

	// Host check commands are lightweight and use the same fixed short timeout
	// as in-container check commands (1 minute).
	clog.WithField("command", command).Debug("Executing host pre-check command")

	if _, err := executeHostCommand(ctx, command, checkCommandTimeout, hostCommandEnv(container)); err != nil {
		clog.WithError(err).Debug("Host pre-check command failed")
	}
}

// hostCommandEnv builds the environment variables exposed to host lifecycle commands.
//
// These mirror the fields of the in-container WT_CONTAINER metadata that are
// meaningful on the host, exposed as discrete variables.
//
// Parameters:
//   - container: Container the command is being run for.
//
// Returns:
//   - []string: Environment variables in "KEY=value" form.
func hostCommandEnv(container types.Container) []string {
	return []string{
		"WT_CONTAINER_NAME=" + container.Name(),
		"WT_CONTAINER_ID=" + string(container.ID()),
		"WT_CONTAINER_IMAGE_NAME=" + container.ImageName(),
	}
}

// executeHostCommand runs a shell command on the Watchtower host.
//
// Parameters:
//   - ctx: Context for cancellation.
//   - command: Command to execute via "sh -c".
//   - timeout: Minutes to wait before timeout (0 for no timeout).
//   - env: Additional environment variables (appended to the host environment).
//
// Returns:
//   - bool: True if the command signalled a skip via exit code 75 (EX_TEMPFAIL).
//   - error: Non-nil if the command exits non-zero (other than 75), fails to start,
//     or times out; nil on success.
func executeHostCommand(ctx context.Context, command string, timeout int, env []string) (bool, error) {
	// Apply a timeout when configured, otherwise inherit the parent context.
	if timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Minute)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = append(cmd.Environ(), env...)

	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		logrus.WithField("output", string(output)).Debug("Host command output")
	}

	if err == nil {
		return false, nil
	}

	// Inspect the exit code when the command ran but exited non-zero.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() == exTempFail {
			return true, nil // Skip update on temporary failure.
		}

		return false, fmt.Errorf(
			"%w with exit code %d: %s",
			errHostCommandFailed,
			exitErr.ExitCode(),
			string(output),
		)
	}

	// Command failed to start or the context was cancelled/timed out.
	return false, fmt.Errorf("%w: %w", errHostCommandFailed, err)
}

// ExecutePostCheckCommand executes the post-check hook for a container.
//
// Parameters:
//   - ctx: Context for cancellation and timeout.
//   - client: Container client for execution.
//   - container: Container to process.
//   - uid: Default UID to run command as.
//   - gid: Default GID to run command as.
func ExecutePostCheckCommand(ctx context.Context, client container.Client, container types.Container, uid, gid int) {
	clog := logrus.WithField("container", container.Name())
	command := container.GetLifecyclePostCheckCommand()

	// Determine effective UID/GID: use container labels if set, otherwise use defaults.
	effectiveUID := uid
	if containerUID, ok := container.GetLifecycleUID(); ok {
		effectiveUID = containerUID
	}

	effectiveGID := gid
	if containerGID, ok := container.GetLifecycleGID(); ok {
		effectiveGID = containerGID
	}

	// Skip if no command is set.
	if len(command) == 0 {
		clog.Debug("No post-check command supplied. Skipping")

		return
	}

	// Execute command with fixed short timeout (1 minute).
	// Check commands are lightweight health checks that should complete quickly,
	// unlike update commands which may perform complex operations and use configurable timeouts.
	clog.WithField("command", command).Debug("Executing post-check command")

	_, err := client.ExecuteCommand(ctx, container, command, checkCommandTimeout, effectiveUID, effectiveGID)
	if err != nil {
		clog.WithError(err).Debug("Post-check command failed")
	}
}

// ExecuteHostPostCheckCommand executes the host post-check hook for a container.
//
// Unlike ExecutePostCheckCommand, the command runs on the Watchtower host (the
// process running Watchtower) instead of inside the container. The triggering
// container's name, ID, and image are exposed to the command via environment
// variables (WT_CONTAINER_NAME, WT_CONTAINER_ID, WT_CONTAINER_IMAGE_NAME).
//
// Parameters:
//   - ctx: Context for cancellation and timeout.
//   - container: Container to process.
func ExecuteHostPostCheckCommand(ctx context.Context, container types.Container) {
	clog := logrus.WithField("container", container.Name())
	command := container.GetLifecycleHostPostCheckCommand()

	// Skip if no command is set.
	if len(command) == 0 {
		clog.Debug("No host post-check command supplied. Skipping")

		return
	}

	// Host check commands are lightweight and use the same fixed short timeout
	// as in-container check commands (1 minute).
	clog.WithField("command", command).Debug("Executing host post-check command")

	if _, err := executeHostCommand(ctx, command, checkCommandTimeout, hostCommandEnv(container)); err != nil {
		clog.WithError(err).Debug("Host post-check command failed")
	}
}

// ExecutePreUpdateCommand executes the pre-update hook for a container.
//
// Parameters:
//   - ctx: Context for cancellation and timeout.
//   - client: Container client for execution.
//   - container: Container to process.
//   - uid: UID to run command as.
//   - gid: GID to run command as.
//
// Returns:
//   - bool: True if command ran, false if skipped.
//   - error: Non-nil if execution fails, nil otherwise.
func ExecutePreUpdateCommand(
	ctx context.Context,
	client container.Client,
	container types.Container,
	uid int,
	gid int,
) (bool, error) {
	timeout := container.PreUpdateTimeout()
	command := container.GetLifecyclePreUpdateCommand()
	clog := logrus.WithFields(logrus.Fields{
		"container": container.Name(),
		"timeout":   timeout,
	})

	// Skip if no command or container isn't running.
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

	// Determine effective UID/GID: use container labels if set, otherwise use defaults.
	effectiveUID := uid
	if containerUID, ok := container.GetLifecycleUID(); ok {
		effectiveUID = containerUID
	}

	effectiveGID := gid
	if containerGID, ok := container.GetLifecycleGID(); ok {
		effectiveGID = containerGID
	}

	// Execute command with configured timeout.
	clog.WithField("command", command).Debug("Executing pre-update command")

	success, err := client.ExecuteCommand(ctx, container, command, timeout, effectiveUID, effectiveGID)
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

// ExecuteHostPreUpdateCommand executes the host pre-update hook for a container.
//
// Unlike ExecutePreUpdateCommand, the command runs on the Watchtower host (the
// process running Watchtower) instead of inside the container, so it executes
// regardless of whether the container is currently running. It honours the same
// exit-code semantics as the in-container pre-update hook: exit code 75
// (EX_TEMPFAIL) requests skipping this container's update, while any other
// non-zero exit aborts the update. The triggering container's name, ID, and
// image are exposed via WT_CONTAINER_NAME, WT_CONTAINER_ID, and
// WT_CONTAINER_IMAGE_NAME environment variables.
//
// Parameters:
//   - ctx: Context for cancellation and timeout.
//   - container: Container to process.
//
// Returns:
//   - bool: True if the update should be skipped (exit code 75).
//   - error: Non-nil if execution fails, nil otherwise.
func ExecuteHostPreUpdateCommand(ctx context.Context, container types.Container) (bool, error) {
	timeout := container.PreUpdateTimeout()
	command := container.GetLifecycleHostPreUpdateCommand()
	clog := logrus.WithFields(logrus.Fields{
		"container": container.Name(),
		"timeout":   timeout,
	})

	// Skip if no command is set.
	if len(command) == 0 {
		clog.Debug("No host pre-update command supplied. Skipping")

		return false, nil
	}

	// Execute command with configured timeout.
	clog.WithField("command", command).Debug("Executing host pre-update command")

	skipUpdate, err := executeHostCommand(ctx, command, timeout, hostCommandEnv(container))
	if err != nil {
		clog.WithError(err).Debug("Host pre-update command failed")

		return false, fmt.Errorf(
			"%w for container %s: %w",
			errPreUpdateFailed,
			container.Name(),
			err,
		)
	}

	clog.WithField("skip_update", skipUpdate).Debug("Host pre-update command executed")

	return skipUpdate, nil
}

// ExecutePostUpdateCommand executes the post-update hook for a container.
//
// Parameters:
//   - ctx: Context for cancellation and timeout.
//   - client: Container client for execution.
//   - newContainerID: ID of the updated container.
//   - uid: UID to run command as.
//   - gid: GID to run command as.
func ExecutePostUpdateCommand(
	ctx context.Context,
	client container.Client,
	newContainerID types.ContainerID,
	uid int,
	gid int,
) {
	clog := logrus.WithField("container_id", newContainerID.ShortID())
	clog.Debug("Retrieving container for post-update")

	// Retrieve container by ID.
	newContainer, err := client.GetContainer(ctx, newContainerID)
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

	// Determine effective UID/GID: use container labels if set, otherwise use defaults.
	effectiveUID := uid
	if containerUID, ok := newContainer.GetLifecycleUID(); ok {
		effectiveUID = containerUID
	}

	effectiveGID := gid
	if containerGID, ok := newContainer.GetLifecycleGID(); ok {
		effectiveGID = containerGID
	}

	// Skip if no command is set.
	if len(command) == 0 {
		clog.Debug("No post-update command supplied. Skipping")

		return
	}

	// Execute command with configured timeout.
	clog.WithField("command", command).Debug("Executing post-update command")

	_, err = client.ExecuteCommand(ctx, newContainer, command, timeout, effectiveUID, effectiveGID)
	if err != nil {
		clog.WithError(err).WithFields(logrus.Fields{
			"container_id": newContainerID.ShortID(),
		}).Debug("Post-update command failed")
	}
}

// ExecuteHostPostUpdateCommand executes the host post-update hook for a container.
//
// Unlike ExecutePostUpdateCommand, the command runs on the Watchtower host (the
// process running Watchtower) instead of inside the updated container. The updated
// container's name, ID, and image are exposed to the command via environment
// variables (WT_CONTAINER_NAME, WT_CONTAINER_ID, WT_CONTAINER_IMAGE_NAME).
//
// Parameters:
//   - ctx: Context for cancellation and timeout.
//   - client: Container client used to resolve the updated container.
//   - newContainerID: ID of the updated container.
func ExecuteHostPostUpdateCommand(
	ctx context.Context,
	client container.Client,
	newContainerID types.ContainerID,
) {
	clog := logrus.WithField("container_id", newContainerID.ShortID())
	clog.Debug("Retrieving container for host post-update")

	// Retrieve container by ID.
	newContainer, err := client.GetContainer(ctx, newContainerID)
	if err != nil {
		clog.WithError(err).Debug("Failed to get container for host post-update")

		return
	}

	timeout := newContainer.PostUpdateTimeout()
	clog = logrus.WithFields(logrus.Fields{
		"container": newContainer.Name(),
		"timeout":   timeout,
	})
	command := newContainer.GetLifecycleHostPostUpdateCommand()

	// Skip if no command is set.
	if len(command) == 0 {
		clog.Debug("No host post-update command supplied. Skipping")

		return
	}

	// Execute command with configured timeout.
	clog.WithField("command", command).Debug("Executing host post-update command")

	if _, err := executeHostCommand(ctx, command, timeout, hostCommandEnv(newContainer)); err != nil {
		clog.WithError(err).WithField("container_id", newContainerID.ShortID()).
			Debug("Host post-update command failed")
	}
}
