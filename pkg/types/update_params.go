package types

import (
	"time"
)

// UpdateParams defines options for the Update function.
type UpdateParams struct {
	Filter          Filter        // Container filter.
	Cleanup         bool          // Remove old images if true.
	NoRestart       bool          // Skip restarts if true.
	Timeout         time.Duration // Update timeout.
	MonitorOnly     bool          // Monitor without updating if true.
	NoPull          bool          // Skip image pulls if true.
	LifecycleHooks  bool          // Enable lifecycle hooks if true.
	RollingRestart  bool          // Use rolling restart if true.
	LabelPrecedence bool          // Prioritize labels if true.
}
