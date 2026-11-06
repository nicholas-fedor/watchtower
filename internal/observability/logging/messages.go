package logging

// Process messages. Watchtower logs them when it starts or before it exits.
const (
	// SelfUnidentified is logged when Watchtower runs in a container but
	// cannot find its own container ID.
	SelfUnidentified = "Could not identify Watchtower's own container. " +
		"Self-updates are disabled and old instances will not be removed"
	// SelfUninspected is logged when Watchtower knows its own container ID but
	// cannot inspect the container.
	SelfUninspected = "Could not inspect Watchtower's own container. Self-updates are disabled"
	// ScopeLabelUnreadable is logged when Watchtower exits because it cannot
	// read the scope label of its own container.
	ScopeLabelUnreadable = "Could not inspect Watchtower's own container to read its scope label. " +
		"Exiting rather than running without a scope"
	// RestartsKept is logged when Watchtower fails to set its container's
	// restart policy to "no" before it exits.
	RestartsKept = "Could not disable Watchtower's restart policy. The runtime may start it again"
	// RunConfigurationFailed is logged when Watchtower exits because the
	// settings of its run, such as the HTTP API's, are invalid.
	RunConfigurationFailed = "Failed to build run configuration"
	// RollingRestartValidationFailed is logged when Watchtower exits because
	// it cannot confirm that the monitored containers suit rolling restarts.
	RollingRestartValidationFailed = "Rolling restart compatibility validation failed"
	// APISetupFailed is logged when Watchtower exits because the HTTP API
	// cannot be set up.
	APISetupFailed = "API setup failed"
	// SchedulingFailed is logged when Watchtower exits because its update
	// schedule is invalid.
	SchedulingFailed = "Could not schedule updates"
	// ScopedToContainers is logged when Watchtower is limited to the
	// containers of one scope.
	ScopedToContainers = "Only checking containers in scope"
	// ImageUsageBudgetEnabled is logged when a Docker image usage limit is
	// configured.
	ImageUsageBudgetEnabled = "Docker image usage budget enabled"
	// StartingAPIServer is logged before the HTTP API server starts.
	StartingAPIServer = "Starting HTTP API server"
	// APIServerEnabled is logged once the HTTP API server is set up.
	APIServerEnabled = "HTTP API server is enabled"
)

// Update session messages. Watchtower logs them while it checks and updates
// containers.
const (
	// UpdateFailed is logged when an update session ends with an error.
	UpdateFailed = "Update execution failed"
	// UpdatePanicked is logged when an update session recovers from a panic.
	UpdatePanicked = "Update panicked"
	// UpdateCheckFailed is logged when Watchtower cannot check a container
	// for a newer image or Git revision.
	UpdateCheckFailed = "Could not check container for updates"
	// ConfigNotRecreatable is logged when a stale container is skipped
	// because its configuration cannot be recreated.
	ConfigNotRecreatable = "Could not update container. Its configuration cannot be recreated"
	// RateLimitRetriesExhausted is logged when a container's update check
	// fails after every registry rate limit retry.
	RateLimitRetriesExhausted = "Registry rate limit retries exhausted. Container failed for this cycle"
	// ContainerUpdateFailed is logged when a container's stop, recreation or
	// pre-update command fails while it is being updated.
	ContainerUpdateFailed = "Could not update container"
	// SelfUpdateSkippedRunOnce is logged when a run-once session leaves
	// Watchtower's own container alone.
	SelfUpdateSkippedRunOnce = "Skipping Watchtower self-update in run-once mode"
	// SelfDependency is logged when a container is skipped because it
	// depends on itself.
	SelfDependency = "Skipping container update (self-dependency)"
	// CircularDependency is logged when a container is skipped because it is
	// part of a dependency cycle.
	CircularDependency = "Skipping container update (circular dependency)"
	// UnresolvedDependsOn is logged when a depends-on label entry names no
	// container.
	UnresolvedDependsOn = "Ignoring depends-on entry that names no container"
	// ExcessInstancesRemoved is logged after Watchtower removes the other
	// Watchtower containers in its scope.
	ExcessInstancesRemoved = "Successfully removed all excess Watchtower containers"
)

// Image messages. Watchtower logs them about image updates, cleanup and disk
// use.
const (
	// FoundNewImage is logged when a container's image has a newer version.
	FoundNewImage = "Found new image"
	// RemovingImage is logged when Watchtower removes an old image.
	RemovingImage = "Removing image"
	// ImageUsageCheckFailed is logged when an image is kept because
	// Watchtower cannot list the containers that might still use it.
	ImageUsageCheckFailed = "Failed to list containers for image usage check, skipping removal"
	// WithinCooldown is logged when a new image is too recent to update to.
	WithinCooldown = "Image is within cooldown period - not eligible for update"
	// CooldownElapsed is logged when a new image is old enough to update to.
	CooldownElapsed = "Image age exceeds cooldown - eligible for update"
	// CooldownUnavailable is logged when the age of a new image is unknown,
	// so the cooldown cannot be checked.
	CooldownUnavailable = "Image creation time unavailable - update check unavailable"
	// ImageDiskUsageQueryFailed is logged when Watchtower cannot read
	// Docker's image disk usage.
	ImageDiskUsageQueryFailed = "Failed to query Docker image disk usage"
	// ImageUsageExceedsMaximum is logged when image disk usage is above the
	// configured maximum, so the session is skipped.
	ImageUsageExceedsMaximum = "Docker image usage exceeds configured maximum"
	// ImageUsageExceedsWarning is logged when image disk usage is above the
	// configured warning threshold.
	ImageUsageExceedsWarning = "Docker image usage exceeds configured warning threshold"
	// Changelog is logged with the changelog URL of an updated container.
	Changelog = "Changelog"
)

