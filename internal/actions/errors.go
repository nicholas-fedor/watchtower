package actions

import "errors"

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
	// errImageCleanupFailed flags failures in image cleanup.
	errImageCleanupFailed = errors.New("errors occurred during image cleanup")
)

// Errors for update operations.
var (
	// errSortDependenciesFailed indicates a failure to sort containers by dependencies.
	errSortDependenciesFailed = errors.New("failed to sort containers by dependencies")
	// errVerifyConfigFailed indicates a failure to verify container configuration for recreation.
	errVerifyConfigFailed = errors.New("failed to verify container configuration")
	// errPreUpdateFailed indicates a failure in the pre-update lifecycle command execution.
	errPreUpdateFailed = errors.New("pre-update command failed")
	// errSkipUpdate signals that a container update should be skipped due to a specific condition (e.g., EX_TEMPFAIL).
	errSkipUpdate = errors.New(
		"skipping container due to pre-update command exit code 75 (EX_TEMPFAIL)",
	)
	// errRenameWatchtowerFailed indicates a failure to rename the Watchtower container before restarting.
	errRenameWatchtowerFailed = errors.New("failed to rename Watchtower container")
	// errStopContainerFailed indicates a failure to stop a container during the update process.
	errStopContainerFailed = errors.New("failed to stop container")
	// errStartContainerFailed indicates a failure to start a container after an update.
	errStartContainerFailed = errors.New("failed to start container")
	// errParseImageReference indicates a failure to parse a container’s image reference.
	errParseImageReference = errors.New("failed to parse image reference")
)
