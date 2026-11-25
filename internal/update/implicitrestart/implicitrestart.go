package implicitrestart

import (
	"slices"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/sorter"
)

// markImplicitRestarts marks each container that depends on a restarting
// container, directly or through other marked containers, as linked to a
// restarting container.
//
// Dependencies are the links of the scan's dependency graph, so a link names
// the same container here as when the containers are sorted. A container that
// is already restarting is not marked again.
//
// Parameters:
//   - log: Process logger.
//   - graph: The scan's dependency graph.
//   - allContainers: Every listed container. Those restarting start the marking.
//   - containers: Containers that may be marked.
//   - excluded: Containers that are neither marked nor treated as restarting.
//
// This function mutates the LinkedToRestarting state of containers in place.
func markImplicitRestarts(
	log *zerolog.Logger,
	graph *sorter.DependencyGraph,
	allContainers, containers []*container.Container,
	excluded map[container.ContainerID]struct{},
) {
	eligible := make(map[container.ContainerID]bool, len(containers))

	for _, c := range containers {
		if _, skip := excluded[c.ID()]; !skip {
			eligible[c.ID()] = true
		}
	}

	queue := make([]*container.Container, 0, len(allContainers))

	for _, c := range allContainers {
		if _, skip := excluded[c.ID()]; !skip && c.ToRestart() {
			queue = append(queue, c)
		}
	}

	for len(queue) > 0 {
		restarting := queue[0]
		queue = queue[1:]

		for _, dependent := range graph.Dependents(restarting) {
			if !eligible[dependent.ID()] || dependent.ToRestart() {
				continue
			}

			dependent.SetLinkedToRestarting(true)
			queue = append(queue, dependent)

			log.Debug().
				Str("container", dependent.Name()).
				Str("restarting", restarting.Name()).
				Msg("Marked container as linked to restarting")
		}
	}
}

// Reconcile marks the containers that restart because a container they depend
// on restarts, and returns every container that should restart. It first
// clears the marks a previous call left, so it can run again after the set of
// restarting containers changes. Excluded containers are neither marked nor
// treated as restarting, and monitor-only containers are left out of the
// result.
//
// Parameters:
//   - log: Process logger.
//   - graph: The scan's dependency graph.
//   - allContainers: Full list of containers being managed.
//   - containers: Containers eligible for this session.
//   - params: Update parameters.
//   - excluded: Container IDs that must not propagate or receive implicit restarts.
//
// Returns:
//   - []*container.Container: Containers that should still restart.
func Reconcile(
	log *zerolog.Logger,
	graph *sorter.DependencyGraph,
	allContainers, containers []*container.Container,
	params updateParams.Params,
	excluded map[container.ContainerID]struct{},
) []*container.Container {
	for _, c := range containers {
		c.SetLinkedToRestarting(false)
	}

	markImplicitRestarts(log, graph, allContainers, containers, excluded)

	restart := make([]*container.Container, 0, len(containers))
	for _, c := range containers {
		if _, skip := excluded[c.ID()]; skip {
			continue
		}

		if c.ToRestart() && !c.IsMonitorOnly(params.Policy) {
			restart = append(restart, c)
		}
	}

	return restart
}

// Rebind marks the containers that join the network namespace of a container
// in plan as rebinding and returns plan with them placed in it. A container of
// plan that joins another's namespace moves behind that container too.
//
// A container is rebound whether it is running or stopped, including one that
// is monitor-only or not monitored. A container that joins a rebound container
// is rebound in turn. Watchtower containers, runs of Docker Engine jobs,
// excluded containers and containers already in plan are never rebound.
// Neither are the containers that join a Watchtower container, whose
// self-update replaces it after this process hands over. The containers are
// placed as [sorter.DependencyGraph.PlaceRebound] describes.
//
// Parameters:
//   - log: Process logger.
//   - graph: The scan's dependency graph.
//   - plan: Containers to restart, in restart order.
//   - candidates: Containers that may be rebound, in the order to consider them.
//   - excluded: Container IDs that must not be rebound.
//
// Returns:
//   - []*container.Container: plan with the rebound containers placed in it,
//     in restart order.
func Rebind(
	log *zerolog.Logger,
	graph *sorter.DependencyGraph,
	plan, candidates []*container.Container,
	excluded map[container.ContainerID]struct{},
) []*container.Container {
	joiners, rebound := markRebound(log, plan, candidates, excluded)

	return graph.PlaceRebound(plan, rebound, joiners)
}

// markRebound marks the candidates that join the network namespace of a
// container in plan, directly or through other marked candidates, as
// rebinding.
//
// Every container of plan and every marked candidate that joins one of these
// containers names it by name in its network mode afterwards.
//
// Each marked candidate's network mode is set to name the container it joins,
// so that its replacement joins that container's replacement.
//
// Parameters:
//   - log: Process logger.
//   - plan: Containers to restart.
//   - candidates: Containers that may be rebound, in the order to consider them.
//   - excluded: Container IDs that must not be rebound.
//
// Returns:
//   - map[container.ContainerID][]*container.Container: For each container,
//     the marked candidates that join its namespace, in candidate order.
//   - []*container.Container: The marked candidates.
func markRebound(
	log *zerolog.Logger,
	plan, candidates []*container.Container,
	excluded map[container.ContainerID]struct{},
) (map[container.ContainerID][]*container.Container, []*container.Container) {
	// The plan's containers are never marked.
	seen := make(map[container.ContainerID]bool, len(plan))
	for _, c := range plan {
		seen[c.ID()] = true
	}

	joiners := make(map[container.ContainerID][]*container.Container)

	// Docker stores a network mode that names another container by that
	// container's ID. A container that joins a recreated container's
	// namespace keeps the removed ID and loses its network unless it is
	// recreated after it, naming the container by name. The scan replaces the
	// ID with the name, unless it could not inspect the container.
	// The queue holds the plan, followed by each marked candidate in turn.
	providers := slices.Clone(plan)

	for i := 0; i < len(providers); i++ {
		provider := providers[i]

		// The self-update replaces Watchtower's container after this process
		// hands over, too late to recreate a container against it.
		if provider.IsWatchtower() {
			continue
		}

		// A container of plan that joins the provider is recreated after it
		// too, so its network mode must name the provider rather than the
		// ID the replacement makes stale.
		for _, c := range plan {
			if c.JoinsNetworkOf(provider) {
				c.SetNetworkContainer(provider.Name())
			}
		}

		for _, c := range candidates {
			_, skip := excluded[c.ID()]
			if skip || seen[c.ID()] || c.IsWatchtower() || c.IsJobRun() || !c.JoinsNetworkOf(provider) {
				continue
			}

			seen[c.ID()] = true
			c.SetRebinding(true)

			c.SetNetworkContainer(provider.Name())

			log.Debug().
				Str("container", c.Name()).
				Str("network_container", provider.Name()).
				Msg("Marked container for rebinding to a recreated network container")

			joiners[provider.ID()] = append(joiners[provider.ID()], c)
			providers = append(providers, c)
		}
	}

	return joiners, providers[len(plan):]
}
