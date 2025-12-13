package sorter

import (
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Sorter provides a common interface for sorting containers.
type Sorter interface {
	Sort(containers []types.Container) error
}

// SortByCreated sorts containers in place by creation time.
//
// Parameters:
//   - containers: Slice to sort in place.
//
// Returns:
//   - error: Always nil, propagated from sorter.Sort.
func SortByCreated(containers []types.Container) error {
	sorter := TimeSorter{}

	return sorter.Sort(containers)
}

// SortByDependencies sorts containers in place by dependencies.
//
// Parameters:
//   - containers: Slice to sort in place.
//
// Returns:
//   - error: Non-nil if circular reference detected, nil on success.
func SortByDependencies(containers []types.Container) error {
	sorter := DependencySorter{}

	return sorter.Sort(containers)
}
