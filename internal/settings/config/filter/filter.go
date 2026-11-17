package filter

import (
	"fmt"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container/filters"
)

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

// Build returns a copy of f with its predicate and description built from its
// settings.
//
// Parameters:
//   - log: Logger for the filter's debug output. May be nil.
//
// Returns:
//   - Filter: The copy with Predicate and Desc set.
//   - error: Non-nil when an enable or disable label pair is malformed.
func (f Filter) Build(log *zerolog.Logger) (Filter, error) {
	predicate, desc, err := filters.BuildFilter(
		log,
		f.Names,
		f.DisableContainers,
		f.MonitorImageNames,
		f.SkipImageNames,
		f.EnableContainersByLabel,
		f.DisableContainersByLabel,
		f.LabelEnable,
		f.Scope,
	)
	if err != nil {
		return Filter{}, fmt.Errorf("build filter: %w", err)
	}

	f.Predicate = predicate
	f.Desc = desc

	return f, nil
}

// ScopeWithLabel returns the scope Watchtower runs in when its own container's
// scope label has the given value.
//
// A scope set in the configuration wins. Without one, a non-empty label value
// is the scope, "none" included. A replacement from a self-update keeps the
// scope of the instance it replaced this way.
//
// Parameters:
//   - label: Value of the scope label on Watchtower's own container. It is
//     empty when the container has no scope label.
//
// Returns:
//   - string: The scope, empty when neither the configuration nor the label
//     sets one.
func (f Filter) ScopeWithLabel(label string) string {
	if f.Scope != "" {
		return f.Scope
	}

	return label
}

// WithLabelScope returns f for the scope that [Filter.ScopeWithLabel] gives,
// rebuilt when the label sets that scope.
//
// Parameters:
//   - log: Logger for the filter's debug output. May be nil.
//   - label: Value of the scope label on Watchtower's own container. It is
//     empty when the container has no scope label.
//
// Returns:
//   - Filter: f unchanged when the label does not apply. Otherwise a copy with
//     the label's scope and a rebuilt predicate and description.
//   - error: Non-nil when an enable or disable label pair is malformed.
func (f Filter) WithLabelScope(log *zerolog.Logger, label string) (Filter, error) {
	scope := f.ScopeWithLabel(label)
	if scope == f.Scope {
		return f, nil
	}

	f.Scope = scope

	return f.Build(log)
}
