// Package filters provides filtering logic for Watchtower containers.
// It defines functions to select containers by container names, image names, labels, and scopes.
//
// Key components:
//   - Filter Functions: Select containers (e.g., FilterByNames, FilterByScope).
//   - BuildFilter: Combines filters into a single function.
//
// Usage example:
//
//	filter, desc := filters.BuildFilter(names, disableNames, monitoredImageNamePatterns, skippedImageNamePatterns, true, "scope")
//	containers, _ := client.ListContainers(filter)
//	logrus.Info(desc)
//
// The package uses logrus for logging filter operations and integrates with container types.
package filters
