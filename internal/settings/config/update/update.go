package update

import "time"

// Policy holds the update settings that a container's labels can override. It
// also holds the setting that decides how a label and a global setting combine.
// Container checks receive it without the rest of the update settings.
type Policy struct {
	// MonitorOnly monitors for new images without updating containers
	// (--monitor-only / WATCHTOWER_MONITOR_ONLY).
	MonitorOnly bool
	// NoPull skips pulling new images from the registry
	// (--no-pull / WATCHTOWER_NO_PULL).
	NoPull bool
	// EnableChangelog turns on the changelog notification line and the versioned
	// release link. It is off by default because resolving the exact release tag
	// costs one or two extra registry manifest requests per updated container
	// (--enable-changelog / WATCHTOWER_ENABLE_CHANGELOG).
	EnableChangelog bool
	// LabelPrecedence lets the monitor-only, no-pull and enable-changelog labels
	// override the global settings. Without it, either one enabling a setting
	// enables it. A cooldown-delay label always wins
	// (--label-take-precedence / WATCHTOWER_LABEL_TAKE_PRECEDENCE).
	LabelPrecedence bool
	// CooldownDelay is the minimum age a new image must have before an update is allowed,
	// reducing risk from freshly pushed images (--cooldown-delay / WATCHTOWER_COOLDOWN_DELAY).
	// It accepts extended units such as d, w and M.
	CooldownDelay time.Duration
}

// Update holds the process-wide container update settings.
type Update struct {
	// DiskSpaceMax is the raw --disk-space-max / WATCHTOWER_DISK_SPACE_MAX value.
	DiskSpaceMax string
	// DiskSpaceWarn is the raw --disk-space-warn / WATCHTOWER_DISK_SPACE_WARN value.
	DiskSpaceWarn string
	// Policy holds the settings that a container's labels can override.
	Policy
	// StopTimeout is the maximum duration for container stop before a forceful kill
	// (--stop-timeout / WATCHTOWER_TIMEOUT).
	StopTimeout time.Duration
	// PullFailureDelay is the delay after a failed Watchtower self-update pull in run-once mode.
	PullFailureDelay time.Duration
	// DiskSpaceMaxBytes is the parsed Docker image-usage block threshold in bytes.
	// Zero means the block gate is unset.
	DiskSpaceMaxBytes int64
	// DiskSpaceWarnBytes is the parsed Docker image-usage warning threshold in bytes.
	// Zero means the warning threshold is unset.
	DiskSpaceWarnBytes int64

	// Cleanup removes previously used images after updating
	// (--cleanup / WATCHTOWER_CLEANUP).
	Cleanup bool
	// NoRestart prevents containers from being restarted after an update
	// (--no-restart / WATCHTOWER_NO_RESTART).
	NoRestart bool
	// RollingRestart updates containers sequentially rather than all at once
	// (--rolling-restart / WATCHTOWER_ROLLING_RESTART).
	RollingRestart bool
	// UseComposeDependsOn honors Docker Compose depends_on labels for stop/start order
	// (--use-compose-depends-on / WATCHTOWER_USE_COMPOSE_DEPENDS_ON).
	UseComposeDependsOn bool
	// EphemeralSelfUpdate uses a short-lived orchestrator container for Watchtower self-update
	// (--ephemeral-self-update / WATCHTOWER_EPHEMERAL_SELF_UPDATE).
	EphemeralSelfUpdate bool
}
