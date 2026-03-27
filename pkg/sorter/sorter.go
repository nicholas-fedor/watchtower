package sorter

import (
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Sorter provides a common interface for sorting containers.
type Sorter interface {
	Sort(containers []types.Container, useComposeDependsOn bool) error
}

// SortByCreated sorts containers in place by creation time.
//
// Parameters:
//   - containers: Slice to sort in place.
//
// Returns:
//   - error: propagated from TimeSorter.Sort.
func SortByCreated(containers []types.Container) error {
	sorter := TimeSorter{}

	return sorter.Sort(containers, false)
}

// SortByDependencies sorts containers in place by dependencies.
//
// Parameters:
//   - containers: Slice to sort in place.
//   - useComposeDependsOn: Whether to include Docker Compose depends_on label in dependency resolution.
//
// Returns:
//   - error: Non-nil if circular reference detected, nil on success.
func SortByDependencies(containers []types.Container, useComposeDependsOn bool) error {
	sorter := DependencySorter{}

	return sorter.Sort(containers, useComposeDependsOn)
}
