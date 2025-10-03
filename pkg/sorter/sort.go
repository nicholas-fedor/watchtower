package sorter

import (
	"errors"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// ErrCircularReference indicates a circular dependency between containers.
var ErrCircularReference = errors.New("circular reference detected")

// ByCreated implements sort.Interface for creation time sorting.
type ByCreated []types.Container

// Len returns the number of containers.
//
// Returns:
//   - int: Container count.
func (c ByCreated) Len() int { return len(c) }

// Swap exchanges two containers by index.
//
// Parameters:
//   - i, indexJ: Indices to swap.
func (c ByCreated) Swap(i, indexJ int) { c[i], c[indexJ] = c[indexJ], c[i] }

// Less compares creation times, using now as fallback.
//
// Parameters:
//   - i, indexJ: Indices to compare.
//
// Returns:
//   - bool: True if i was created before j.
func (c ByCreated) Less(i, indexJ int) bool {
	// Parse creation time for container i.
	createdTimeI, err := time.Parse(time.RFC3339Nano, c[i].ContainerInfo().Created)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"container_id": c[i].ID().ShortID(),
			"name":         c[i].Name(),
			"created":      c[i].ContainerInfo().Created,
		}).WithError(err).Debug("Failed to parse created time, using current time as fallback")

		createdTimeI = time.Now()
	}

	// Parse creation time for container j.
	createdTimeJ, err := time.Parse(time.RFC3339Nano, c[indexJ].ContainerInfo().Created)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"container_id": c[indexJ].ID().ShortID(),
			"name":         c[indexJ].Name(),
			"created":      c[indexJ].ContainerInfo().Created,
		}).WithError(err).Debug("Failed to parse created time, using current time as fallback")

		createdTimeJ = time.Now()
	}

	return createdTimeI.Before(createdTimeJ)
}

// SortByDependencies sorts containers by dependencies.
//
// Parameters:
//   - containers: List to sort.
//
// Returns:
//   - []types.Container: Sorted list.
//   - error: Non-nil if circular reference detected, nil on success.
func SortByDependencies(containers []types.Container) ([]types.Container, error) {
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

	// Sort non-Watchtower containers by dependencies
	sorter := dependencySorter{
		unvisited: nil, // Containers yet to be visited
		marked:    nil, // Marks visited containers for cycle detection
		sorted:    nil, // Sorted result
	}

	sortedNonWatchtower, err := sorter.Sort(nonWatchtowerContainers)
	if err != nil {
		logrus.WithError(err).Debug("Dependency sort failed for non-Watchtower containers")

		return nil, err
	}

	// Append Watchtower containers at the end
	sorted := make([]types.Container, 0, len(sortedNonWatchtower)+len(watchtowerContainers))
	sorted = append(sorted, sortedNonWatchtower...)
	sorted = append(sorted, watchtowerContainers...)

	logrus.WithField("sorted_count", len(sorted)).
		Debug("Completed dependency sort with Watchtower containers last")

	return sorted, nil
}

// dependencySorter handles topological sorting by dependencies.
type dependencySorter struct {
	unvisited []types.Container // Yet-to-visit containers.
	marked    map[string]bool   // Visited markers for cycle detection.
	sorted    []types.Container // Sorted result.
}

// Sort performs topological sort on containers.
//
// Parameters:
//   - containers: List to sort.
//
// Returns:
//   - []types.Container: Sorted list.
//   - error: Non-nil if circular reference detected, nil on success.
func (ds *dependencySorter) Sort(containers []types.Container) ([]types.Container, error) {
	ds.unvisited = containers
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
	if _, ok := ds.marked[c.Name()]; ok {
		logrus.WithFields(logrus.Fields{
			"container_id": c.ID().ShortID(),
			"name":         c.Name(),
		}).Debug("Detected circular reference")

		return fmt.Errorf("%w: %s", ErrCircularReference, c.Name())
	}

	// Mark as visited, unmark on exit.
	ds.marked[c.Name()] = true
	defer delete(ds.marked, c.Name())

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
		if c.Name() == name {
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
	var idx int

	for i := range ds.unvisited {
		if ds.unvisited[i].Name() == c.Name() {
			idx = i

			break
		}
	}

	ds.unvisited = append(ds.unvisited[0:idx], ds.unvisited[idx+1:]...)
}
