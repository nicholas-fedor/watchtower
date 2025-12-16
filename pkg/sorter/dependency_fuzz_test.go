package sorter

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/nicholas-fedor/watchtower/pkg/sorter/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// FuzzDependencySort fuzz tests the dependency sorting algorithm with random inputs.
func FuzzDependencySort(f *testing.F) {
	// Add some seed corpus
	f.Add(int32(5), float32(0.5))  // Small graph with medium dependencies
	f.Add(int32(20), float32(0.2)) // Medium graph with low dependencies
	f.Add(int32(50), float32(0.8)) // Large graph with high dependencies

	f.Fuzz(func(t *testing.T, count int32, depFactor float32) {
		// Sanitize inputs
		if count < 1 {
			count = 1
		}

		if count > 200 { // Limit for fuzz testing
			count = 200
		}

		if depFactor < 0 {
			depFactor = 0
		}

		if depFactor > 1 {
			depFactor = 1
		}

		containers := generateBenchmarkContainers(int(count), float64(depFactor))

		ds := DependencySorter{}
		testContainers := make([]types.Container, len(containers))
		copy(testContainers, containers)

		// This should not panic
		err := ds.Sort(testContainers)
		// If there's an error, it should be a CircularReferenceError
		if err != nil {
			var circularErr CircularReferenceError
			if !errors.As(err, &circularErr) {
				t.Fatalf("Unexpected error type: %T, error: %v", err, err)
			}
		}

		// Verify all containers are still present
		if len(testContainers) != len(containers) {
			t.Fatalf("Container count changed: %d -> %d", len(containers), len(testContainers))
		}

		// Verify all original containers are present in result
		originalNames := make(map[string]bool)
		for _, c := range containers {
			originalNames[c.Name()] = true
		}

		for _, c := range testContainers {
			if !originalNames[c.Name()] {
				t.Fatalf("Unknown container in result: %s", c.Name())
			}
		}
	})
}

// FuzzCycleDetection fuzz tests cycle detection specifically.
func FuzzCycleDetection(f *testing.F) {
	// Add seed corpus with known cycle patterns
	f.Add([]byte("A->B,B->A"))      // Simple cycle
	f.Add([]byte("A->B,B->C,C->A")) // Triangle cycle
	f.Add([]byte("A->B,B->C,C->D")) // No cycle
	f.Add([]byte("A->B,B->A,C->D")) // Cycle with separate component

	f.Fuzz(func(t *testing.T, data []byte) {
		// Parse the fuzz data into a dependency graph
		containers := parseFuzzDependencyData(data)

		if len(containers) == 0 {
			return // Skip empty inputs
		}

		ds := DependencySorter{}
		testContainers := make([]types.Container, len(containers))
		copy(testContainers, containers)

		// This should not panic
		err := ds.Sort(testContainers)
		// Error should either be nil or CircularReferenceError
		if err != nil {
			var circularErr CircularReferenceError
			if !errors.As(err, &circularErr) {
				t.Fatalf("Unexpected error type: %T, error: %v", err, err)
			}
		}
	})
}

// parseFuzzDependencyData parses fuzz input into containers with dependencies
// Format: "A->B,B->C" becomes containers A,B,C with A depending on B, B depending on C.
func parseFuzzDependencyData(data []byte) []types.Container {
	if len(data) == 0 {
		return nil
	}

	// Simple parsing: split by comma, then by ->
	dependencies := make(map[string][]string)
	allNodes := make(map[string]bool)

	parts := bytes.SplitSeq(data, []byte(","))
	for part := range parts {
		part = bytes.TrimSpace(part)
		if len(part) == 0 {
			continue
		}

		arrowIndex := bytes.Index(part, []byte("->"))
		if arrowIndex == -1 {
			// Single node
			node := string(bytes.TrimSpace(part))
			if node != "" {
				allNodes[node] = true
			}

			continue
		}

		from := string(bytes.TrimSpace(part[:arrowIndex]))
		to := string(bytes.TrimSpace(part[arrowIndex+2:]))

		if from != "" && to != "" {
			dependencies[from] = append(dependencies[from], to)
			allNodes[from] = true
			allNodes[to] = true
		}
	}

	// Create containers
	containers := make([]types.Container, 0, len(allNodes))

	i := 0

	for node := range allNodes {
		links := dependencies[node]
		if links == nil {
			links = []string{}
		}

		container := &mocks.SimpleContainer{
			ContainerName:  node,
			ContainerID:    types.ContainerID(fmt.Sprintf("fuzz-id-%d", i)),
			ContainerLinks: links,
		}
		containers = append(containers, container)
		i++
	}

	return containers
}
