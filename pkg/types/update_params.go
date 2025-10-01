package types

import (
	"time"
)

// UpdateParams defines options for the Update function.
type UpdateParams struct {
	Filter           Filter        // Container filter.
	Cleanup          bool          // Remove old images if true.
	NoRestart        bool          // Skip restarts if true.
	Timeout          time.Duration // Update timeout.
	MonitorOnly      bool          // Monitor without updating if true.
	NoPull           bool          // Skip image pulls if true.
	LifecycleHooks   bool          // Enable lifecycle hooks if true.
	RollingRestart   bool          // Use rolling restart if true.
	LabelPrecedence  bool          // Prioritize labels if true.
	PullFailureDelay time.Duration // Delay after failed self-update pull.
	LifecycleUID     int           // Default UID for lifecycle hooks.
	LifecycleGID     int           // Default GID for lifecycle hooks.
	NoSelfUpdate     bool          // Skip self-update of Watchtower if true.
	CPUCopyMode      string        // CPU copy mode for container recreation.
	GitAuthToken     string        // Git authentication token for private repositories.
	GitUsername      string        // Git username for basic authentication.
	GitPassword      string        // Git password for basic authentication.
	GitSSHKeyPath    string        // Path to SSH key file for Git authentication.
}
