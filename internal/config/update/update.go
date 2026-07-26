// Package update holds container update policy settings.
//
// These values flow into types.UpdateParams via config.UpdateParams for every
// run-once, scheduled, and HTTP API update path.
package update

import "time"

// Update holds process-wide container update policy.
type Update struct {
	// Cleanup removes previously used images after updating
	// (--cleanup / WATCHTOWER_CLEANUP).
	Cleanup bool
	// NoPull skips pulling new images from the registry
	// (--no-pull / WATCHTOWER_NO_PULL).
	NoPull bool
	// NoRestart prevents containers from being restarted after an update
	// (--no-restart / WATCHTOWER_NO_RESTART).
	NoRestart bool
	// MonitorOnly monitors for new images without updating containers
	// (--monitor-only / WATCHTOWER_MONITOR_ONLY).
	MonitorOnly bool
	// RollingRestart updates containers sequentially rather than all at once
	// (--rolling-restart / WATCHTOWER_ROLLING_RESTART).
	RollingRestart bool
	// StopTimeout is the maximum duration for container stop before a forceful kill
	// (--stop-timeout / WATCHTOWER_TIMEOUT).
	StopTimeout time.Duration
	// CooldownDelay is the minimum age a new image must have before an update is allowed,
	// reducing risk from freshly pushed images (--cooldown-delay / WATCHTOWER_COOLDOWN_DELAY).
	// Supports extended units such as d, w, and M via util.ParseDuration.
	CooldownDelay time.Duration
	// UseComposeDependsOn honors Docker Compose depends_on labels for stop/start order
	// (--use-compose-depends-on / WATCHTOWER_USE_COMPOSE_DEPENDS_ON).
	UseComposeDependsOn bool
	// LabelPrecedence gives container label settings priority over global flags
	// (--label-take-precedence / WATCHTOWER_LABEL_TAKE_PRECEDENCE).
	LabelPrecedence bool
	// EphemeralSelfUpdate uses a short-lived orchestrator container for Watchtower self-update
	// (--ephemeral-self-update / WATCHTOWER_EPHEMERAL_SELF_UPDATE).
	EphemeralSelfUpdate bool
	// PullFailureDelay is the delay after a failed Watchtower self-update pull.
	PullFailureDelay time.Duration
}
