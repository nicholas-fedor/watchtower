package hooks

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container"
	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for lifecycle hook execution.
var (
	// errPreUpdateFailed indicates a failure in executing the pre-update command.
	errPreUpdateFailed = errors.New("pre-update command failed")
)

// ExecutePreChecks runs pre-check lifecycle hooks for filtered containers.
//
// When listed is non-nil, those containers are used and ListContainers is skipped.
// A non-nil empty slice is a valid filtered snapshot. Nil falls back to listing.
//
// Parameters:
//   - log: Logger for debug output.
//   - ctx: Context for cancellation and timeout.
//   - client: Container client for execution.
//   - filter: Container filter applied when listed is nil.
//   - uid: Default UID for hooks without a lifecycle UID label.
//   - gid: Default GID for hooks without a lifecycle GID label.
//   - listed: Already-listed containers from the current scan, or nil.
func ExecutePreChecks(
	log *zerolog.Logger,
	ctx context.Context,
	client containerClient.Client,
	filter types.Filter,
	uid, gid int,
	listed []*container.Container,
) {
	containers, ok := resolveCheckContainers(log, ctx, client, filter, listed, "pre-checks")
	if !ok {
		// Listing failed. Hooks are skipped for this phase.
		return
	}

	for _, currentContainer := range containers {
		executePreCheckCommand(log, ctx, client, currentContainer, uid, gid)
	}
}

// ExecutePostChecks runs post-check lifecycle hooks for filtered containers.
//
// When listed is non-nil, those containers are used and ListContainers is skipped.
// A non-nil empty slice is a valid filtered snapshot. Nil falls back to listing.
//
// Parameters:
//   - log: Logger for debug output.
//   - ctx: Context for cancellation and timeout.
//   - client: Container client for execution.
//   - filter: Container filter applied when listed is nil.
//   - uid: Default UID for hooks without a lifecycle UID label.
//   - gid: Default GID for hooks without a lifecycle GID label.
//   - listed: Already-listed containers from the current scan, or nil.
func ExecutePostChecks(
	log *zerolog.Logger,
	ctx context.Context,
	client containerClient.Client,
	filter types.Filter,
	uid, gid int,
	listed []*container.Container,
) {
	containers, ok := resolveCheckContainers(log, ctx, client, filter, listed, "post-checks")
	if !ok {
		// Listing failed. Hooks are skipped for this phase.
		return
	}

	for _, currentContainer := range containers {
		executePostCheckCommand(log, ctx, client, currentContainer, uid, gid)
	}
}

// resolveCheckContainers returns listed containers, or lists them when listed is nil.
//
// Parameters:
//   - log: Logger for debug output.
//   - ctx: Context for cancellation and timeout.
//   - client: Container client for listing when listed is empty.
//   - filter: Container filter applied to a fresh list.
//   - listed: Already-listed containers from the current scan, or nil.
//   - phase: Log label for the pre-check or post-check phase.
//
// Returns:
//   - []*container.Container: Containers to run hooks against.
//   - bool: False when listing fails.
func resolveCheckContainers(
	log *zerolog.Logger,
	ctx context.Context,
	client containerClient.Client,
	filter types.Filter,
	listed []*container.Container,
	phase string,
) ([]*container.Container, bool) {
	clogVal := log.With().
		Str("filter", fmt.Sprintf("%v", filter)).
		Logger()
	clog := &clogVal

	if listed != nil {
		// Use the scan snapshot instead of listing again.
		clog.Debug().
			Int("count", len(listed)).
			Msg("Found containers for " + phase)

		return listed, true
	}

	// Fetch containers using the provided filter.
	clog.Debug().Msg("Listing containers for " + phase)

	containers, err := client.ListContainers(ctx, filter)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to list containers for " + phase)

		return nil, false
	}

	clog.Debug().
		Int("count", len(containers)).
		Msg("Found containers for " + phase)

	return containers, true
}

// executePreCheckCommand executes the pre-check hook for a container.
//
// Parameters:
//   - log: Process logger.
//   - ctx: Context for cancellation and timeout.
//   - client: Container client for execution.
//   - c: Container to process.
//   - uid: Default UID to run command as.
//   - gid: Default GID to run command as.
func executePreCheckCommand(log *zerolog.Logger, ctx context.Context, client containerClient.Client, c *container.Container, uid, gid int) {
	executeCheckCommand(
		log,
		ctx,
		client,
		c,
		uid,
		gid,
		c.GetLifecyclePreCheckCommand(),
		c.PreCheckTimeout,
		"pre-check",
	)
}

// executePostCheckCommand executes the post-check hook for a container.
//
// Parameters:
//   - log: Process logger.
//   - ctx: Context for cancellation and timeout.
//   - client: Container client for execution.
//   - c: Container to process.
//   - uid: Default UID to run command as.
//   - gid: Default GID to run command as.
func executePostCheckCommand(log *zerolog.Logger, ctx context.Context, client containerClient.Client, c *container.Container, uid, gid int) {
	executeCheckCommand(
		log,
		ctx,
		client,
		c,
		uid,
		gid,
		c.GetLifecyclePostCheckCommand(),
		c.PostCheckTimeout,
		"post-check",
	)
}

