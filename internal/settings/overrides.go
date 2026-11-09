package settings

import (
	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
)

// RunOverrides holds per-invocation deltas applied on top of process-wide Config.
//
// Process-wide policy always comes from Config. Only fields that can differ per
// run-once, schedule tick, or API request belong here.
type RunOverrides struct {
	// Filter replaces Config.Filter.Predicate when non-nil.
	Filter filters.Filter
	// CurrentContainerID is the running Watchtower container ID when known.
	CurrentContainerID container.ContainerID
	// RunOnce marks a one-shot update session.
	RunOnce bool
	// SkipSelfUpdate disables Watchtower self-update for this invocation.
	SkipSelfUpdate bool
}
