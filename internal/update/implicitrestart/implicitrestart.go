package implicitrestart

import (
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
