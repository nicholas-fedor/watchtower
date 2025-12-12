package sorter

import (
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/compose"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// DependencySorter handles topological sorting by dependencies.
type DependencySorter struct{}

// Sort sorts containers in place by dependencies, placing Watchtower containers last.
//
// Parameters:
//   - containers: Slice to sort in place.
//
// Returns:
//   - error: Non-nil if circular reference detected, nil on success.
func (ds DependencySorter) Sort(containers []types.Container) error {
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
	sorter := dependencySorter{
		unvisited: nil, // Containers yet to be visited
		marked:    nil, // Marks visited containers for cycle detection
		sorted:    nil, // Sorted result
	}

	sortedNonWatchtower, err := sorter.sort(nonWatchtowerContainers)
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

// dependencySorter handles topological sorting by dependencies.
type dependencySorter struct {
	unvisited []types.Container // Yet-to-visit containers.
	marked    map[string]bool   // Visited markers for cycle detection.
	sorted    []types.Container // Sorted result.
}

// sort performs topological sort on containers.
//
// Parameters:
//   - containers: List to sort.
//
// Returns:
//   - []types.Container: Sorted list.
//   - error: Non-nil if circular reference detected, nil on success.
func (ds *dependencySorter) sort(containers []types.Container) ([]types.Container, error) {
	ds.unvisited = make([]types.Container, len(containers))
	copy(ds.unvisited, containers)
	ds.marked = map[string]bool{}

	// Process containers with no links first.
	for i := 0; i < len(ds.unvisited); i++ {
		if len(ds.unvisited[i].Links()) == 0 {
			if err := ds.visit(ds.unvisited[i]); err != nil {
				return nil, err
			}

			i-- // Adjust for removal.
		}
	}

	// Process remaining containers.
	for len(ds.unvisited) > 0 {
		if err := ds.visit(ds.unvisited[0]); err != nil {
			return nil, err
		}
	}

	return ds.sorted, nil
}

// visit adds a container to the sorted list after its links.
//
// Parameters:
//   - c: Container to visit.
//
// Returns:
//   - error: Non-nil if circular reference detected, nil on success.
func (ds *dependencySorter) visit(c types.Container) error {
	// Check for circular reference.
	if _, ok := ds.marked[util.NormalizeContainerName(GetContainerIdentifier(c))]; ok {
		logrus.WithFields(logrus.Fields{
			"container_id": c.ID().ShortID(),
			"name":         c.Name(),
		}).Debug("Detected circular reference")

		return CircularReferenceError{ContainerName: c.Name()}
	}

	// Mark as visited, unmark on exit.
	ds.marked[util.NormalizeContainerName(GetContainerIdentifier(c))] = true
	defer delete(ds.marked, util.NormalizeContainerName(GetContainerIdentifier(c)))

	// Visit all linked containers.
	for _, linkName := range c.Links() {
		if linkedContainer := ds.findUnvisited(linkName); linkedContainer != nil {
			if err := ds.visit(*linkedContainer); err != nil {
				return err
			}
		}
	}

	// Add to sorted list.
	ds.removeUnvisited(c)
	ds.sorted = append(ds.sorted, c)
	logrus.WithFields(logrus.Fields{
		"container_id": c.ID().ShortID(),
		"name":         c.Name(),
	}).Debug("Added container to sorted list")

	return nil
}

// findUnvisited finds an unvisited container by name.
//
// Parameters:
//   - name: Name to find.
//
// Returns:
//   - *types.Container: Found container or nil.
func (ds *dependencySorter) findUnvisited(name string) *types.Container {
	for _, c := range ds.unvisited {
		if util.NormalizeContainerName(
			GetContainerIdentifier(c),
		) == util.NormalizeContainerName(
			name,
		) {
			return &c
		}
	}

	return nil
}

// removeUnvisited removes a container from the unvisited list.
//
// Parameters:
//   - c: Container to remove.
func (ds *dependencySorter) removeUnvisited(c types.Container) {
	idx := -1

	for i := range ds.unvisited {
		if util.NormalizeContainerName(
			GetContainerIdentifier(ds.unvisited[i]),
		) == util.NormalizeContainerName(
			GetContainerIdentifier(c),
		) {
			idx = i

			break
		}
	}

	if idx == -1 {
		return
	}

	ds.unvisited = append(ds.unvisited[:idx], ds.unvisited[idx+1:]...)
}

// GetContainerIdentifier returns the service name if available, otherwise container name.
func GetContainerIdentifier(c types.Container) string {
	if serviceName := compose.GetServiceName(c.ContainerInfo().Config.Labels); serviceName != "" {
		return serviceName
	}

	return c.Name()
}
