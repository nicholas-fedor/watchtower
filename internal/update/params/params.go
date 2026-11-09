package params

import (
	"math"
	"time"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/observability/events"
	gitConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/git"
	registryConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/registry"
	updateConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/update"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Params holds the settings that one update run applies to the containers it checks.
type Params struct {
	Filter              types.Filter            // Container filter.
	ComposeProjects     map[string]string       // Compose project name to directory inside Watchtower.
	CurrentContainerID  types.ContainerID       // ID of the current container being updated.
	Git                 gitConfig.Defaults      // Credential-free Git association settings.
	Registry            registryConfig.Registry // Settings for registry requests.
	updateConfig.Policy                         // Settings that a container's labels can override.
	Timeout             time.Duration           // Container stop timeout before a forced kill.
	PullFailureDelay    time.Duration           // Delay after a failed self-update pull in run-once mode.
	LifecycleUID        int                     // Default UID for lifecycle hooks.
	LifecycleGID        int                     // Default GID for lifecycle hooks.
	DiskSpaceMax        int64                   // Block session when Docker image usage reaches this many bytes. Zero disables the block gate.
	DiskSpaceWarn       int64                   // Warn when Docker image usage reaches this many bytes. Zero disables the warning.
	GitTimeout          time.Duration           // Git network timeout.
	Cleanup             bool                    // Remove old images if true.
	NoRestart           bool                    // Skip restarts if true.
	ReviveStopped       bool                    // Start stopped containers after update if true.
	LifecycleHooks      bool                    // Enable lifecycle hooks if true.
	RollingRestart      bool                    // Use rolling restart if true.
	RunOnce             bool                    // Run once mode if true.
	UseComposeDependsOn bool                    // Enable Docker Compose depends_on label processing.
	SkipSelfUpdate      bool                    // Skip Watchtower self-update if true.
	EphemeralSelfUpdate bool                    // Use ephemeral container for self-update if true.
	LabelEnable         bool                    // Require enable label for monitoring.
	GitComposeStash     bool                    // Save and restore local Compose project files around checkout.
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
func (p Params) DetachedTimeout() time.Duration {
	return detachedBound(p.Timeout)
}

// DetachedStopTimeout returns the bound for detached Docker calls that stop
// container c, such as removing a failed replacement.
//
// A stop timeout set on c takes precedence over the configured one, so the stop
// can wait longer than [Params.DetachedTimeout] allows for. The bound is
// the longer of the two stop timeouts plus 30 seconds, so it is never shorter
// than [Params.DetachedTimeout].
//
// Parameters:
//   - c: The container the detached calls stop.
//
// Returns:
//   - time.Duration: The bound for the detached calls.
func (p Params) DetachedStopTimeout(c *container.Container) time.Duration {
	return detachedBound(max(p.Timeout, c.StopTimeout(p.Timeout)))
}

// ScanStartedData returns the payload of the scan_started event. It copies only
// the update flags the event publishes and leaves out every other setting.
//
// Returns:
//   - events.ScanStartedData: The published update settings.
func (p Params) ScanStartedData() events.ScanStartedData {
	return events.ScanStartedData{
		Cleanup:             p.Cleanup,
		NoRestart:           p.NoRestart,
		MonitorOnly:         p.MonitorOnly,
		LifecycleHooks:      p.LifecycleHooks,
		RollingRestart:      p.RollingRestart,
		LabelPrecedence:     p.LabelPrecedence,
		NoPull:              p.NoPull,
		RunOnce:             p.RunOnce,
		UseComposeDependsOn: p.UseComposeDependsOn,
		SkipSelfUpdate:      p.SkipSelfUpdate,
		EphemeralSelfUpdate: p.EphemeralSelfUpdate,
		ReviveStopped:       p.ReviveStopped,
	}
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