// Container messages. Watchtower logs them when it stops and starts
// containers.
const (
	// StoppingContainer is logged before Watchtower stops a container.
	StoppingContainer = "Stopping container"
	// StoppingLinkedContainer is logged before Watchtower stops a container
	// that restarts with one it updates.
	StoppingLinkedContainer = "Stopping linked container"
	// StartedNewContainer is logged after Watchtower starts a container.
	StartedNewContainer = "Started new container"
	// StartedLinkedContainer is logged after Watchtower starts a container
	// that restarts with one it updates.
	StartedLinkedContainer = "Started linked container"
	// InvalidPreCheckTimeout is logged when a pre-check timeout label is not
	// a valid timeout.
	InvalidPreCheckTimeout = "Invalid pre-check timeout value, using default"
	// InvalidPostCheckTimeout is logged when a post-check timeout label is
	// not a valid timeout.
	InvalidPostCheckTimeout = "Invalid post-check timeout value, using default"
	// InvalidPreUpdateTimeout is logged when a pre-update timeout label is
	// not a valid timeout.
	InvalidPreUpdateTimeout = "Invalid pre-update timeout value, using default"
	// InvalidPostUpdateTimeout is logged when a post-update timeout label is
	// not a valid timeout.
	InvalidPostUpdateTimeout = "Invalid post-update timeout value, using default"
)

// Git and Compose rebuild messages. Watchtower logs them when it rebuilds
// containers from Git.
const (
	// FoundNewGitRevision is logged when a watched Git ref has a new commit.
	FoundNewGitRevision = "Found new Git revision"
	// BuiltGitImage is logged after Watchtower builds an image from a Git URL.
	BuiltGitImage = "Built image from Git URL context"
	// GitBuildFailed is logged when a Git build fails and the running
	// container is kept.
	GitBuildFailed = "Git build failed. Leaving running container untouched"
	// InvalidGitSemverPolicy is logged when a container is skipped because
	// of an invalid git semver policy label.
	InvalidGitSemverPolicy = "Skipped container with an invalid git semver policy"
	// InvalidGitHost is logged when a container is skipped because of an
	// invalid git-host label.
	InvalidGitHost = "Skipped container with an invalid git-host"
	// BuiltComposeProject is logged after Watchtower applies a Compose
	// project.
	BuiltComposeProject = "Built Compose project"
	// ComposeDirUnreadable is logged when a Compose project directory cannot
	// be read and the running container is kept.
	ComposeDirUnreadable = "Compose project directory is not readable. Leaving the running container untouched"
	// ComposeApplySkipped is logged when a container has no Compose service
	// label to apply.
	ComposeApplySkipped = "Compose apply skipped. Container has no compose service label"
	// ComposeUpdateFailed is logged when a Compose update fails before any
	// container is replaced.
	ComposeUpdateFailed = "Compose update failed before container replacement"
	// ComposeApplyFailed is logged when a Compose apply fails after it may
	// have recreated services.
	ComposeApplyFailed = "Compose apply failed. Docker Compose may have partially recreated services"
	// ComposeApplyUnconfirmed is logged when a Compose apply does not confirm
	// every service it ran.
	ComposeApplyUnconfirmed = "Compose apply finished with unconfirmed service results"
	// ComposeServiceOmitted is logged when a Compose apply result leaves out
	// a service.
	ComposeServiceOmitted = "Compose apply result omitted this service. Runtime state is unknown"
	// ComposeReplicaUnidentified is logged when a Compose apply returns no
	// instance that matches a replica.
	ComposeReplicaUnidentified = "Compose apply did not return an identifiable instance for this replica. " +
		"Runtime state is unknown"
	// ComposeBatchSortFailed is logged when Compose batches cannot be sorted
	// by their dependencies.
	ComposeBatchSortFailed = "Could not dependency-sort Compose batches. Using the existing container order"
	// ComposeBatchOrderFailed is logged when Compose batches cannot be
	// ordered by their dependencies.
	ComposeBatchOrderFailed = "Could not order Compose batches by dependencies. Using first-seen order"
	// ComposeBatchesCyclic is logged when Compose batches depend on each
	// other in a cycle.
	ComposeBatchesCyclic = "Compose batch dependencies are cyclic. Appending unresolved batches in first-seen order"
)
