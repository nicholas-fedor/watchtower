package docker

import (
	"context"
	"fmt"
	"maps"

	"github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
)

// CreatedSubjects is the fixture Watchtower will act on.
type CreatedSubjects struct {
	// PrimaryID is the inspect target (the first node, or the lone subject).
	PrimaryID string
	// PrimaryName is the Docker name of PrimaryID.
	PrimaryName string
	// DependencyOrder is names from dependency to dependent (A, B, C, D).
	DependencyOrder []string
}

// CreateSubjects starts the topology graph on the inner daemon.
//
// chain-4 is A <- B <- C <- D (D depends on C, and so on). Cycle is A <-> B.
// compose-depends sets Compose depends_on labels instead of Watchtower labels.
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Inner Docker client.
//   - prefix: Case resource prefix.
//   - image: Image all subjects run.
//   - topo: Graph kind and labels.
//
// Returns:
//   - CreatedSubjects: Names and IDs in dependency order.
//   - error: Create failure.
func CreateSubjects(ctx context.Context, cli *client.Client, prefix, image string, topo engine.Topology) (CreatedSubjects, error) {
	switch topo.Graph {
	case engine.GraphNone, "":
		return createGroup(ctx, cli, prefix, image, topo)
	case engine.GraphChain4:
		return createChain(ctx, cli, prefix, image, topo, false)
	case engine.GraphCycle:
		return createCycle(ctx, cli, prefix, image, topo)
	case engine.GraphComposeDepends:
		return createChain(ctx, cli, prefix, image, topo, true)
	default:
		return CreatedSubjects{}, fmt.Errorf("%w: %s", errUnknownGraph, topo.Graph)
	}
}

// createGroup starts SubjectCount unlabeled peers on the same image.
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Inner Docker client.
//   - prefix: Name prefix.
//   - image: Shared image.
//   - topo: Base topology, including SubjectCount and Networks.
//
// Returns:
//   - CreatedSubjects: Peers. Primary is the first.
//   - error: Create failure.
func createGroup(ctx context.Context, cli *client.Client, prefix, image string, topo engine.Topology) (CreatedSubjects, error) {
	count := max(topo.SubjectCount, 1)

	names := make([]string, 0, count)

	var primaryID string

	for idx := range count {
		name := prefix + "-subject"
		if count > 1 {
			name = fmt.Sprintf("%s-subject-%d", prefix, idx)
		}

		containerID, err := CreateSubject(ctx, cli, name, image, topo)
		if err != nil {
			return CreatedSubjects{}, fmt.Errorf("create %s: %w", name, err)
		}

		if idx == 0 {
			primaryID = containerID
		}

		names = append(names, name)
	}

	return CreatedSubjects{
		PrimaryID:       primaryID,
		PrimaryName:     names[0],
		DependencyOrder: names,
	}, nil
}

// createChain starts A, B, C, D with B depending on A, C on B, D on C.
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Inner Docker client.
//   - prefix: Name prefix.
//   - image: Shared image.
//   - topo: Base topology.
//   - compose: When true, set Compose depends_on labels.
//
// Returns:
//   - CreatedSubjects: Chain in dependency order.
//   - error: Create failure.
func createChain(ctx context.Context, cli *client.Client, prefix, image string, topo engine.Topology, compose bool) (CreatedSubjects, error) {
	nodes := []string{prefix + "-a", prefix + "-b", prefix + "-c", prefix + "-d"}
	rootName := nodes[0]
	rootTopo := topo
	rootTopo.Labels = copyLabels(topo.Labels)

	rootID, err := CreateSubject(ctx, cli, rootName, image, rootTopo)
	if err != nil {
		return CreatedSubjects{}, fmt.Errorf("create %s: %w", rootName, err)
	}

	parent := rootName

	for _, name := range nodes[1:] {
		nodeTopo := topo

		nodeTopo.Labels = copyLabels(topo.Labels)
		if compose {
			nodeTopo.Labels["com.docker.compose.depends_on"] = parent + ":service_started:false"
		} else {
			nodeTopo.Labels[dependsOnLabel] = parent
		}

		_, createErr := CreateSubject(ctx, cli, name, image, nodeTopo)
		if createErr != nil {
			return CreatedSubjects{}, fmt.Errorf("create %s: %w", name, createErr)
		}

		parent = name
	}

	return CreatedSubjects{
		PrimaryID:       rootID,
		PrimaryName:     rootName,
		DependencyOrder: nodes,
	}, nil
}

// createCycle starts A and B that each declare depends-on the other.
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Inner Docker client.
//   - prefix: Name prefix.
//   - image: Shared image.
//   - topo: Base topology.
//
// Returns:
//   - CreatedSubjects: The two nodes.
//   - error: Create failure.
func createCycle(ctx context.Context, cli *client.Client, prefix, image string, topo engine.Topology) (CreatedSubjects, error) {
	left := prefix + "-a"
	right := prefix + "-b"

	leftTopo := topo
	leftTopo.Labels = copyLabels(topo.Labels)
	leftTopo.Labels[dependsOnLabel] = right

	rightTopo := topo
	rightTopo.Labels = copyLabels(topo.Labels)
	rightTopo.Labels[dependsOnLabel] = left

	leftID, err := CreateSubject(ctx, cli, left, image, leftTopo)
	if err != nil {
		return CreatedSubjects{}, fmt.Errorf("create %s: %w", left, err)
	}

	_, rightErr := CreateSubject(ctx, cli, right, image, rightTopo)
	if rightErr != nil {
		return CreatedSubjects{}, fmt.Errorf("create %s: %w", right, rightErr)
	}

	return CreatedSubjects{
		PrimaryID:       leftID,
		PrimaryName:     left,
		DependencyOrder: []string{left, right},
	}, nil
}

// copyLabels copies a label map so each node can add depends-on without sharing.
//
// Parameters:
//   - src: Original labels. May be nil.
//
// Returns:
//   - map[string]string: Independent copy.
func copyLabels(src map[string]string) map[string]string {
	out := make(map[string]string, len(src)+1)
	maps.Copy(out, src)

	return out
}
