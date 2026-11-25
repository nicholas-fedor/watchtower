package sorter

import (
	"fmt"
	"slices"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container"
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
// Watchtower containers are not part of the graph and are always sorted last,
// but the graph still records which containers depend on them, and which
// containers they depend on, for Dependents. A link that names an unmonitored
// container exactly resolves to nothing, rather than to a different container
// with a similar name.
//
// A service name shared by several Compose projects names the service of the
// linking container's own project when it has one, and every monitored one of
// them otherwise. When the service of its own project is unmonitored, the link
// resolves to nothing.
//
// A graph reflects the labels and host config of its containers when it is
// built. Build a new graph after they change.
type DependencyGraph struct {
	// containers lists the containers of the graph in the order given.
	containers []*container.Container
	// identifiers maps each container to its normalized dependency identifier.
	identifiers map[*container.Container]string
	// dependencies lists, for each container, the identifiers its links
	// resolve to, in link order. A link to the container itself is left out.
	dependencies map[*container.Container][]string
	// dependents lists, for each container, the containers whose links name
	// it: members in the order given, then Watchtower containers.
	dependents map[*container.Container][]*container.Container
	// unresolved lists the depends-on label entries that name no container.
	unresolved []UnresolvedLink
	// selfDependent lists the containers with a link that names the container
	// itself. Members come first in the order given, then Watchtower containers.
	selfDependent []*container.Container
}

// UnresolvedLink is a Watchtower depends-on label entry that names no
// container.
type UnresolvedLink struct {
	// Container is the container whose label holds the entry.
	Container *container.Container
	// Link is the entry, normalized as the dependency resolution reads it.
	Link string
}

// NewDependencyGraph builds the dependency graph of containers.
//
// Watchtower containers are set aside, as SortByDependencies sets them aside,
// so links never resolve to them for ordering. Their own links still resolve,
// and a link that names one exactly by name, identifier, or ID, or without its
// replica number when it is the only such Watchtower container and no
// unmonitored container has that name, makes the linking container one of its
// dependents.
//
// Unmonitored containers are not part of the graph. A link that names one
// exactly resolves to nothing, so it is not matched to a monitored container
// instead.
//
// Parameters:
//   - log: Process logger.
//   - containers: Monitored containers to build the graph of.
//   - unmonitored: Other containers that links may name.
//   - useComposeDependsOn: Whether to include Compose depends_on labels as links.
//
// Returns:
//   - *DependencyGraph: The graph.
//   - error: An error naming the identifier and containers when monitored
//     containers other than Watchtower containers share an identifier.
func NewDependencyGraph(
	log *zerolog.Logger,
	containers, unmonitored []*container.Container,
	useComposeDependsOn bool,
) (*DependencyGraph, error) {
	members, watchtowers := splitWatchtowers(containers)

	return newDependencyGraph(
		log,
		members,
		watchtowers,
		unmonitored,
		useComposeDependsOn,
		true,
	)
}

// newDependencyGraph builds the dependency graph of members.
//
// Parameters:
//   - log: Process logger.
//   - members: Containers of the graph.
//   - watchtowers: Monitored Watchtower containers, outside the graph.
//   - unmonitored: Containers that links may name exactly but that are
//     outside the graph.
//   - useComposeDependsOn: Whether to include Compose depends_on labels as links.
//   - logDependencies: Whether to log each container's dependencies. Only the
//     scan's graph logs them, so sorting a subset on its own does not repeat them.
//
// Returns:
//   - *DependencyGraph: The graph.
//   - error: An identifierCollisionError when members share an identifier.
func newDependencyGraph(
	log *zerolog.Logger,
	members, watchtowers, unmonitored []*container.Container,
	useComposeDependsOn, logDependencies bool,
) (*DependencyGraph, error) {
	containerMap, err := identifierIndex(log, members)
	if err != nil {
		return nil, err
	}

	identifiers := make(map[*container.Container]string, len(containerMap))
	for identifier, c := range containerMap {
		identifiers[c] = identifier
	}

	// Lookup identifiers for link resolution: canonical keys plus unique bare names.
	// Docs specify Watchtower depends-on and network_mode targets use container names,
	// while Compose depends_on uses service names. Aliases bridge those forms to the
	// canonical project-service graph keys without inventing extra Kahn nodes.
	matchIDSet, aliasToCanonical := buildLinkMatchIndexes(log, containerMap)

	watchtowerExact := exactNames(watchtowers)

	resolver := &linkResolver{
		matchIDSet:        newMatchSet(matchIDSet),
		aliasToCanonical:  aliasToCanonical,
		projects:          memberProjects(containerMap),
		services:          memberServices(containerMap),
		watchtowers:       watchtowerExact,
		unmonitored:       exactNames(unmonitored),
		unmonitoredNames:  projectNames(unmonitored),
		watchtowerAliases: watchtowerAliases(watchtowers, watchtowerExact),
	}

	graph := &DependencyGraph{
		containers:   members,
		identifiers:  identifiers,
		dependencies: make(map[*container.Container][]string, len(members)),
		dependents:   make(map[*container.Container][]*container.Container, len(members)),
	}

	for _, c := range slices.Concat(members, watchtowers) {
		self, isMember := identifiers[c]

		// Links leaves out an entry naming the container by its own name and
		// reports it. An entry naming it by another form resolves to the
		// container itself below.
		links, namesItself := c.Links(useComposeDependsOn)
		if logDependencies && len(links) > 0 {
			log.Debug().
				Str("container", c.Name()).
				Strs("links", links).
				Msg("Found container dependencies")
		}

		for _, normalizedLink := range links {
			keys, watchtower, _ := resolver.resolve(c, normalizedLink)
			if watchtower == c {
				namesItself = true
			} else if watchtower != nil {
				graph.addDependent(watchtower, c)
			}

			for _, key := range keys {
				if key == self {
					// A link to the container itself adds no edge.
					namesItself = true

					continue
				}

				// Several links can name the same container, such as a Compose
				// depends_on entry and the network mode of the same service.
				if isMember && !slices.Contains(graph.dependencies[c], key) {
					graph.dependencies[c] = append(graph.dependencies[c], key)
				}

				graph.addDependent(containerMap[key], c)
			}
		}

		graph.unresolved = append(graph.unresolved, resolver.unresolvedDependsOn(c, self)...)

		if namesItself {
			graph.selfDependent = append(graph.selfDependent, c)
		}
	}

	return graph, nil
}

// CycleMembers returns the containers that are part of a circular dependency.
//
// Every member of every cycle is reported, in the order the containers were
// given to the graph. A container that only depends on a cycle member is not
// part of the cycle, and a container that lists itself is a self-dependency
// rather than a cycle, so neither is reported. [DependencyGraph.SelfDependencies]
// reports the self-dependencies.
//
// Returns:
//   - []*container.Container: Containers in a cycle.
func (g *DependencyGraph) CycleMembers() []*container.Container {
	edges := make(map[string][]string, len(g.containers))
	for _, c := range g.containers {
		edges[g.identifiers[c]] = g.dependencies[c]
	}

	finder := newCycleFinder(edges)

	// Start from the containers in the order given so the search is deterministic.
	for _, c := range g.containers {
		if _, visited := finder.index[g.identifiers[c]]; !visited {
			finder.visit(g.identifiers[c])
		}
	}

	members := make([]*container.Container, 0, len(finder.inCycle))

	for _, c := range g.containers {
		if finder.inCycle[g.identifiers[c]] {
			members = append(members, c)
		}
	}

	return members
}

// Dependents returns the containers that depend directly on c: members of the
// graph in the order they were given, then Watchtower containers. Watchtower
// containers are included both as c and among the dependents.
//
// Parameters:
//   - c: A monitored container.
//
// Returns:
//   - []*container.Container: The containers whose links name c. Nil when none do
//     or c is not monitored.
func (g *DependencyGraph) Dependents(c *container.Container) []*container.Container {
	return g.dependents[c]
}

// UnresolvedDependsOn returns the Watchtower depends-on label entries that name
// no container. They are left out of the graph.
//
// Returns:
//   - []UnresolvedLink: The entries, in the order the containers were given.
func (g *DependencyGraph) UnresolvedDependsOn() []UnresolvedLink {
	return g.unresolved
}

// SelfDependencies returns the containers with a link that names the container
// itself. The link may use the container's name or another form that resolves
// to it, such as its own Compose service. Such a link is left out of the graph.
//
// Returns:
//   - []*container.Container: The containers, monitored members in the order
//     given, then Watchtower containers.
func (g *DependencyGraph) SelfDependencies() []*container.Container {
	return g.selfDependent
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
//   - error: An error naming the cycle when the containers depend on each other
//     in a cycle, or naming the container when one is outside the graph.
func (g *DependencyGraph) Sort(log *zerolog.Logger, containers []*container.Container) error {
	ordered, watchtowers := splitWatchtowers(containers)

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

// PlaceRebound returns plan with the rebound containers placed in it and each
// container of plan that joins another's namespace moved behind it.
//
// Each container is placed after the containers of plan and rebound that its
// host config names and that it depends on in the graph. Docker resolves the
// host config's containers when it creates a container, so they must exist
// again by then. A container that joins another's namespace is placed as soon
// as those are, so it directly follows the container it joins unless it waits
// for another. The rest of plan keeps its order. When only containers in a
// dependency cycle remain, the first container on a cycle is placed, so a
// container that waits for a cycle still follows the members it depends on.
// Watchtower's own containers stay last, as [DependencyGraph.Sort] places
// them, since their self-update hands over to a new process.
//
// Parameters:
//   - plan: Containers to restart, in the order [DependencyGraph.Sort] gives.
//   - rebound: The rebound containers.
//   - joiners: For each container, the rebound containers that join its
//     namespace.
//
// Returns:
//   - []*container.Container: The containers of plan and rebound, in restart
//     order.
func (g *DependencyGraph) PlaceRebound(
	plan, rebound []*container.Container,
	joiners map[container.ContainerID][]*container.Container,
) []*container.Container {
	// Watchtower's own containers stay last, so nothing is placed after
	// them.
	ordered, watchtowers := splitWatchtowers(plan)

	members := slices.Concat(ordered, rebound)

	// A container of plan that joins another's namespace comes before the
	// rebound containers that join it too.
	joining := make(map[container.ContainerID][]*container.Container, len(joiners))

	for _, c := range ordered {
		provider := slices.IndexFunc(members, c.JoinsNetworkOf)
		if provider >= 0 {
			id := members[provider].ID()
			joining[id] = append(joining[id], c)
		}
	}

	if len(rebound) == 0 && len(joining) == 0 {
		return plan
	}

	for id, reboundJoiners := range joiners {
		joining[id] = append(joining[id], reboundJoiners...)
	}

	pending, dependents := g.reboundEdges(members, rebound)

	// A joiner waits from the start, so it is placed as soon as it can be.
	// Any other container of plan waits once its turn came before its
	// dependencies were placed.
	waiting := make(map[container.ContainerID]bool, len(members))
	for _, c := range rebound {
		waiting[c.ID()] = true
	}

	for _, cs := range joining {
		for _, c := range cs {
			waiting[c.ID()] = true
		}
	}

	result := make([]*container.Container, 0, len(members))
	placed := make(map[container.ContainerID]bool, len(members))

	var place func(c *container.Container)

	place = func(c *container.Container) {
		placed[c.ID()] = true
		result = append(result, c)

		for _, dependent := range dependents[c.ID()] {
			pending[dependent.ID()]--
		}

		// The containers that join c come first, then the others c freed.
		for _, next := range slices.Concat(joining[c.ID()], dependents[c.ID()]) {
			if !placed[next.ID()] && waiting[next.ID()] && pending[next.ID()] == 0 {
				place(next)
			}
		}
	}

	for _, c := range ordered {
		if placed[c.ID()] {
			continue
		}

		if pending[c.ID()] == 0 {
			place(c)
		} else {
			waiting[c.ID()] = true
		}
	}

	// Every container left waits, directly or through others, for a
	// dependency cycle among the containers left.
	for next := firstOnCycle(members, placed, dependents); next != nil; next = firstOnCycle(members, placed, dependents) {
		place(next)
	}

	return append(result, watchtowers...)
}

// reboundEdges returns what each container waits for before PlaceRebound
// places it.
//
// A container waits for the containers it depends on in graph and for the
// rebound containers its host config names. A rebound container also waits
// for the containers of the plan its host config names.
//
// Parameters:
//   - members: The containers of the plan, followed by the rebound ones.
//   - rebound: The rebound containers.
//
// Returns:
//   - map[container.ContainerID]int: The number of containers each container
//     waits for.
//   - map[container.ContainerID][]*container.Container: For each container,
//     the containers that wait for it.
func (g *DependencyGraph) reboundEdges(
	members, rebound []*container.Container,
) (map[container.ContainerID]int, map[container.ContainerID][]*container.Container) {
	isMember := make(map[container.ContainerID]bool, len(members))
	for _, c := range members {
		isMember[c.ID()] = true
	}

	pending := make(map[container.ContainerID]int, len(members))
	dependents := make(map[container.ContainerID][]*container.Container, len(members))

	depend := func(c, dependency *container.Container) {
		pending[c.ID()]++
		dependents[dependency.ID()] = append(dependents[dependency.ID()], c)
	}

	for _, c := range members {
		for _, dependent := range g.Dependents(c) {
			if isMember[dependent.ID()] {
				depend(dependent, c)
			}
		}

		// The graph orders plan already, so a container of plan needs only
		// the rebound containers its host config names.
		others := rebound
		if c.IsRebinding() {
			others = members
		}

		for _, target := range c.CreateConfigTargets(others) {
			depend(c, target)
		}
	}

	return pending, dependents
}

// subgraph returns the part of the graph between containers, in the form
// kahnSort takes. Edges are added in the order of containers and of their
// links, so ties break as they do when the graph of containers alone is built.
//
// Parameters:
//   - containers: Containers of the graph.
//
// Returns:
//   - map[string]*container.Container: Container for each identifier.
//   - map[string]int: Number of dependencies of each identifier.
//   - map[string][]string: Dependents of each identifier.
//   - map[*container.Container]string: Identifier of each container.
//   - error: errNotInGraph for a container outside the graph, or an
//     identifierCollisionError for a container given twice.
func (g *DependencyGraph) subgraph(containers []*container.Container) (
	map[string]*container.Container, map[string]int, map[string][]string, map[*container.Container]string, error,
) {
	containerMap := make(map[string]*container.Container, len(containers))
	indegree := make(map[string]int, len(containers))
	adjacency := make(map[string][]string)
	normalizedMap := make(map[*container.Container]string, len(containers))

	for _, c := range containers {
		identifier, ok := g.identifiers[c]
		if !ok {
			return nil, nil, nil, nil, fmt.Errorf("%w: %s", errNotInGraph, c.Name())
		}

		if existing, duplicate := containerMap[identifier]; duplicate {
			return nil, nil, nil, nil, identifierCollisionError{
				DuplicateIdentifier: identifier,
				AffectedContainers:  []*container.Container{existing, c},
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

// addDependent records that dependent depends on target.
//
// Parameters:
//   - target: The container depended on.
//   - dependent: The container whose link names target.
func (g *DependencyGraph) addDependent(target, dependent *container.Container) {
	// A container's links are resolved together, so a container already
	// listed as a dependent of target is the last one listed.
	if listed := g.dependents[target]; len(listed) == 0 || listed[len(listed)-1] != dependent {
		g.dependents[target] = append(listed, dependent)
	}
}

// firstOnCycle returns the first container of members that is not placed and
// lies on a dependency cycle among the containers not placed.
//
// Parameters:
//   - members: The containers to place, in order.
//   - placed: The containers placed so far.
//   - dependents: For each container, the containers that wait on it.
//
// Returns:
//   - *container.Container: The container, or nil when none is left.
func firstOnCycle(
	members []*container.Container,
	placed map[container.ContainerID]bool,
	dependents map[container.ContainerID][]*container.Container,
) *container.Container {
	edges := make(map[string][]string, len(members))

	for _, c := range members {
		if placed[c.ID()] {
			continue
		}

		for _, dependent := range dependents[c.ID()] {
			if !placed[dependent.ID()] {
				edges[string(c.ID())] = append(edges[string(c.ID())], string(dependent.ID()))
			}
		}
	}

	finder := newCycleFinder(edges)

	for _, c := range members {
		if _, visited := finder.index[string(c.ID())]; !visited && !placed[c.ID()] {
			finder.visit(string(c.ID()))
		}
	}

	for _, c := range members {
		if !placed[c.ID()] && finder.inCycle[string(c.ID())] {
			return c
		}
	}

	return nil
}

// splitWatchtowers splits containers into Watchtower's own and the others.
//
// Watchtower's own containers are set aside and sorted last, since their
// self-update hands over to a new process.
//
// Parameters:
//   - containers: The containers to split, in order.
//
// Returns:
//   - []*container.Container: The containers that are not Watchtower's, in
//     order.
//   - []*container.Container: Watchtower's own containers, in order.
//
//nolint:nonamedreturns // Same-type returns need names.
func splitWatchtowers(containers []*container.Container) (others, watchtowers []*container.Container) {
	others = make([]*container.Container, 0, len(containers))

	for _, c := range containers {
		if c.IsWatchtower() {
			watchtowers = append(watchtowers, c)
		} else {
			others = append(others, c)
		}
	}

	return others, watchtowers
}
