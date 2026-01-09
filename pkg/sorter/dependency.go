package sorter

import (
	"sort"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// DependencySorter handles topological sorting by dependencies.
type DependencySorter struct{}

// Sort sorts containers in place by dependencies, placing Watchtower containers last.
//
// This function implements a two-phase sorting strategy to ensure proper update order:
//  1. Separate Watchtower containers from regular containers, as Watchtower instances
//     should always be updated last to avoid disrupting the update process itself.
//  2. Perform topological sorting on non-Watchtower containers using Kahn's algorithm
//     to respect dependency relationships (containers that depend on others must be
//     updated after their dependencies).
//
// The sorting ensures that:
// - Dependent containers are updated after their dependencies
// - Watchtower containers are processed last to maintain monitoring capability
// - Circular dependencies are detected and reported as errors
//
// Time Complexity: O(V + E) where V is containers and E is dependency links
// Space Complexity: O(V + E) for graph structures
//
// Parameters:
//   - containers: Slice to sort in place. Modified directly.
//
// Returns:
//   - error: Non-nil if circular reference detected, nil on success.
//     Error includes the container name and cycle path for debugging.
func (ds DependencySorter) Sort(containers []types.Container) error {
	if containers == nil {
		return nil
	}

	logrus.WithField("container_count", len(containers)).Debug("Starting dependency sort")

	// Separate Watchtower containers from non-Watchtower containers
	var (
		nonWatchtowerContainers []types.Container
		watchtowerContainers    []types.Container
	)

	for _, container := range containers {
		if container.IsWatchtower() {
			watchtowerContainers = append(watchtowerContainers, container)
		} else {
			nonWatchtowerContainers = append(nonWatchtowerContainers, container)
		}
	}

	logrus.WithFields(logrus.Fields{
		"non_watchtower_count": len(nonWatchtowerContainers),
		"watchtower_count":     len(watchtowerContainers),
	}).Debug("Separated containers by Watchtower status")

	// Sort non-Watchtower containers by dependencies using internal sorter
	sortedNonWatchtower, err := sortByDependencies(nonWatchtowerContainers)
	if err != nil {
		logrus.WithError(err).Debug("Dependency sort failed for non-Watchtower containers")

		return err
	}

	// Copy sorted results back to original slice
	copy(containers, sortedNonWatchtower)

	for i, wt := range watchtowerContainers {
		containers[len(sortedNonWatchtower)+i] = wt
	}

	sortedNames := make([]string, len(containers))
	for i, c := range containers {
		sortedNames[i] = c.Name()
	}

	logrus.WithFields(logrus.Fields{
		"sorted_count": len(containers),
		"sorted_order": sortedNames,
	}).Debug("Completed dependency sort with Watchtower containers last")

	return nil
}

// sortByDependencies performs topological sort on containers using Kahn's algorithm.
//
// Kahn's algorithm is a breadth-first approach to topological sorting that works by:
// 1. Building a directed graph where nodes are containers and edges represent dependencies
// 2. Calculating indegree (number of incoming edges) for each node
// 3. Starting with nodes that have zero indegree (no dependencies)
// 4. Processing nodes in order, reducing indegree of dependents, and adding them to queue when indegree becomes zero
// 5. Detecting cycles if not all nodes are processed
//
// This implementation uses normalized container identifiers to handle Docker Compose service names
// and container names consistently. The queue is sorted in reverse alphabetical order to match
// the behavior of previous DFS-based implementations for consistency.
//
// Time Complexity: O(V + E) where V = number of containers, E = number of dependency links
// Space Complexity: O(V + E) for maps and adjacency lists
//
// Edge Cases:
// - Empty container list: returns empty list
// - Single container: returns that container
// - Circular dependencies: detected and reported with cycle path
// - Containers with no dependencies: processed first
// - Missing dependency targets: ignored (only considers existing containers)
//
// Parameters:
//   - containers: List to sort. Should not be nil.
//
// Returns:
//   - []types.Container: Sorted list in dependency order (dependencies first).
//   - error: Non-nil if circular reference detected, nil on success.
//     Error includes container name and cycle path for debugging.
func sortByDependencies(containers []types.Container) ([]types.Container, error) {
	// Phase 1: Build the dependency graph data structures
	containerMap, indegree, adjacency, normalizedMap, err := buildDependencyGraph(containers)
	if err != nil {
		return nil, err
	}

	// Phase 2: Initialize processing queue with containers that have no dependencies
	queue := initializeQueue(indegree)

	// Phase 3: Process the queue using Kahn's algorithm
	// While there are containers with no remaining dependencies:
	// - Remove a container from queue and add it to sorted result
	// - For each container that depends on it, decrement their indegree
	// - If a dependent's indegree becomes 0, add it to queue
	sorted := make([]types.Container, 0, len(containers))
	for len(queue) > 0 {
		// Dequeue the next container with no dependencies
		current := queue[0]
		queue = queue[1:]

		// Add to sorted result - this container can be updated now
		sorted = append(sorted, containerMap[current])
		logrus.WithFields(logrus.Fields{
			"container_id": containerMap[current].ID().ShortID(),
			"name":         containerMap[current].Name(),
		}).Debug("Added container to sorted list")

		// Update all containers that depend on this one
		for _, dependent := range adjacency[current] {
			indegree[dependent]--
			// If this dependent now has no remaining dependencies, add to queue
			if indegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// Phase 4: Cycle detection
	if err := detectAndReportCycle(
		sorted,
		containers,
		containerMap,
		adjacency,
		normalizedMap,
	); err != nil {
		return nil, err
	}

	return sorted, nil
}

// buildDependencyGraph constructs the dependency graph data structures for topological sorting.
//
// This function builds three key data structures:
// - containerMap: Maps normalized container identifiers to container objects for O(1) lookup
// - indegree: Tracks number of incoming dependencies for each container (nodes with indegree 0 have no dependencies)
// - adjacency: Lists containers that depend on each container (outgoing edges)
//
// Normalization ensures consistent handling of Docker Compose service names vs container names.
// Container links from c.Links() are already normalized.
//
// Parameters:
//   - containers: List of containers to build graph for.
//
// Returns:
//   - map[string]types.Container: Container lookup map
//   - map[string]int: Indegree count for each container
//   - map[string][]string: Adjacency list (container -> dependents)
//   - map[types.Container]string: Reverse map from container to normalized identifier
//   - error: IdentifierCollisionError if duplicate identifiers detected, nil otherwise
func buildDependencyGraph(
	containers []types.Container,
) (map[string]types.Container, map[string]int, map[string][]string, map[types.Container]string, error) {
	containerMap := make(map[string]types.Container)
	indegree := make(map[string]int)
	adjacency := make(map[string][]string)
	normalizedMap := make(map[types.Container]string)

	// Use temporary map to collect all containers per normalized identifier
	tempMap := make(map[string][]types.Container)

	for _, c := range containers {
		normalizedIdentifier := util.NormalizeContainerName(container.ResolveContainerIdentifier(c))
		tempMap[normalizedIdentifier] = append(tempMap[normalizedIdentifier], c)
	}

	// Check for identifier collisions
	for identifier, containers := range tempMap {
		if len(containers) > 1 {
			logrus.Errorf(
				"Identifier collision detected: '%s' used by multiple containers: %v",
				identifier,
				containers,
			)

			return nil, nil, nil, nil, IdentifierCollisionError{
				DuplicateIdentifier: identifier,
				AffectedContainers:  containers,
			}
		}
		// No collision, populate maps
		containerMap[identifier] = containers[0]
		indegree[identifier] = 0
		normalizedMap[containers[0]] = identifier
	}

	// Build the graph by processing container links (dependencies)
	// For each container, increment its indegree for each link it has to an existing container
	// Add reverse edges in adjacency list: link target -> dependent container
	for _, c := range containers {
		normalizedIdentifier := normalizedMap[c]
		// c.Links() already returns normalized container names
		for _, normalizedLink := range c.Links() {
			if _, exists := containerMap[normalizedLink]; exists {
				// This container depends on the linked container, so increment its indegree
				indegree[normalizedIdentifier]++
				// The linked container has this container as a dependent
				adjacency[normalizedLink] = append(adjacency[normalizedLink], normalizedIdentifier)
			}
		}
	}

	return containerMap, indegree, adjacency, normalizedMap, nil
}

// initializeQueue creates the initial processing queue for Kahn's algorithm.
//
// This function identifies all containers with no dependencies (indegree 0) and
// sorts them in reverse alphabetical order to ensure deterministic ordering
// when multiple containers have zero indegree. This provides consistent,
// reproducible sorting order for containers with no dependencies.
//
// Parameters:
//   - indegree: Map of container identifiers to their indegree count.
//
// Returns:
//   - []string: Sorted queue of container identifiers with zero indegree.
func initializeQueue(indegree map[string]int) []string {
	var queue []string

	for identifier, deg := range indegree {
		if deg == 0 {
			queue = append(queue, identifier)
		}
	}

	// Sort queue in reverse alphabetical order to ensure deterministic ordering
	sort.Sort(sort.Reverse(sort.StringSlice(queue)))

	return queue
}

// detectAndReportCycle checks for circular dependencies and reports detailed error information.
//
// This function implements cycle detection for Kahn's algorithm. If not all containers
// were processed during topological sorting, there must be a cycle in the dependency graph.
// It identifies unprocessed containers, selects the first one, and uses DFS to find
// the actual cycle path for detailed error reporting.
//
// Parameters:
//   - sorted: List of successfully sorted containers
//   - containers: Original list of all containers
//   - containerMap: Map of container identifiers to container objects
//   - adjacency: Graph adjacency list (container -> dependents)
//   - normalizedMap: Map of containers to their normalized identifiers
//
// Returns:
//   - error: CircularReferenceError if cycle detected, nil otherwise
func detectAndReportCycle(
	sorted []types.Container,
	containers []types.Container,
	containerMap map[string]types.Container,
	adjacency map[string][]string,
	normalizedMap map[types.Container]string,
) error {
	if len(sorted) != len(containers) {
		// Identify which containers were not processed (part of cycles)
		processed := make(map[string]bool)

		for _, c := range sorted {
			normalizedIdentifier := normalizedMap[c]
			processed[normalizedIdentifier] = true
		}

		var unprocessed []string

		for id := range containerMap {
			if !processed[id] {
				unprocessed = append(unprocessed, id)
			}
		}
		// Sort for deterministic error reporting
		sort.Strings(unprocessed)

		// Find and report cycle details for the first unprocessed container
		cycleContainer := ""

		var cyclePath []string

		if len(unprocessed) > 0 {
			cycleContainer = unprocessed[0]
			// Use DFS to find the actual cycle path starting from this container
			visited := make(map[string]bool)
			cyclePath = findCyclePath(cycleContainer, adjacency, visited)
		}

		logrus.WithFields(logrus.Fields{
			"cycle_container": cycleContainer,
			"cycle_path":      cyclePath,
		}).Debug("Detected circular reference in dependency graph")

		// Return detailed error with cycle information for debugging
		return CircularReferenceError{ContainerName: cycleContainer, CyclePath: cyclePath}
	}

	return nil
}

// findCyclePath performs DFS to find a cycle path starting from the given node.
//
// This function implements cycle detection using Depth-First Search with three states:
// - visited: nodes that have been fully explored (no cycles through them)
// - visiting: nodes currently in the recursion stack (potential cycle)
//
// When a node is encountered that is already in 'visiting' state, a cycle is detected.
// The path from the current node back to the start of the cycle is returned.
//
// Algorithm:
// 1. Start DFS from given node, tracking current path
// 2. Mark node as visiting when entering
// 3. Recurse on neighbors
// 4. If neighbor is visiting, extract cycle path from current path
// 5. Mark node as visited when backtracking
//
// Time Complexity: O(V + E) in worst case, but typically much faster for cycle detection
// Space Complexity: O(V) for recursion stack and maps
//
// Parameters:
//   - start: Container identifier to start cycle detection from
//   - adjacency: Graph adjacency list (container -> list of containers that depend on it)
//   - visited: Map tracking fully explored nodes (modified in place)
//
// Returns:
//   - []string: Cycle path if found (empty slice if no cycle)
//     Path includes the starting node at both ends to show the cycle
func findCyclePath(start string, adjacency map[string][]string, visited map[string]bool) []string {
	visiting := make(map[string]bool) // Nodes currently in recursion stack

	// Recursive DFS function to detect cycles
	var dfs func(string, []string) []string

	dfs = func(node string, path []string) []string {
		// If node is already in visiting set, we found a back edge (cycle)
		if visiting[node] {
			// Extract the cycle path: from first occurrence of node to current position
			idx := -1

			for i, p := range path {
				if p == node {
					idx = i

					break
				}
			}

			if idx >= 0 {
				// Return path segment that forms the cycle, including node at both ends
				return append(path[idx:], node)
			}

			return nil
		}

		// If already fully visited, no cycle through this path
		if visited[node] {
			return nil
		}

		// Mark as visiting and add to current path
		visiting[node] = true
		path = append(path, node)

		// Explore all neighbors (containers that depend on this one)
		for _, neighbor := range adjacency[node] {
			if cycle := dfs(neighbor, path); cycle != nil {
				return cycle // Propagate cycle up the call stack
			}
		}

		// Backtrack: remove from path and visiting set, mark as fully visited
		visiting[node] = false
		visited[node] = true

		return nil // No cycle found from this path
	}

	return dfs(start, []string{})
}
