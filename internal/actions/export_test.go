package actions

import (
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/pkg/sorter"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// UpdateImplicitRestart marks the containers that depend on a restarting
// container as linked to restarting, as an update scan does. The dependency
// graph is built from containers, with the rest of allContainers unmonitored.
//
// Parameters:
//   - log: Process logger.
//   - allContainers: Every listed container.
//   - containers: The monitored containers, which may be marked.
//   - useComposeDependsOn: Whether to include Compose depends_on labels as links.
func UpdateImplicitRestart(log *zerolog.Logger, allContainers, containers []types.Container, useComposeDependsOn bool) {
	graph, err := sorter.NewDependencyGraph(log,
		containers,
		unmonitoredContainers(allContainers, containers),
		useComposeDependsOn,
	)
	if err != nil {
		return
	}

	markImplicitRestarts(log, graph, allContainers, containers, nil)
}

// reconcileImplicitRestarts clears linked-restart marks and derives them again
// with a dependency graph built from containers, with the rest of
// allContainers unmonitored.
//
// Parameters:
//   - log: Process logger.
//   - allContainers: Every listed container.
//   - containers: The monitored containers.
//   - params: Update parameters.
//
// Returns:
//   - []types.Container: Containers that should still restart.
func reconcileImplicitRestarts(
	log *zerolog.Logger,
	allContainers, containers []types.Container,
	params types.UpdateParams,
) []types.Container {
	graph, err := sorter.NewDependencyGraph(log,
		containers,
		unmonitoredContainers(allContainers, containers),
		params.UseComposeDependsOn,
	)
	if err != nil {
		return nil
	}

	return reconcileImplicitRestartsExcluding(log, graph, allContainers, containers, params, nil)
}
