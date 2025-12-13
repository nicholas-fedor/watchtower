package container

import (
	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// CycleDetector implements cycle detection in container dependency graphs using the three-color DFS algorithm.
//
// The three-color DFS algorithm is a graph traversal technique that detects cycles by maintaining
// three states for each node during depth-first search:
//
//  1. White (0): Node has not been visited yet. Represents unexplored territory in the graph.
//  2. Gray (1): Node is currently being visited (in the current DFS path). If we encounter a gray
//     node while traversing, it indicates a back edge and thus a cycle.
//  3. Black (2): Node has been fully explored, including all its descendants. No cycles can exist
//     through this node in future traversals.
//
// Cycle Detection Logic:
// - When DFS visits a node, it marks it gray and adds it to the current path.
// - For each neighbor:
//   - If white: Recurse on it (continue exploration)
//   - If gray: Cycle detected! Mark all nodes from the gray node to current node in the path as cyclic
//   - If black: Already fully explored, no cycle through this edge
//
// - After exploring all neighbors, mark node black and remove from path (backtrack)
//
// This algorithm efficiently detects all nodes involved in cycles in a single pass, with
// Time Complexity: O(V + E) where V = vertices (containers), E = edges (dependencies)
// Space Complexity: O(V) for color map, path stack, and recursion depth
//
// Data Structures:
// - graph: Adjacency list mapping container names to their dependency lists (outgoing edges)
// - colors: State map for three-color algorithm (0=white, 1=gray, 2=black)
// - cycles: Result map marking containers involved in cycles (true = cyclic)
// - path: Current DFS path for cycle reconstruction when back edges are found.
type CycleDetector struct {
	graph  map[string][]string
	colors map[string]int // 0: white, 1: gray, 2: black
	cycles map[string]bool
	path   []string
}

// dfs performs depth-first search to detect cycles in the dependency graph using the three-color algorithm.
//
// This method implements the core cycle detection logic by traversing the graph depth-first while
// maintaining the current path and node states. When a back edge to a gray (visiting) node is found,
// all nodes in the cycle (from the gray node to the current node in the path) are marked as cyclic.
//
// Algorithm Steps:
//  1. Mark current node as gray (visiting) and add to current path
//  2. For each dependency (neighbor) of current node:
//     a. If neighbor is white (unvisited): Recurse on neighbor to continue exploration
//     b. If neighbor is gray (visiting): CYCLE DETECTED!
//     - Find the position of neighbor in current path (start of cycle)
//     - Mark all nodes from cycle start to current node as involved in cycle
//     c. If neighbor is black (visited): Already fully explored, no cycle through this edge
//  3. After exploring all neighbors: Mark node black (fully visited) and remove from path (backtrack)
//
// Cycle Reconstruction:
// When a cycle is detected (back edge to gray node), the cycle consists of all nodes in the
// current path from the first occurrence of the gray node to the current node. This ensures
// all nodes participating in the cycle are correctly identified, even in complex multi-node cycles.
//
// Time Complexity: O(V + E) amortized across all DFS calls
// Space Complexity: O(V) for recursion stack in worst case (linear graph)
//
// Parameters:
//   - node: The container name to start DFS traversal from. Must exist in the graph.
func (cd *CycleDetector) dfs(node string) {
	// Step 1: Mark node as visiting (gray) and add to current exploration path
	// This indicates we're actively exploring this node's dependencies
	cd.colors[node] = 1 // gray

	cd.path = append(cd.path, node)

	// Step 2: Explore all dependencies (neighbors) of this node
	for _, neighbor := range cd.graph[node] {
		if cd.colors[neighbor] == 0 {
			// Neighbor is unvisited (white): Continue DFS exploration from this neighbor
			// This recursively explores the dependency chain
			cd.dfs(neighbor)
		} else if cd.colors[neighbor] == 1 {
			// CYCLE DETECTED: Neighbor is currently being visited (gray)
			// This indicates a back edge in the DFS tree, meaning a cycle exists
			// All nodes from the first occurrence of neighbor in path to current node form the cycle

			// Find the starting index of the cycle in the current path
			idx := -1

			for i, n := range cd.path {
				if n == neighbor {
					idx = i

					break
				}
			}

			// Mark all nodes in the cycle path as cyclic
			// This includes the neighbor (cycle start) through the current node
			if idx >= 0 {
				for i := idx; i < len(cd.path); i++ {
					cd.cycles[cd.path[i]] = true
				}
			}
		}
		// If neighbor is black (2): Already fully explored, no cycle through this edge
	}

	// Step 3: Backtrack - remove node from current path and mark as fully visited (black)
	// This node and all its descendants have been fully explored
	cd.path = cd.path[:len(cd.path)-1]
	cd.colors[node] = 2 // black
}

// DetectCycles identifies all containers involved in circular dependencies using three-color DFS.
//
// This function constructs a directed dependency graph from container relationships and uses
// depth-first search with node coloring to detect all cycles. The algorithm ensures that
// every connected component of the graph is explored, identifying all containers that are
// part of any circular dependency chain.
//
// Graph Construction:
// 1. Each container becomes a node identified by its resolved container identifier
// 2. Dependencies (links) become directed edges from dependent to dependency
// 3. Container names are normalized to handle Docker Compose service name variations
// 4. Only containers present in the input list are included (missing dependencies ignored)
//
// DFS Traversal Strategy:
// - Start DFS from each unvisited (white) node to ensure all components are explored
// - The three-color algorithm detects cycles during traversal and marks all involved nodes
// - Multiple DFS calls are needed because the graph may have disconnected components
//
// Implementation Rationale:
// - Uses adjacency list representation for efficient neighbor iteration (O(1) per edge)
// - Normalization ensures consistent handling of Docker Compose vs container names
// - Returns all cyclic nodes rather than just detecting presence, enabling targeted handling
// - Single pass through graph with O(V + E) complexity makes it suitable for large deployments
//
// Time Complexity: O(V + E) where V = containers, E = dependency links
// Space Complexity: O(V + E) for graph storage and DFS recursion stack
//
// Edge Cases:
// - Empty container list: Returns empty map
// - No dependencies: Returns empty map (no cycles possible)
// - Single container with self-dependency: Detected as cycle
// - Multiple disconnected cycles: All detected and marked
// - Missing dependency targets: Ignored (only analyze provided containers)
//
// Parameters:
//   - containers: List of containers to analyze for cycles. Should not be nil.
//
// Returns:
//   - map[string]bool: Map where keys are container names and values indicate cycle involvement.
//     true = container is part of at least one cycle, false/absent = no cycles detected.
//     Empty map returned if no cycles found.
func DetectCycles(containers []types.Container) map[string]bool {
	// Initialize cycle detector with empty data structures
	// All containers start as white (unvisited) and no cycles detected yet
	cycleDetector := &CycleDetector{
		graph:  make(map[string][]string), // Adjacency list: container -> its dependencies
		colors: make(map[string]int),      // Node states: 0=white, 1=gray, 2=black
		cycles: make(map[string]bool),     // Result: containers involved in cycles
		path:   []string{},                // Current DFS path for cycle reconstruction
	}

	// Phase 1: Build the dependency graph from container relationships
	// Convert container objects into a directed graph where edges represent dependencies
	for _, c := range containers {
		// Use resolved identifier for consistent naming (handles Docker naming variations)
		name := ResolveContainerIdentifier(c)

		// Get all dependencies (links) for this container
		links := c.Links()

		// Normalize dependency names to handle service names vs container names consistently
		// This ensures "web" and "/web" are treated as the same container
		normalizedLinks := make([]string, len(links))
		for i, link := range links {
			normalizedLinks[i] = util.NormalizeContainerName(link)
		}

		// Add container to graph with its normalized dependencies
		cycleDetector.graph[name] = normalizedLinks

		// Initialize container as unvisited (white) in the coloring algorithm
		cycleDetector.colors[name] = 0
	}

	// Filter out unknown dependencies: only keep neighbors that are present in the containers list
	// This prevents DFS from recursing into containers not in the original input
	for name, neighbors := range cycleDetector.graph {
		filtered := make([]string, 0, len(neighbors))
		for _, neighbor := range neighbors {
			if _, exists := cycleDetector.colors[neighbor]; exists {
				filtered = append(filtered, neighbor)
			}
		}
		cycleDetector.graph[name] = filtered
	}

	// Phase 2: Run DFS from each unvisited node to ensure complete graph coverage
	// Multiple DFS calls needed because graph may have disconnected components
	// Each DFS traversal detects cycles within its reachable component
	for name := range cycleDetector.graph {
		if cycleDetector.colors[name] == 0 { // Only start from unvisited nodes
			cycleDetector.dfs(name) // DFS will mark all reachable nodes and detect cycles
		}
	}

	// Return map of all containers found to be involved in cycles
	// Empty map if no cycles detected, populated map with cyclic containers otherwise
	return cycleDetector.cycles
}
