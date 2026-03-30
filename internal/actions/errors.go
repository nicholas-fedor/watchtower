package actions

import "errors"

// Errors for sanity and instance checks.
var (
	// errRollingRestartDependency flags incompatible dependencies for rolling restarts.
	errRollingRestartDependency = errors.New(
		"container has dependencies incompatible with rolling restarts",
	)
	// errListContainersFailed flags failures in listing containers.
	errListContainersFailed = errors.New("failed to list containers")
	// errImageRemovalFailed flags failures in image cleanup.
	errImageRemovalFailed = errors.New("errors occurred during image cleanup")
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
	// errStopContainerFailed indicates a failure to stop a container during the update process.
	errStopContainerFailed = errors.New("failed to stop container")
	// errStartContainerFailed indicates a failure to start a container after an update.
	errStartContainerFailed = errors.New("failed to start container")
	// errParseImageReference indicates a failure to parse a container’s image reference.
	errParseImageReference = errors.New("failed to parse image reference")
	// errInvalidImageReference indicates an invalid image reference that cannot be processed.
	errInvalidImageReference = errors.New("invalid image reference")
	// errCircularDependency indicates a circular dependency between containers.
	errCircularDependency = errors.New("circular dependency detected")
	// errSelfDependency indicates a container has a self-dependency.
	errSelfDependency = errors.New("container has self-dependency")
)

// Errors for image cooldown operations.
var (
	// errImageCooldown indicates the image is within the cooldown period and the update is deferred.
	errImageCooldown = errors.New("deferred")
	// errFetchImageAgeFailed indicates the image creation time could not be determined from the registry.
	errFetchImageAgeFailed = errors.New("image creation time unavailable")
)

// Errors for Watchtower self-update operations.
var (
	// errRenameWatchtowerFailed indicates a failure to rename the Watchtower container before restarting.
	errRenameWatchtowerFailed = errors.New("failed to rename Watchtower container")
	// errStopWatchtowerFailed flags failures in stopping excess Watchtower instances.
	errStopWatchtowerFailed = errors.New("errors occurred while stopping watchtower containers")
)
