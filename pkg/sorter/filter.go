package sorter

import (
	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// CycleDetector detects cycles in dependency graph.
type CycleDetector struct {
	graph  map[string][]string
	colors map[string]int // 0: white, 1: gray, 2: black
	cycles map[string]bool
	path   []string
}

// dfs performs DFS and detects cycles.
func (cd *CycleDetector) dfs(node string) {
	cd.colors[node] = 1 // gray

	cd.path = append(cd.path, node)
	for _, neighbor := range cd.graph[node] {
		if cd.colors[neighbor] == 0 {
			cd.dfs(neighbor)
		} else if cd.colors[neighbor] == 1 {
			// cycle detected, mark all nodes in cycle
			idx := -1

			for i, n := range cd.path {
				if n == neighbor {
					idx = i

					break
				}
			}

			if idx >= 0 {
				for i := idx; i < len(cd.path); i++ {
					cd.cycles[cd.path[i]] = true
				}
			}
		}
	}

	cd.path = cd.path[:len(cd.path)-1]
	cd.colors[node] = 2 // black
}

// DetectCycles identifies all containers involved in circular dependencies.
func DetectCycles(containers []types.Container) map[string]bool {
	cycleDetector := &CycleDetector{
		graph:  make(map[string][]string),
		colors: make(map[string]int),
		cycles: make(map[string]bool),
		path:   []string{},
	}

	// Build graph
	for _, c := range containers {
		name := GetContainerIdentifier(c)
		links := c.Links()

		normalizedLinks := make([]string, len(links))
		for i, link := range links {
			normalizedLinks[i] = util.NormalizeContainerName(link)
		}

		cycleDetector.graph[name] = normalizedLinks
		cycleDetector.colors[name] = 0
	}

	// Run DFS from each node
	for name := range cycleDetector.graph {
		if cycleDetector.colors[name] == 0 {
			cycleDetector.dfs(name)
		}
	}

	return cycleDetector.cycles
}
