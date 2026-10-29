package types

import (
	"math"
	"time"

	registryConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/registry"
)

// UpdateParams defines options for the Update function.
type UpdateParams struct {
	Filter              Filter                  `json:"-"`                      // Container filter.
	Cleanup             bool                    `json:"cleanup"`                // Remove old images if true.
	NoRestart           bool                    `json:"no_restart"`             // Skip restarts if true.
	ReviveStopped       bool                    `json:"revive_stopped"`         // Start stopped containers after update if true.
	Timeout             time.Duration           `json:"timeout"`                // Container stop timeout before a forced kill.
	MonitorOnly         bool                    `json:"monitor_only"`           // Monitor without updating if true.
	NoPull              bool                    `json:"no_pull"`                // Skip image pulls if true.
	EnableChangelog     bool                    `json:"enable_changelog"`       // Resolve a release-notes link in notifications if true.
	LifecycleHooks      bool                    `json:"lifecycle_hooks"`        // Enable lifecycle hooks if true.
	RollingRestart      bool                    `json:"rolling_restart"`        // Use rolling restart if true.
	LabelPrecedence     bool                    `json:"label_precedence"`       // Prioritize labels if true.
	PullFailureDelay    time.Duration           `json:"pull_failure_delay"`     // Delay after a failed self-update pull in run-once mode.
	LifecycleUID        int                     `json:"lifecycle_uid"`          // Default UID for lifecycle hooks.
	LifecycleGID        int                     `json:"lifecycle_gid"`          // Default GID for lifecycle hooks.
	RunOnce             bool                    `json:"run_once"`               // Run once mode if true.
	CurrentContainerID  ContainerID             `json:"current_container_id"`   // ID of the current container being updated.
	UseComposeDependsOn bool                    `json:"use_compose_depends_on"` // Enable Docker Compose depends_on label processing.
	SkipSelfUpdate      bool                    `json:"skip_self_update"`       // Skip Watchtower self-update if true.
	EphemeralSelfUpdate bool                    `json:"ephemeral_self_update"`  // Use ephemeral container for self-update if true.
	CooldownDelay       time.Duration           `json:"cooldown_delay"`         // Minimum time since image creation before allowing updates.
	LabelEnable         bool                    `json:"label_enable"`           // Require enable label for monitoring.
	DiskSpaceMax        int64                   `json:"disk_space_max"`         // Block session when Docker image usage reaches this many bytes. Zero disables the block gate.
	DiskSpaceWarn       int64                   `json:"disk_space_warn"`        // Warn when Docker image usage reaches this many bytes. Zero disables the warning.
	EnableGitMonitoring bool                    `json:"enable_git_monitoring"`  // Process-wide Git watcher default.
	GitDefaultRef       string                  `json:"git_default_ref"`        // Ref used when a mapping or label omits one.
	GitSemverPolicy     string                  `json:"git_semver_policy"`      // Semver policy used when a mapping or label omits one.
	GitTimeout          time.Duration           `json:"git_timeout"`            // Git network timeout.
	GitImages           map[string]GitImage     `json:"git_images"`             // Per-image Git associations keyed by ImageName().
	GitDockerfile       string                  `json:"git_dockerfile"`         // Default Dockerfile path relative to the build context.
	GitContext          string                  `json:"git_context"`            // Default subdirectory of a Git URL context used as the Docker build context.
	GitComposeStash     bool                    `json:"git_compose_stash"`      // Save and restore local Compose project files around checkout.
	ComposeProjects     map[string]string       `json:"compose_projects"`       // Compose project name to directory inside Watchtower.
	Registry            registryConfig.Registry `json:"-"`                      // Settings for registry requests.
}

// detachedCallBudget is the time a detached Docker call gets beyond the
// container stop timeout.
const detachedCallBudget = 30 * time.Second

// DetachedTimeout returns the bound for Docker calls that run detached from the
// update session, such as restoring a container after a failed update or
// removing old images.
//
// The bound is the container stop timeout plus 30 seconds. A detached stop can
// then wait out the stop timeout and still leave time for the calls after it.
// A negative stop timeout counts as zero, and the bound saturates at the longest
// duration rather than overflowing.
//
// Returns:
//   - time.Duration: The bound for detached Docker calls.
func (p UpdateParams) DetachedTimeout() time.Duration {
	return detachedBound(p.Timeout)
}

// DetachedStopTimeout returns the bound for detached Docker calls that stop
// container c, such as removing a failed replacement.
//
// A stop timeout set on c takes precedence over the configured one, so the stop
// can wait longer than [UpdateParams.DetachedTimeout] allows for. The bound is
// the longer of the two stop timeouts plus 30 seconds, so it is never shorter
// than [UpdateParams.DetachedTimeout].
//
// Parameters:
//   - c: The container the detached calls stop.
//
// Returns:
//   - time.Duration: The bound for the detached calls.
func (p UpdateParams) DetachedStopTimeout(c Container) time.Duration {
	return detachedBound(max(p.Timeout, c.StopTimeout(p.Timeout)))
}

// detachedBound returns a stop timeout plus 30 seconds. A negative stop timeout
// counts as zero, and the result saturates at the longest duration.
//
// Parameters:
//   - stop: How long a stop may wait.
//
// Returns:
//   - time.Duration: The bound for detached Docker calls around that stop.
func detachedBound(stop time.Duration) time.Duration {
	return min(max(stop, 0), math.MaxInt64-detachedCallBudget) + detachedCallBudget
}
