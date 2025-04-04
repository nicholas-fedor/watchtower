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

// Implements sort.Interface.
type ByCreated []types.Container

// Len returns the number of containers in the list.
func (c ByCreated) Len() int { return len(c) }

// Swap exchanges two containers in the list by their indices.
func (c ByCreated) Swap(i, indexJ int) { c[i], c[indexJ] = c[indexJ], c[i] }

// Uses current time as fallback if parsing fails.
func (c ByCreated) Less(i, indexJ int) bool {
	createdTimeI, err := time.Parse(time.RFC3339Nano, c[i].ContainerInfo().Created)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"container_id": c[i].ID().ShortID(),
			"name":         c[i].Name(),
			"created":      c[i].ContainerInfo().Created,
		}).WithError(err).Debug("Failed to parse created time, using current time as fallback")

		createdTimeI = time.Now()
	}

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

// Places containers with no outgoing links first, followed by their dependents.
func SortByDependencies(containers []types.Container) ([]types.Container, error) {
	logrus.WithField("container_count", len(containers)).Debug("Starting dependency sort")

	sorter := dependencySorter{
		unvisited: nil, // Containers yet to be visited
		marked:    nil, // Marks visited containers for cycle detection
		sorted:    nil, // Sorted result
	}

	sorted, err := sorter.Sort(containers)
	if err != nil {
		logrus.WithError(err).Debug("Dependency sort failed")
	} else {
		logrus.WithField("sorted_count", len(sorted)).Debug("Completed dependency sort")
	}

	return sorted, err
}

// dependencySorter manages the topological sort of containers by dependencies.
type dependencySorter struct {
	unvisited []types.Container // Containers yet to be visited
	marked    map[string]bool   // Marks visited containers for cycle detection
	sorted    []types.Container // Sorted result
}

// Prioritizes containers with no links, then processes dependents; returns an error for circular references.
func (ds *dependencySorter) Sort(containers []types.Container) ([]types.Container, error) {
	ds.unvisited = containers
	ds.marked = map[string]bool{}

	// Process containers with no links first
	for i := 0; i < len(ds.unvisited); i++ {
		if len(ds.unvisited[i].Links()) == 0 {
			if err := ds.visit(ds.unvisited[i]); err != nil {
				return nil, err
			}

			i-- // Adjust for removal
		}
	}

	// Process remaining containers with links
	for len(ds.unvisited) > 0 {
		if err := ds.visit(ds.unvisited[0]); err != nil {
			return nil, err
		}
	}

	return ds.sorted, nil
}

// Adds the container to the sorted list after all its links are visited.
func (ds *dependencySorter) visit(c types.Container) error {
	if _, ok := ds.marked[c.Name()]; ok {
		logrus.WithFields(logrus.Fields{
			"container_id": c.ID().ShortID(),
			"name":         c.Name(),
		}).Debug("Detected circular reference")

		return fmt.Errorf("%w: %s", ErrCircularReference, c.Name())
	}

	// Mark container as visited to detect cycles
	ds.marked[c.Name()] = true
	defer delete(ds.marked, c.Name())

	// Visit each linked container recursively
	for _, linkName := range c.Links() {
		if linkedContainer := ds.findUnvisited(linkName); linkedContainer != nil {
			if err := ds.visit(*linkedContainer); err != nil {
				return err
			}
		}
	}

	// Move container from unvisited to sorted
	ds.removeUnvisited(c)
	ds.sorted = append(ds.sorted, c)
	logrus.WithFields(logrus.Fields{
		"container_id": c.ID().ShortID(),
		"name":         c.Name(),
	}).Debug("Added container to sorted list")

	return nil
}

// Returns a pointer to the container or nil if not found.
func (ds *dependencySorter) findUnvisited(name string) *types.Container {
	for _, c := range ds.unvisited {
		if c.Name() == name {
			return &c
		}
	}

	return nil
}

// Adjusts the slice to exclude the matching container.
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
