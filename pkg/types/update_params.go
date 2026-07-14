package types

import (
	"time"
)

// UpdateParams defines options for the Update function.
type UpdateParams struct {
	Filter              Filter        `json:"-"`                      // Container filter.
	Cleanup             bool          `json:"cleanup"`                // Remove old images if true.
	NoRestart           bool          `json:"no_restart"`             // Skip restarts if true.
	ReviveStopped       bool          `json:"revive_stopped"`         // Start stopped containers after update if true.
	Timeout             time.Duration `json:"timeout"`                // Update timeout.
	MonitorOnly         bool          `json:"monitor_only"`           // Monitor without updating if true.
	NoPull              bool          `json:"no_pull"`                // Skip image pulls if true.
	LifecycleHooks      bool          `json:"lifecycle_hooks"`        // Enable lifecycle hooks if true.
	RollingRestart      bool          `json:"rolling_restart"`        // Use rolling restart if true.
	LabelPrecedence     bool          `json:"label_precedence"`       // Prioritize labels if true.
	PullFailureDelay    time.Duration `json:"pull_failure_delay"`     // Delay after failed self-update pull.
	LifecycleUID        int           `json:"lifecycle_uid"`          // Default UID for lifecycle hooks.
	LifecycleGID        int           `json:"lifecycle_gid"`          // Default GID for lifecycle hooks.
	CPUCopyMode         string        `json:"cpu_copy_mode"`          // CPU copy mode for container recreation.
	RunOnce             bool          `json:"run_once"`               // Run once mode if true.
	CurrentContainerID  ContainerID   `json:"current_container_id"`   // ID of the current container being updated.
	UseComposeDependsOn bool          `json:"use_compose_depends_on"` // Enable Docker Compose depends_on label processing.
	SkipSelfUpdate      bool          `json:"skip_self_update"`       // Skip Watchtower self-update if true.
	EphemeralSelfUpdate bool          `json:"ephemeral_self_update"`  // Use ephemeral container for self-update if true.
	CooldownDelay       time.Duration `json:"cooldown_delay"`         // Minimum time since image creation before allowing updates.
}
