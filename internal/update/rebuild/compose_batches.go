package rebuild

import (
	"slices"
	"strings"

	"github.com/rs/zerolog"

	composeProject "github.com/nicholas-fedor/watchtower/internal/compose/project"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/internal/update/sorter"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// composeMember is one stale container and the check result that selected its commit.
type composeMember struct {
	container types.Container
	result    git.CheckResult
}

// composeBatch is one on-disk Compose project to checkout and apply.
type composeBatch struct {
	ref     composeProject.ProjectRef
	members []composeMember
}

// composeBatches collects stale containers into one batch per Compose project,
// in the order each project is first seen. The zero value is ready to use.
type composeBatches struct {
	// byKey finds the batch of a project by its identity.
	byKey map[composeBatchKey]*composeBatch
	// firstSeen holds the batches in the order their projects were first seen.
	firstSeen []*composeBatch
}

// composeBatchKey identifies one Compose project, so containers of the same
// project share a batch.
type composeBatchKey struct {
	// name is the Compose project name.
	name string
	// dir is the project directory.
	dir string
	// configFiles holds the project's config file paths, joined by NUL bytes.
	configFiles string
}

// composeBatchKeyFor returns the identity used to group one Compose project batch.
//
// Parameters:
//   - ref: Resolved Compose project reference.
//
// Returns:
//   - composeBatchKey: Identity derived from the project name, directory, and config files.
func composeBatchKeyFor(ref composeProject.ProjectRef) composeBatchKey {
	return composeBatchKey{
		name:        ref.Name,
		dir:         ref.Dir,
		configFiles: strings.Join(ref.ConfigFiles, "\x00"),
	}
}

// orderedComposeContainers returns a dependency-ordered copy of the containers.
//
// Parameters:
//   - log: Process logger.
//   - containers: Containers to order.
//   - useComposeDependsOn: Whether to include Compose depends_on relationships when sorting.
//
// Returns:
//   - []types.Container: Containers in dependency order, or a copy of the original order if sorting fails.
func orderedComposeContainers(
	log *zerolog.Logger,
	containers []types.Container,
	useComposeDependsOn bool,
) []types.Container {
	ordered := slices.Clone(containers)
	if len(ordered) == 0 || len(ordered) == 1 {
		return ordered
	}

	err := sorter.SortByDependencies(log, ordered, useComposeDependsOn)
	if err != nil {
		log.Warn().
			Err(err).
			Msg("Could not dependency-sort Compose batches. Using the existing container order")

		return slices.Clone(containers)
	}

	return ordered
}

// add appends a stale container to the batch of its Compose project, starting
// a new batch when the project is first seen.
//
// Parameters:
//   - ref: The container's resolved Compose project.
//   - member: The container and the check result that selected its commit.
func (b *composeBatches) add(ref composeProject.ProjectRef, member composeMember) {
	key := composeBatchKeyFor(ref)

	batch, ok := b.byKey[key]
	if !ok {
		if b.byKey == nil {
			b.byKey = make(map[composeBatchKey]*composeBatch)
		}

		batch = &composeBatch{ref: ref}
		b.byKey[key] = batch
		b.firstSeen = append(b.firstSeen, batch)
	}

	batch.members = append(batch.members, member)
}

// inDependencyOrder returns the batches ordered by cross-project dependencies.
//
// First-seen order is wrong when a project contains both an independent service
// and a service that depends on another project. A batch runs after every batch
// that contains a dependency of any of its members. Dependencies inside one
// batch add no order. Among batches that are ready together, the one seen first
// runs first. Batches in a dependency cycle, and batches that depend on one,
// follow the others in first-seen order. A failed dependency lookup leaves
// first-seen order in place.
//
// Parameters:
//   - log: Process logger.
//   - containers: Containers used to resolve dependency links.
//   - useComposeDependsOn: Whether to include Compose depends_on relationships.
//
// Returns:
//   - []*composeBatch: Batches in dependency order.
func (b *composeBatches) inDependencyOrder(
	log *zerolog.Logger,
	containers []types.Container,
	useComposeDependsOn bool,
) []*composeBatch {
	const minBatchesToOrder = 2

	batches := b.firstSeen
	if len(batches) < minBatchesToOrder {
		return batches
	}

	dependencies, err := sorter.ContainerDependencies(log, containers, useComposeDependsOn)
	if err != nil {
		log.Warn().
			Err(err).
			Msg("Could not order Compose batches by dependencies. Using first-seen order")

		return batches
	}

	batchOf := make(map[types.ContainerID]int, len(containers))

	for i, batch := range batches {
		for _, member := range batch.members {
			batchOf[member.container.ID()] = i
		}
	}

	indegree := make([]int, len(batches))
	after := make([][]int, len(batches))
	seen := make(map[[2]int]struct{})

	for i, batch := range batches {
		for _, member := range batch.members {
			for _, dependencyID := range dependencies[member.container.ID()] {
				predecessor, ok := batchOf[dependencyID]
				if !ok || predecessor == i {
					continue
				}

				edge := [2]int{predecessor, i}
				if _, exists := seen[edge]; exists {
					continue
				}

				seen[edge] = struct{}{}

				after[predecessor] = append(after[predecessor], i)
				indegree[i]++
			}
		}
	}

	ready := make([]int, 0, len(batches))

	for i, degree := range indegree {
		if degree == 0 {
			ready = append(ready, i)
		}
	}

	ordered := make([]*composeBatch, 0, len(batches))
	for len(ready) > 0 {
		pick := 0
		for i := 1; i < len(ready); i++ {
			if ready[i] < ready[pick] {
				pick = i
			}
		}

		current := ready[pick]
		ready = append(ready[:pick], ready[pick+1:]...)
		ordered = append(ordered, batches[current])

		for _, next := range after[current] {
			indegree[next]--
			if indegree[next] == 0 {
				ready = append(ready, next)
			}
		}
	}

	if len(ordered) == len(batches) {
		return ordered
	}

	log.Warn().
		Msg("Compose batch dependencies are cyclic. Appending unresolved batches in first-seen order")

	for i, batch := range batches {
		if indegree[i] > 0 {
			ordered = append(ordered, batch)
		}
	}

	return ordered
}
