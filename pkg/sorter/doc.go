// Package sorter provides sorting functionality for Watchtower containers.
// It implements dependency-based topological sorting and creation time ordering.
//
// Key components:
//   - SortByDependencies: Sorts containers in place by links, detecting circular references.
//   - SortByCreated: Sorts containers in place by creation time with fallback to current time.
//   - Sorter: Common interface for all sorting implementations.
//
// Usage example:
//
//	err := sorter.SortByDependencies(containers)
//	if err != nil {
//	    logrus.WithError(err).Error("Dependency sort failed")
//	}
//
//	err = sorter.SortByCreated(containers)
//	if err != nil {
//	    logrus.WithError(err).Error("Time sort failed")
//	}
//
// The package uses logrus for logging sort operations and errors.
package sorter
