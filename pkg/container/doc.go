// Package container provides functionality for managing Docker containers in Watchtower.
// It defines types and methods to interact with the Docker API, handle container metadata,
// and implement lifecycle operations like updates, restarts, and image management.
//
// Key components:
//   - Container: Implements types.Container interface for state and metadata operations.
//   - Client: Interface for Docker API interactions (list, start, stop, etc.).
//   - imageClient: Manages image pulling and staleness checks.
//   - Filters: Logic to select containers by names, labels, and scopes.
//   - Labels: Methods to interpret Watchtower-specific labels and lifecycle hooks.
//
// Usage example:
//
//	cli := container.NewClient(container.ClientOptions{})
//	containers, _ := cli.ListContainers(filters.NoFilter)
//	for _, c := range containers {
//	    stale, _, _ := cli.IsContainerStale(c, types.UpdateParams{})
//	    if stale {
//	        cli.StopContainer(c, 10*time.Second)
//	    }
//	}
//
// The package integrates with Docker’s API via docker/docker client libraries and supports
// Watchtower’s update workflows, including authentication, scope filtering, and custom lifecycle hooks.
package container
