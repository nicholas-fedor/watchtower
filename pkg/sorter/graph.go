package sorter

import (
	"fmt"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// DependencyGraph is the dependency graph of a set of containers, with every
// container's links resolved once when the graph is built.
//
// Links are matched to containers as SortByDependencies matches them,
// including Compose service names, replica names, container-name aliases, and
// Docker IDs. Sorting a subset of the containers keeps only the dependencies
// between the containers in the subset, so a link always names the same
// container whichever subset is sorted, and a subset of containers outside
// every cycle always sorts.
//
// A graph reflects the labels and host config of its containers when it is
// built. Build a new graph after they change.
type DependencyGraph struct {
	// containers lists the containers of the graph in the order given.
	containers []types.Container
	// identifiers maps each container to its normalized dependency identifier.
	identifiers map[types.Container]string
	// dependencies lists, for each container, the identifiers its links
	// resolve to, in link order. A link to the container itself is left out.
	dependencies map[types.Container][]string
}

// NewDependencyGraph builds the dependency graph of containers.
//
// Watchtower containers are set aside, as SortByDependencies sets them aside:
// they are not part of the graph and links never resolve to them.
//
// Parameters:
//   - log: Process logger.
//   - containers: Containers to build the graph of.
//   - useComposeDependsOn: Whether to include Compose depends_on labels as links.
//
// Returns:
//   - *DependencyGraph: The graph.
//   - error: An IdentifierCollisionError when containers share an identifier.
func NewDependencyGraph(
	log *zerolog.Logger,
	containers []types.Container,
	useComposeDependsOn bool,
) (*DependencyGraph, error) {
	ordered := make([]types.Container, 0, len(containers))

	for _, c := range containers {
		if !c.IsWatchtower() {
			ordered = append(ordered, c)
		}
	}

	return newDependencyGraph(log, ordered, useComposeDependsOn)
}

// newDependencyGraph builds the dependency graph of every given container,
// including Watchtower containers.
//
// Parameters:
//   - log: Process logger.
//   - containers: Containers to build the graph of.
//   - useComposeDependsOn: Whether to include Compose depends_on labels as links.
//
// Returns:
//   - *DependencyGraph: The graph.
//   - error: An IdentifierCollisionError when containers share an identifier.
func newDependencyGraph(
	log *zerolog.Logger,
	containers []types.Container,
	useComposeDependsOn bool,
) (*DependencyGraph, error) {
	containerMap, err := identifierIndex(log, containers)
	if err != nil {
		return nil, err
	}

	identifiers := make(map[types.Container]string, len(containerMap))
	for identifier, c := range containerMap {
		identifiers[c] = identifier
	}

	// Lookup identifiers for link resolution: canonical keys plus unique bare names.
	// Docs specify Watchtower depends-on and network_mode targets use container names,
	// while Compose depends_on uses service names. Aliases bridge those forms to the
	// canonical project-service graph keys without inventing extra Kahn nodes.
	matchIDSet, aliasToCanonical := buildLinkMatchIndexes(log, containerMap)

	dependencies := make(map[types.Container][]string, len(containers))

	for _, c := range containers {
		self := identifiers[c]

		for _, normalizedLink := range c.Links(useComposeDependsOn) {
			for _, key := range resolveLinkToCanonicalKeys(normalizedLink, matchIDSet, aliasToCanonical) {
				if key == self {
					// Self-reference: skip so the container stays indegree 0 for this link.
					continue
				}

				dependencies[c] = append(dependencies[c], key)
			}
		}
	}

	return &DependencyGraph{
		containers:   containers,
		identifiers:  identifiers,
		dependencies: dependencies,
	}, nil
}

// CycleMembers returns the containers that are part of a circular dependency.
//
// Every member of every cycle is reported, in the order the containers were
// given to the graph. A container that only depends on a cycle member is not
// part of the cycle, and a container that lists itself is a self-dependency
// rather than a cycle, so neither is reported.
//
// Returns:
//   - []types.Container: Containers in a cycle.
func (g *DependencyGraph) CycleMembers() []types.Container {
	edges := make(map[string][]string, len(g.containers))
	for _, c := range g.containers {
		edges[g.identifiers[c]] = g.dependencies[c]
	}

	finder := &cycleFinder{
		adjacency: edges,
		index:     make(map[string]int, len(g.containers)),
		lowLink:   make(map[string]int, len(g.containers)),
		onStack:   make(map[string]bool, len(g.containers)),
		stack:     make([]string, 0, len(g.containers)),
		inCycle:   make(map[string]bool),
	}

	// Start from the containers in the order given so the search is deterministic.
	for _, c := range g.containers {
		if _, visited := finder.index[g.identifiers[c]]; !visited {
			finder.visit(g.identifiers[c])
		}
	}

	members := make([]types.Container, 0, len(finder.inCycle))

	for _, c := range g.containers {
		if finder.inCycle[g.identifiers[c]] {
			members = append(members, c)
		}
	}

	return members
}

// Sort sorts containers in place by the dependencies between them, with
// Watchtower containers last.
//
// Only dependencies between the given containers affect the order. Ties are
// broken as SortByDependencies breaks them, so sorting every container of the
// graph gives the same order as SortByDependencies.
//
// Parameters:
//   - log: Process logger.
//   - containers: Slice to sort in place. Every container other than a
//     Watchtower container must be in the graph.
//
// Returns:
//   - error: A CircularReferenceError when the containers depend on each other
//     in a cycle, or ErrNotInGraph for a container outside the graph.
func (g *DependencyGraph) Sort(log *zerolog.Logger, containers []types.Container) error {
	ordered := make([]types.Container, 0, len(containers))
	watchtowers := make([]types.Container, 0, 1)

	for _, c := range containers {
		if c.IsWatchtower() {
			watchtowers = append(watchtowers, c)
		} else {
			ordered = append(ordered, c)
		}
	}

	containerMap, indegree, adjacency, normalizedMap, err := g.subgraph(ordered)
	if err != nil {
		return err
	}

	sorted, err := kahnSort(log, ordered, containerMap, indegree, adjacency, normalizedMap)
	if err != nil {
		return err
	}

	copy(containers, sorted)
	copy(containers[len(sorted):], watchtowers)

	return nil
}

// subgraph returns the part of the graph between containers, in the form
// kahnSort takes. Edges are added in the order of containers and of their
// links, so ties break as they do when the graph of containers alone is built.
//
// Parameters:
//   - containers: Containers of the graph.
//
// Returns:
//   - map[string]types.Container: Container for each identifier.
//   - map[string]int: Number of dependencies of each identifier.
//   - map[string][]string: Dependents of each identifier.
//   - map[types.Container]string: Identifier of each container.
//   - error: ErrNotInGraph for a container outside the graph, or an
//     IdentifierCollisionError for a container given twice.
func (g *DependencyGraph) subgraph(containers []types.Container) (
	map[string]types.Container, map[string]int, map[string][]string, map[types.Container]string, error,
) {
	containerMap := make(map[string]types.Container, len(containers))
	indegree := make(map[string]int, len(containers))
	adjacency := make(map[string][]string)
	normalizedMap := make(map[types.Container]string, len(containers))

	for _, c := range containers {
		identifier, ok := g.identifiers[c]
		if !ok {
			return nil, nil, nil, nil, fmt.Errorf("%w: %s", ErrNotInGraph, c.Name())
		}

		if existing, duplicate := containerMap[identifier]; duplicate {
			return nil, nil, nil, nil, IdentifierCollisionError{
				DuplicateIdentifier: identifier,
				AffectedContainers:  []types.Container{existing, c},
			}
		}

		containerMap[identifier] = c
		indegree[identifier] = 0
		normalizedMap[c] = identifier
	}

	for _, c := range containers {
		identifier := normalizedMap[c]

		for _, key := range g.dependencies[c] {
			if _, isNode := containerMap[key]; !isNode {
				// The link names a container outside the ones being sorted.
				continue
			}

			indegree[identifier]++
			adjacency[key] = append(adjacency[key], identifier)
		}
	}

	return containerMap, indegree, adjacency, normalizedMap, nil
}
