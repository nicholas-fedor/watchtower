// Package filters provides filtering logic for Watchtower containers.
// It defines functions to select containers by names, labels, scopes, and images.
//
// Key components:
//   - Filter Functions: Select containers (e.g., FilterByNames, FilterByScope).
//   - BuildFilter: Combines filters into a single function.
//
// Usage example:
//
//	filter, desc := filters.BuildFilter(names, disableNames, true, "scope")
//	containers, _ := client.ListContainers(filter)
//	logrus.Info(desc)
//
// The package uses logrus for logging filter operations and integrates with container types.
package filters
