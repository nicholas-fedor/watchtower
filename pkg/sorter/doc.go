// Package sorter provides sorting functionality for Watchtower containers.
// It implements dependency-based topological sorting and creation time ordering.
//
// Key components:
//   - SortByDependencies: Sorts containers by links, detecting circular references.
//   - ByCreated: Sorts containers by creation time with fallback to current time.
//
// Usage example:
//
//	sorted, err := sorter.SortByDependencies(containers)
//	if err != nil {
//	    logrus.WithError(err).Error("Sort failed")
//	} else {
//	    sort.Sort(sorter.ByCreated(sorted))
//	}
//
// The package uses logrus for logging sort operations and errors.
package sorter
