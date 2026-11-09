package filter

import "github.com/nicholas-fedor/watchtower/internal/container/filters"

// Filter holds raw filter inputs and the resolved predicate.
type Filter struct {
	// Predicate is the resolved container filter function.
	Predicate filters.Filter
	// Scope is the Watchtower monitoring scope label value.
	Scope string
	// Desc is a human-readable description of the filter.
	Desc string
	// DisableContainers lists container names to exclude.
	DisableContainers []string
	// MonitorImageNames lists image name patterns to include.
	MonitorImageNames []string
	// SkipImageNames lists image name patterns to exclude.
	SkipImageNames []string
	// EnableContainersByLabel lists key=value pairs that must match.
	EnableContainersByLabel []string
	// DisableContainersByLabel lists key=value pairs that exclude matches.
	DisableContainersByLabel []string
	// Names are positional container name arguments.
	Names []string
	// LabelEnable restricts watching to containers with the enable label.
	LabelEnable bool
}
