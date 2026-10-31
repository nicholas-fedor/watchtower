package labels

// Watchtower labels identify the containers Watchtower manages and configure how it updates them.
const (
	// Watchtower marks a container as the Watchtower instance itself when set to "true".
	Watchtower = "com.centurylinklabs.watchtower"
	// StopSignal specifies a custom stop signal for the container (e.g., "SIGTERM").
	StopSignal = "com.centurylinklabs.watchtower.stop-signal"
	// Enable indicates whether Watchtower should manage this container (true/false).
	Enable = "com.centurylinklabs.watchtower.enable"
	// MonitorOnly flags the container for monitoring only, without updates (true/false).
	MonitorOnly = "com.centurylinklabs.watchtower.monitor-only"
	// NoPull prevents Watchtower from pulling a new image for this container (true/false).
	NoPull = "com.centurylinklabs.watchtower.no-pull"
	// DependsOn lists container names this container depends on, comma-separated.
	DependsOn = "com.centurylinklabs.watchtower.depends-on"
	// ContainerChain accumulates container IDs across Watchtower self-updates, comma-separated.
	ContainerChain = "com.centurylinklabs.watchtower.container-chain"
	// ZodiacOriginalImage stores the original image name for Zodiac compatibility.
	ZodiacOriginalImage = "com.centurylinklabs.zodiac.original-image"
	// Scope defines a unique monitoring scope for this Watchtower instance.
	Scope = "com.centurylinklabs.watchtower.scope"
	// EphemeralOrchestrator identifies ephemeral orchestrator containers used during self-update.
	EphemeralOrchestrator = "com.centurylinklabs.watchtower.ephemeral-orchestrator"
	// CooldownDelay sets the minimum image age before updating this container.
	// Accepts duration strings (e.g., "24h", "3d", "1w", "0" to disable).
	CooldownDelay = "com.centurylinklabs.watchtower.cooldown-delay"
	// CopyFile lists in-container file paths to copy during recreation, comma-separated.
	CopyFile = "com.centurylinklabs.watchtower.copy-file"
)

// Lifecycle hook labels configure commands executed during container update phases.
const (
	// LifecyclePreCheck specifies a command to run before checking for updates.
	LifecyclePreCheck = "com.centurylinklabs.watchtower.lifecycle.pre-check"
	// LifecyclePostCheck specifies a command to run after checking for updates.
	LifecyclePostCheck = "com.centurylinklabs.watchtower.lifecycle.post-check"
	// LifecyclePreUpdate specifies a command to run before updating the container.
	LifecyclePreUpdate = "com.centurylinklabs.watchtower.lifecycle.pre-update"
	// LifecyclePostUpdate specifies a command to run after updating the container.
	LifecyclePostUpdate = "com.centurylinklabs.watchtower.lifecycle.post-update"
	// LifecyclePreCheckTimeout sets the timeout (in minutes) for the pre-check command.
	LifecyclePreCheckTimeout = "com.centurylinklabs.watchtower.lifecycle.pre-check-timeout"
	// LifecyclePostCheckTimeout sets the timeout (in minutes) for the post-check command.
	LifecyclePostCheckTimeout = "com.centurylinklabs.watchtower.lifecycle.post-check-timeout"
	// LifecyclePreUpdateTimeout sets the timeout (in minutes) for the pre-update command.
	LifecyclePreUpdateTimeout = "com.centurylinklabs.watchtower.lifecycle.pre-update-timeout"
	// LifecyclePostUpdateTimeout sets the timeout (in minutes) for the post-update command.
	LifecyclePostUpdateTimeout = "com.centurylinklabs.watchtower.lifecycle.post-update-timeout"
	// LifecycleUID specifies the UID to run lifecycle hooks as.
	LifecycleUID = "com.centurylinklabs.watchtower.lifecycle.uid"
	// LifecycleGID specifies the GID to run lifecycle hooks as.
	LifecycleGID = "com.centurylinklabs.watchtower.lifecycle.gid"
)

// Git and changelog labels associate a container with a Git source and record what Watchtower built from it.
const (
	// GitRepo is the clone URL of the Git repository the container is built from.
	GitRepo = "com.centurylinklabs.watchtower.git-repo"
	// GitRef names the branch or tag to watch in GitRepo.
	GitRef = "com.centurylinklabs.watchtower.git-ref"
	// GitBranch is an alias for GitRef.
	GitBranch = "com.centurylinklabs.watchtower.git-branch"
	// GitHost is the provider's HTTP API origin when it differs from the GitRepo host.
	GitHost = "com.centurylinklabs.watchtower.git-host"
	// GitSemverPolicy sets how far a semantic version tag may advance (none, patch, minor, or major).
	GitSemverPolicy = "com.centurylinklabs.watchtower.git-semver-policy"
	// GitWatch turns the Git watcher on or off for this container, overriding the git-enable option.
	GitWatch = "com.centurylinklabs.watchtower.git-watch"
	// GitDockerfile is the Dockerfile path inside GitRepo for rebuilds.
	GitDockerfile = "com.centurylinklabs.watchtower.git-dockerfile"
	// GitContext is the build context directory inside GitRepo for rebuilds.
	GitContext = "com.centurylinklabs.watchtower.git-context"
	// EnableChangelog enables the changelog notification line and versioned release link for this container.
	EnableChangelog = "com.centurylinklabs.watchtower.enable-changelog"
	// ChangelogURL is the container's changelog URL template. It does not enable changelogs.
	ChangelogURL = "com.centurylinklabs.watchtower.changelog-url"
	// GitLastCommit records the commit Watchtower last built the container from.
	GitLastCommit = "com.centurylinklabs.watchtower.git-last-commit"
	// GitLastTag records the tag of the last tag rebuild.
	GitLastTag = "com.centurylinklabs.watchtower.git-last-tag"
	// ComposeDir is a path inside Watchtower to the Compose project directory.
	ComposeDir = "com.centurylinklabs.watchtower.compose-dir"
)

// Label values with a fixed meaning.
const (
	// NoScope is the scope of a container whose scope label is unset, empty, or
	// set to "none".
	NoScope = "none"
)

// NormalizeScope returns scope, or [NoScope] when scope is empty.
//
// Parameters:
//   - scope: A scope label value or a configured scope.
//
// Returns:
//   - string: The scope, with an empty one as [NoScope].
func NormalizeScope(scope string) string {
	if scope == "" {
		return NoScope
	}

	return scope
}