// executeCheckCommand runs a pre-check or post-check lifecycle command.
//
// Parameters:
//   - log: Process logger.
//   - ctx: Context for cancellation and timeout.
//   - client: Container client for execution.
//   - c: Container to process.
//   - uid: Default UID to run command as.
//   - gid: Default GID to run command as.
//   - command: Command string from the container labels.
//   - timeout: Reads the timeout in minutes from the container labels, where
//     0 means no timeout. It is called only when the command is set.
//   - phase: Label for logs ("pre-check" or "post-check").
func executeCheckCommand(
	log *zerolog.Logger,
	ctx context.Context,
	client containerClient.Client,
	c *container.Container,
	uid, gid int,
	command string,
	timeout func() int,
	phase string,
) {
	clogVal := log.With().
		Str("container", c.Name()).
		Logger()
	clog := &clogVal

	// Skip if no command is set.
	if command == "" {
		clog.Debug().
			Msg("No " + phase + " command supplied. Skipping")

		return
	}

	// The container's lifecycle labels override the configured user.
	effectiveUID, effectiveGID := c.LifecycleUser(uid, gid)

	minutes := timeout()

	clog.Debug().
		Str("command", command).
		Int("timeout", minutes).
		Msg("Executing " + phase + " command")

	_, err := client.ExecuteCommand(ctx, c, command, minutes, effectiveUID, effectiveGID)
	if err != nil {
		// The failure message names the phase, such as "Pre-check command failed".
		failedMsg := "Pre-check command failed"
		if phase == "post-check" {
			failedMsg = "Post-check command failed"
		}

		clog.Debug().
			Err(err).
			Msg(failedMsg)
	}
}

// ExecutePreUpdateCommand executes the pre-update hook for a container.
//
// Parameters:
//   - log: Process logger.
//   - ctx: Context for cancellation and timeout.
//   - client: Container client for execution.
//   - c: Container to process.
//   - uid: UID to run command as.
//   - gid: GID to run command as.
//
// Returns:
//   - bool: True if the update should be skipped, because the command exited
//     with code 75 or failed. False if the command succeeded or did not run.
//   - error: Non-nil if the command failed, nil otherwise.
func ExecutePreUpdateCommand(log *zerolog.Logger,
	ctx context.Context,
	client containerClient.Client,
	c *container.Container,
	uid int,
	gid int,
) (bool, error) {
	command := c.GetLifecyclePreUpdateCommand()
	clogVal := log.With().
		Str("container", c.Name()).
		Logger()
	clog := &clogVal

	// Skip if no command or container isn't running.
	if len(command) == 0 {
		clog.Debug().Msg("No pre-update command supplied. Skipping")

		return false, nil
	}

	if !c.IsRunning() || c.IsRestarting() {
		clog.Debug().
			Bool("is_running", c.IsRunning()).
			Bool("is_restarting", c.IsRestarting()).
			Msg("Container is not running. Skipping pre-update command")

		return false, nil
	}

	// Only a hook that runs reads its timeout label, so an invalid label on a
	// skipped hook logs no warning.
	timeout := c.PreUpdateTimeout()
	clogVal = clog.With().Int("timeout", timeout).Logger()

	// The container's lifecycle labels override the configured user.
	effectiveUID, effectiveGID := c.LifecycleUser(uid, gid)

	// Execute command with configured timeout.
	clog.Debug().
		Str("command", command).
		Msg("Executing pre-update command")

	success, err := client.ExecuteCommand(ctx, c, command, timeout, effectiveUID, effectiveGID)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Pre-update command failed")

		// The callers report the error with the container's name.
		return true, fmt.Errorf("%w: %w", errPreUpdateFailed, err)
	}

	clog.Debug().
		Bool("success", success).
		Msg("Pre-update command executed")

	return success, nil
}

// ExecutePostUpdateCommand executes the post-update hook for a container.
//
// Parameters:
//   - log: Process logger.
//   - ctx: Context for cancellation and timeout.
//   - client: Container client for execution.
//   - newContainerID: ID of the updated container.
//   - uid: UID to run command as.
//   - gid: GID to run command as.
func ExecutePostUpdateCommand(log *zerolog.Logger,
	ctx context.Context,
	client containerClient.Client,
	newContainerID container.ContainerID,
	uid int,
	gid int,
) {
	clogVal := log.With().
		Str("container_id", newContainerID.ShortID()).
		Logger()
	clog := &clogVal
	clog.Debug().Msg("Retrieving container for post-update")

	// Retrieve container by ID.
	newContainer, err := client.GetContainer(ctx, newContainerID)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to get container for post-update")

		return
	}

	clogVal = log.With().
		Str("container", newContainer.Name()).
		Logger()
	clog = &clogVal
	command := newContainer.GetLifecyclePostUpdateCommand()

	// Skip if no command is set.
	if len(command) == 0 {
		clog.Debug().Msg("No post-update command supplied. Skipping")

		return
	}

	// Only a hook that runs reads its timeout label, so an invalid label on a
	// skipped hook logs no warning.
	timeout := newContainer.PostUpdateTimeout()
	clogVal = clog.With().Int("timeout", timeout).Logger()

	// The container's lifecycle labels override the configured user.
	effectiveUID, effectiveGID := newContainer.LifecycleUser(uid, gid)

	// Execute command with configured timeout.
	clog.Debug().
		Str("command", command).
		Msg("Executing post-update command")

	_, err = client.ExecuteCommand(ctx, newContainer, command, timeout, effectiveUID, effectiveGID)
	if err != nil {
		clog.Debug().
			Err(err).
			Str("container_id", newContainerID.ShortID()).
			Msg("Post-update command failed")
	}
}
