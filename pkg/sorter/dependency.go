package sorter

import (
	"sort"
	"strconv"
	"strings"

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
//   - Dependent containers are updated after their dependencies
//   - Watchtower containers are processed last to maintain monitoring capability
//   - Circular dependencies are detected and reported as errors
//
// Time Complexity: O(V + E) where V is containers and E is dependency links
// Space Complexity: O(V + E) for graph structures
//
// Parameters:
//   - containers: Slice to sort in place. Modified directly.
//   - useComposeDependsOn: Whether to include Docker Compose depends_on label in dependency resolution.
//
// Returns:
//   - error: Non-nil if circular reference detected, nil on success.
//     Error includes the container name and cycle path for debugging.
func (ds DependencySorter) Sort(containers []types.Container, useComposeDependsOn bool) error {
	if containers == nil {
		return nil
	}

	logrus.WithField("container_count", len(containers)).Debug("Starting dependency sort")

	// Separate Watchtower containers from non-Watchtower containers
	var (
		nonWatchtowerContainers []types.Container
		watchtowerContainers    []types.Container
	)

	for _, c := range containers {
		if c.IsWatchtower() {
			watchtowerContainers = append(watchtowerContainers, c)
		} else {
			nonWatchtowerContainers = append(nonWatchtowerContainers, c)
		}
	}

	logrus.WithFields(logrus.Fields{
		"non_watchtower_count": len(nonWatchtowerContainers),
		"watchtower_count":     len(watchtowerContainers),
	}).Debug("Separated containers by Watchtower status")

	// Sort non-Watchtower containers by dependencies using internal sorter
	sortedNonWatchtower, err := sortByDependencies(nonWatchtowerContainers, useComposeDependsOn)
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
//   - Empty container list: returns empty list
//   - Single container: returns that container
//   - Circular dependencies: detected and reported with cycle path
//   - Containers with no dependencies: processed first
//   - Missing dependency targets: ignored (only considers existing containers)
//
// Parameters:
//   - containers: List to sort. Should not be nil.
//
// Returns:
//   - []types.Container: Sorted list in dependency order (dependencies first).
//   - error: Non-nil if circular reference detected, nil on success.
//     Error includes container name and cycle path for debugging.
func sortByDependencies(containers []types.Container, useComposeDependsOn bool) ([]types.Container, error) {
	// Phase 1: Build the dependency graph data structures
	containerMap, indegree, adjacency, normalizedMap, err := buildDependencyGraph(containers, useComposeDependsOn)
	if err != nil {
		return nil, err
	}

	// Phase 2: Initialize processing queue with containers that have no dependencies
	queue := initializeQueue(indegree)

	// Phase 3: Process the queue using Kahn's algorithm
	// While there are containers with no remaining dependencies:
	//   - Remove a container from queue and add it to sorted result
	//   - For each container that depends on it, decrement their indegree
	//   - If a dependent's indegree becomes 0, add it to queue
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
	err = detectAndReportCycle(
		sorted,
		containers,
		containerMap,
		adjacency,
		normalizedMap,
	)
	if err != nil {
		return nil, err
	}

	return sorted, nil
}

// buildDependencyGraph constructs the dependency graph data structures for topological sorting.
//
// This function builds three key data structures:
//   - containerMap: Maps canonical normalized identifiers to container objects for O(1) lookup
//   - indegree: Tracks number of incoming dependencies for each container (nodes with indegree 0 have no dependencies)
//   - adjacency: Lists containers that depend on each container (outgoing edges; keys are canonical)
//
// Graph nodes are always keyed by ResolveContainerIdentifier (project-service, service, or name).
// Links from c.Links() may use Watchtower depends-on container names, Compose service names,
// Docker links, or network_mode targets (including explicit container_name values). Those
// strings often differ from the canonical graph key when Compose labels are present.
//
// Link Matching Strategy:
// Matching uses FindMatchingIdentifiers against the canonical keys plus unique bare Name()
// aliases, then maps every hit back to a canonical key before recording edges. Strategies:
//  1. Exact match on canonical identifier or bare container name
//  2. Prefix match for Docker Compose replica suffixes (e.g., "db" matches "db-1", "db-2")
//  3. Service-only match via ExtractServiceName on both sides (e.g., "abs-wireguard" matches
//     "media-abs-wireguard"), only when exactly one candidate is unambiguous
//
// Parameters:
//   - containers: List of containers to build graph for.
//   - useComposeDependsOn: Whether Links() should include Compose depends_on labels.
//
// Returns:
//   - map[string]types.Container: Container lookup map (canonical keys only)
//   - map[string]int: Indegree count for each container
//   - map[string][]string: Adjacency list (canonical dependency -> dependents)
//   - map[types.Container]string: Reverse map from container to normalized identifier
//   - error: IdentifierCollisionError if duplicate identifiers detected, nil otherwise
func buildDependencyGraph(
	containers []types.Container,
	useComposeDependsOn bool,
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
	for identifier, dupContainers := range tempMap {
		if len(dupContainers) > 1 {
			logrus.Errorf(
				"Identifier collision detected: '%s' used by multiple containers: %v",
				identifier,
				dupContainers,
			)

			return nil, nil, nil, nil, IdentifierCollisionError{
				DuplicateIdentifier: identifier,
				AffectedContainers:  dupContainers,
			}
		}
		// No collision, populate maps
		containerMap[identifier] = dupContainers[0]
		indegree[identifier] = 0
		normalizedMap[dupContainers[0]] = identifier
	}

	// Lookup identifiers for link resolution: canonical keys plus unique bare names.
	// Docs specify Watchtower depends-on and network_mode targets use container names,
	// while Compose depends_on uses service names; aliases bridge those forms to the
	// canonical project-service graph keys without inventing extra Kahn nodes.
	matchIdentifiers, aliasToCanonical := buildLinkMatchIndexes(containerMap)

	// Build the graph by processing container links (dependencies).
	// Edges always use canonical identifiers so Kahn's algorithm can traverse them.
	for _, c := range containers {
		normalizedIdentifier := normalizedMap[c]
		// c.Links() already returns normalized container names
		for _, normalizedLink := range c.Links(useComposeDependsOn) {
			matchedKeys := resolveLinkToCanonicalKeys(
				normalizedLink,
				matchIdentifiers,
				aliasToCanonical,
			)

			for _, key := range matchedKeys {
				if key == normalizedIdentifier {
					// Self-reference: skip so the container stays indegree 0 for this link.
					continue
				}

				indegree[normalizedIdentifier]++
				adjacency[key] = append(adjacency[key], normalizedIdentifier)
			}
		}
	}

	return containerMap, indegree, adjacency, normalizedMap, nil
}

// buildLinkMatchIndexes builds the identifier list and alias→canonical map used when
// resolving dependency links against graph nodes.
//
// Each canonical ResolveContainerIdentifier is always included and never overwritten.
// Bare container names are registered as aliases only when they uniquely identify one
// container and do not collide with another container's canonical key, so ambiguous
// names do not create non-deterministic edges.
//
// Parameters:
//   - containerMap: Canonical identifier → container map from graph construction.
//
// Returns:
//   - []string: Identifiers passed to FindMatchingIdentifiers (canonical keys and unique bare names).
//   - map[string]string: Maps every matchable identifier to its canonical graph key.
func buildLinkMatchIndexes(
	containerMap map[string]types.Container,
) ([]string, map[string]string) {
	// Capacity covers one canonical key plus one optional bare-name alias per container.
	const aliasCapacityFactor = 2

	aliasToCanonical := make(map[string]string, len(containerMap)*aliasCapacityFactor)

	// Canonical ResolveContainerIdentifier keys always map to themselves and must never
	// be removed or overwritten by bare-name alias cleanup.
	for identifier := range containerMap {
		aliasToCanonical[identifier] = identifier
	}

	// bareOwners collects distinct canonical owners for each bare container name.
	// A bare name becomes an alias only when exactly one owner claims it and the name
	// does not collide with a different container's canonical graph key.
	bareOwners := make(map[string]map[string]struct{})

	for identifier, c := range containerMap {
		bareName := util.NormalizeContainerName(c.Name())
		if bareName == "" || bareName == identifier {
			continue
		}

		// Never use a bare name that is already another container's canonical key.
		if _, isCanonicalKey := containerMap[bareName]; isCanonicalKey {
			continue
		}

		owners, ok := bareOwners[bareName]
		if !ok {
			owners = make(map[string]struct{})
			bareOwners[bareName] = owners
		}

		owners[identifier] = struct{}{}
	}

	for bareName, owners := range bareOwners {
		if len(owners) != 1 {
			logrus.WithField("bare_name", bareName).
				Debug("Skipped ambiguous bare container name alias for dependency matching")

			continue
		}

		for owner := range owners {
			aliasToCanonical[bareName] = owner
		}
	}

	matchIdentifiers := make([]string, 0, len(aliasToCanonical))
	for id := range aliasToCanonical {
		matchIdentifiers = append(matchIdentifiers, id)
	}

	sort.Strings(matchIdentifiers)

	return matchIdentifiers, aliasToCanonical
}

// resolveLinkToCanonicalKeys resolves a single dependency link to sorted unique canonical
// graph keys using FindMatchingIdentifiers and the alias index.
//
// Parameters:
//   - link: Normalized link from Container.Links().
//   - matchIdentifiers: Identifiers available for matching (canonical + unique bare names).
//   - aliasToCanonical: Maps match hits to canonical graph keys.
//
// Returns:
//   - []string: Sorted unique canonical identifiers the link refers to (empty if none).
func resolveLinkToCanonicalKeys(
	link string,
	matchIdentifiers []string,
	aliasToCanonical map[string]string,
) []string {
	if link == "" {
		return nil
	}

	matches := FindMatchingIdentifiers(link, matchIdentifiers)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(matches))
	canonicalKeys := make([]string, 0, len(matches))

	for _, match := range matches {
		canonical, ok := aliasToCanonical[match]
		if !ok || canonical == "" || seen[canonical] {
			continue
		}

		seen[canonical] = true
		canonicalKeys = append(canonicalKeys, canonical)
	}

	sort.Strings(canonicalKeys)

	return canonicalKeys
}

// IsPositiveInteger checks if a string represents a positive integer (1 or greater).
//
// This validation is critical for distinguishing Docker Compose-style replica suffixes
// (e.g., "db-1", "db-2") from other hyphenated container names (e.g., "db-backup",
// "db-temp"). Docker Compose uses sequential positive integers starting from 1 to
// identify replica instances. By requiring a positive integer suffix, we ensure that:
//
//   - "db" correctly matches "db-1" and "db-2" as replicas
//   - "db" does NOT match "dbase" (no hyphen) or "db-backup" (non-numeric suffix)
//   - "database" does NOT match "database2" (no separator)
//
// This prevents false dependency relationships between unrelated containers with
// similar names, which could cause incorrect update ordering or circular dependencies.
func IsPositiveInteger(s string) bool {
	if s == "" {
		return false
	}

	n, err := strconv.Atoi(s)

	return err == nil && n > 0
}

// ExtractServiceName extracts the service name from a container identifier.
//
// Container identifiers from ResolveContainerIdentifier() follow the pattern:
//   - "project-service" when both project and service labels exist
//   - "project-service-N" for Docker Compose replicas (N is replica number)
//   - "servicename" when only service name is available (no project context)
//
// This function extracts just the service name by:
//  1. If there's no hyphen, return the whole string (it's already just a service name)
//  2. If there are hyphens, the service name is the last segment (or last two segments
//     if the last segment is a replica number)
//
// Examples:
//   - "postgresql-postgres" -> "postgres"
//   - "postgresql-postgres-1" -> "postgres" (strips replica suffix)
//   - "myapp" -> "myapp"
//   - "my-app-service" -> "service" (last segment before any replica number)
//   - "my-app-service-2" -> "service" (strips replica suffix)
func ExtractServiceName(identifier string) string {
	if identifier == "" {
		return ""
	}

	// If no hyphen, the identifier is already just a service name
	if !strings.Contains(identifier, "-") {
		return identifier
	}

	parts := strings.Split(identifier, "-")

	// Check if the last part is a replica number (positive integer)
	// If so, we need to skip it and take the second-to-last part as service name
	if IsPositiveInteger(parts[len(parts)-1]) {
		// Return the part before the replica number (e.g., "service" from "project-service-1")
		return parts[len(parts)-2]
	}

	// No replica number, service name is the last part
	return parts[len(parts)-1]
}

// FindMatchingIdentifiers returns the identifiers from the given list that match
// the provided link. It applies the same strategies used when building the
// dependency graph:
//
//  1. Exact match.
//  2. Replica prefix match: the identifier starts with "<link>-" and the suffix
//     after the hyphen is a positive integer (Docker Compose replica numbering).
//  3. Service-only / project-qualified match: ExtractServiceName equality on both
//     sides, or identifier ends with "-"+link (full service name under a project
//     prefix). This strategy only succeeds when exactly one candidate matches;
//     multiple matches (e.g. the same service name in different projects) are
//     treated as ambiguous and return no results.
//
// Parameters:
//   - link: Dependency link to resolve (typically from Container.Links()).
//   - identifiers: List of known container identifiers to search within.
//
// Returns:
//   - []string: Matching identifiers. Returns nil or an empty slice when there
//     is no match or when the service-only strategy finds multiple candidates.
func FindMatchingIdentifiers(link string, identifiers []string) []string {
	if link == "" || len(identifiers) == 0 {
		return nil
	}

	// Build a temporary lookup for efficiency
	idSet := make(map[string]bool, len(identifiers))
	for _, id := range identifiers {
		idSet[id] = true
	}

	var matches []string

	// 1. Exact match
	if idSet[link] {
		matches = append(matches, link)

		return matches
	}

	// 2. Replica prefix match (only if suffix is positive integer)
	var replicaMatches []string

	for id := range idSet {
		if strings.HasPrefix(id, link+"-") {
			suffix := id[len(link)+1:]
			if IsPositiveInteger(suffix) {
				replicaMatches = append(replicaMatches, id)
			}
		}
	}

	if len(replicaMatches) > 0 {
		sort.Strings(replicaMatches)
		matches = append(matches, replicaMatches...)

		return matches
	}

	// 3. Service-only / project-qualified match.
	// Accept when exactly one candidate matches via either:
	//   - ExtractServiceName equality on both sides (handles project-service keys
	//     when the link is a bare service name, including multi-segment names
	//     once both sides reduce to the same trailing segment), or
	//   - the identifier ends with "-"+link (link is the full service name under
	//     a project prefix, e.g. link "abs-wireguard" → "media-abs-wireguard").
	// Multiple matches are ambiguous (different projects) and are discarded.
	var serviceMatches []string

	normalizedLink := ExtractServiceName(link)
	for id := range idSet {
		if ExtractServiceName(id) == normalizedLink ||
			(link != "" && strings.HasSuffix(id, "-"+link)) {
			serviceMatches = append(serviceMatches, id)
		}
	}

	if len(serviceMatches) == 1 {
		matches = append(matches, serviceMatches[0])
	}

	return matches
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
//   - visited: nodes that have been fully explored (no cycles through them)
//   - visiting: nodes currently in the recursion stack (potential cycle)
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
