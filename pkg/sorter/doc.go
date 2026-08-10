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
//	// log is the process *zerolog.Logger.
//	err := sorter.SortByDependencies(log, containers, useComposeDependsOn)
//	if err != nil {
//	    log.Error().Err(err).Msg("Dependency sort failed")
//	}
//
//	err = sorter.SortByCreated(log, containers)
//	if err != nil {
//	    log.Error().Err(err).Msg("Time sort failed")
//	}
//
// The package uses zerolog for logging sort operations and errors.
package sorter
