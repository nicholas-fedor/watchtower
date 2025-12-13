package sorter

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"testing"

	dockerContainerTypes "github.com/docker/docker/api/types/container"
	dockerImageTypes "github.com/docker/docker/api/types/image"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// SimpleContainer implements a minimal Container interface for benchmarking.
type SimpleContainer struct {
	name  string
	id    string
	links []string
}

func (c *SimpleContainer) Name() string {
	return c.name
}

func (c *SimpleContainer) ID() types.ContainerID {
	return types.ContainerID(c.id)
}

func (c *SimpleContainer) Links() []string {
	return c.links
}

func (c *SimpleContainer) IsWatchtower() bool {
	return false
}

func (c *SimpleContainer) ContainerInfo() *dockerContainerTypes.InspectResponse {
	return &dockerContainerTypes.InspectResponse{
		ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/" + c.name},
		Config:            &dockerContainerTypes.Config{Labels: map[string]string{}},
	}
}

// Implement remaining required methods with minimal implementations.
func (c *SimpleContainer) IsRunning() bool { return true }

func (c *SimpleContainer) ImageID() types.ImageID { return types.ImageID("sha256:" + c.id) }

func (c *SimpleContainer) SafeImageID() types.ImageID { return c.ImageID() }

func (c *SimpleContainer) ImageName() string { return "test-image" }

func (c *SimpleContainer) Enabled() (bool, bool)                   { return true, true }
func (c *SimpleContainer) IsMonitorOnly(_ types.UpdateParams) bool { return false }

func (c *SimpleContainer) Scope() (string, bool) { return "", false }
func (c *SimpleContainer) ToRestart() bool       { return false }

func (c *SimpleContainer) StopSignal() string                                    { return "SIGTERM" }
func (c *SimpleContainer) HasImageInfo() bool                                    { return false }
func (c *SimpleContainer) ImageInfo() *dockerImageTypes.InspectResponse          { return nil }
func (c *SimpleContainer) GetLifecyclePreCheckCommand() string                   { return "" }
func (c *SimpleContainer) GetLifecyclePostCheckCommand() string                  { return "" }
func (c *SimpleContainer) GetLifecyclePreUpdateCommand() string                  { return "" }
func (c *SimpleContainer) GetLifecyclePostUpdateCommand() string                 { return "" }
func (c *SimpleContainer) GetLifecycleUID() (int, bool)                          { return 0, false }
func (c *SimpleContainer) GetLifecycleGID() (int, bool)                          { return 0, false }
func (c *SimpleContainer) VerifyConfiguration() error                            { return nil }
func (c *SimpleContainer) SetStale(_ bool)                                       {}
func (c *SimpleContainer) IsStale() bool                                         { return false }
func (c *SimpleContainer) IsNoPull(_ types.UpdateParams) bool                    { return false }
func (c *SimpleContainer) SetLinkedToRestarting(_ bool)                          {}
func (c *SimpleContainer) IsLinkedToRestarting() bool                            { return false }
func (c *SimpleContainer) PreUpdateTimeout() int                                 { return 30 }
func (c *SimpleContainer) PostUpdateTimeout() int                                { return 30 }
func (c *SimpleContainer) IsRestarting() bool                                    { return false }
func (c *SimpleContainer) GetCreateConfig() *dockerContainerTypes.Config         { return nil }
func (c *SimpleContainer) GetCreateHostConfig() *dockerContainerTypes.HostConfig { return nil }

// generateBenchmarkContainers creates containers with realistic dependency patterns.
func generateBenchmarkContainers(count int, dependencyFactor float64) []types.Container {
	containers := make([]types.Container, count)

	// Create containers
	for i := range count {
		name := fmt.Sprintf("container-%d", i)
		container := &SimpleContainer{
			name:  name,
			id:    fmt.Sprintf("id-%d", i),
			links: []string{}, // Will be set below
		}
		containers[i] = container
	}

	// Add dependencies based on dependency factor

	for i := range count {
		container := containers[i].(*SimpleContainer)
		// Each container depends on up to 'dependencyFactor' percentage of previous containers
		maxDeps := int(float64(i) * dependencyFactor)
		if maxDeps > 5 { // Cap at 5 dependencies per container for realism
			maxDeps = 5
		}

		var links []string

		for range maxDeps {
			depIndex := rand.Intn(i + 1)
			if depIndex != i { // Don't depend on self
				depName := fmt.Sprintf("container-%d", depIndex)
				links = append(links, depName)
			}
		}

		container.links = links
	}

	return containers
}

// generateChainDependencies creates a linear dependency chain.
func generateChainDependencies(count int) []types.Container {
	containers := make([]types.Container, count)

	for i := range count {
		name := fmt.Sprintf("chain-%d", i)

		var links []string
		if i > 0 {
			links = []string{fmt.Sprintf("chain-%d", i-1)}
		}

		container := &SimpleContainer{
			name:  name,
			id:    fmt.Sprintf("chain-id-%d", i),
			links: links,
		}

		containers[i] = container
	}

	return containers
}

// generateDiamondDependencies creates a diamond-shaped dependency graph.
func generateDiamondDependencies(levels int) []types.Container {
	var containers []types.Container

	// Create root
	root := &SimpleContainer{
		name:  "diamond-root",
		id:    "diamond-id-root",
		links: []string{},
	}
	containers = append(containers, root)

	// Create levels
	for level := 1; level <= levels; level++ {
		nodesInLevel := level * 2 // Exponential growth

		for i := range nodesInLevel {
			name := fmt.Sprintf("diamond-l%d-n%d", level, i)

			var links []string

			// Connect to all nodes in previous level
			if level == 1 {
				links = []string{"diamond-root"}
			} else {
				for j := range (level - 1) * 2 {
					links = append(links, fmt.Sprintf("diamond-l%d-n%d", level-1, j))
				}
			}

			container := &SimpleContainer{
				name:  name,
				id:    fmt.Sprintf("diamond-id-l%d-n%d", level, i),
				links: links,
			}

			containers = append(containers, container)
		}
	}

	return containers
}

// Benchmark Kahn's algorithm sorting performance.
func BenchmarkDependencySorter(b *testing.B) {
	testCases := []struct {
		name        string
		containers  func() []types.Container
		description string
	}{
		{
			name:        "Small_NoDeps",
			containers:  func() []types.Container { return generateBenchmarkContainers(10, 0.0) },
			description: "10 containers with no dependencies",
		},
		{
			name:        "Small_LowDeps",
			containers:  func() []types.Container { return generateBenchmarkContainers(10, 0.3) },
			description: "10 containers with low dependency factor",
		},
		{
			name:        "Medium_NoDeps",
			containers:  func() []types.Container { return generateBenchmarkContainers(100, 0.0) },
			description: "100 containers with no dependencies",
		},
		{
			name:        "Medium_LowDeps",
			containers:  func() []types.Container { return generateBenchmarkContainers(100, 0.3) },
			description: "100 containers with low dependency factor",
		},
		{
			name:        "Medium_HighDeps",
			containers:  func() []types.Container { return generateBenchmarkContainers(100, 0.8) },
			description: "100 containers with high dependency factor",
		},
		{
			name:        "Large_NoDeps",
			containers:  func() []types.Container { return generateBenchmarkContainers(1000, 0.0) },
			description: "1000 containers with no dependencies",
		},
		{
			name:        "Large_LowDeps",
			containers:  func() []types.Container { return generateBenchmarkContainers(1000, 0.3) },
			description: "1000 containers with low dependency factor",
		},
		{
			name:        "Chain_100",
			containers:  func() []types.Container { return generateChainDependencies(100) },
			description: "Linear dependency chain of 100 containers",
		},
		{
			name:        "Chain_500",
			containers:  func() []types.Container { return generateChainDependencies(500) },
			description: "Linear dependency chain of 500 containers",
		},
		{
			name:        "Diamond_3Levels",
			containers:  func() []types.Container { return generateDiamondDependencies(3) },
			description: "Diamond dependency graph with 3 levels",
		},
		{
			name:        "Diamond_4Levels",
			containers:  func() []types.Container { return generateDiamondDependencies(4) },
			description: "Diamond dependency graph with 4 levels",
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			containers := tc.containers()

			b.ResetTimer()

			for range b.N {
				ds := DependencySorter{}
				// Create a copy for each iteration to avoid modifying the original
				testContainers := make([]types.Container, len(containers))
				copy(testContainers, containers)

				err := ds.Sort(testContainers)
				if err != nil {
					b.Fatalf("Unexpected error in benchmark %s: %v", tc.name, err)
				}
			}
		})
	}
}

// Benchmark cycle detection specifically.
func BenchmarkCycleDetection(b *testing.B) {
	testCases := []struct {
		name        string
		containers  func() []types.Container
		description string
		hasCycle    bool
	}{
		{
			name:        "NoCycle_Small",
			containers:  func() []types.Container { return generateBenchmarkContainers(50, 0.2) },
			description: "50 containers, no cycles",
			hasCycle:    false,
		},
		{
			name:        "NoCycle_Large",
			containers:  func() []types.Container { return generateBenchmarkContainers(500, 0.1) },
			description: "500 containers, no cycles",
			hasCycle:    false,
		},
		{
			name: "Cycle_Simple",
			containers: func() []types.Container {
				c1 := &SimpleContainer{name: "c1", id: "id1", links: []string{"c2"}}
				c2 := &SimpleContainer{name: "c2", id: "id2", links: []string{"c1"}}

				return []types.Container{c1, c2}
			},
			description: "Simple 2-node cycle",
			hasCycle:    true,
		},
		{
			name: "Cycle_Complex",
			containers: func() []types.Container {
				c1 := &SimpleContainer{name: "c1", id: "id1", links: []string{"c2"}}
				c2 := &SimpleContainer{name: "c2", id: "id2", links: []string{"c3"}}
				c3 := &SimpleContainer{name: "c3", id: "id3", links: []string{"c1"}}
				c4 := &SimpleContainer{name: "c4", id: "id4", links: []string{}}

				return []types.Container{c1, c2, c3, c4}
			},
			description: "3-node cycle with additional node",
			hasCycle:    true,
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			containers := tc.containers()

			b.ResetTimer()

			for range b.N {
				ds := DependencySorter{}
				testContainers := make([]types.Container, len(containers))
				copy(testContainers, containers)

				err := ds.Sort(testContainers)
				if tc.hasCycle && err == nil {
					b.Fatalf("Expected cycle detection error in benchmark %s", tc.name)
				}

				if !tc.hasCycle && err != nil {
					b.Fatalf("Unexpected error in benchmark %s: %v", tc.name, err)
				}
			}
		})
	}
}

// Benchmark memory usage patterns.
func BenchmarkMemoryUsage(b *testing.B) {
	b.Run("MemoryEfficiency", func(b *testing.B) {
		containers := generateBenchmarkContainers(1000, 0.5)

		b.ResetTimer()

		b.ReportAllocs()

		for range b.N {
			ds := DependencySorter{}
			testContainers := make([]types.Container, len(containers))
			copy(testContainers, containers)

			err := ds.Sort(testContainers)
			if err != nil {
				b.Fatalf("Unexpected error: %v", err)
			}
		}
	})
}

// Benchmark sort stability - ensure consistent results for equivalent inputs.
func BenchmarkSortStability(b *testing.B) {
	containers := generateBenchmarkContainers(100, 0.3)

	b.Run("StabilityCheck", func(b *testing.B) {
		results := make([][]string, b.N)

		for i := range b.N {
			ds := DependencySorter{}
			testContainers := make([]types.Container, len(containers))
			copy(testContainers, containers)

			err := ds.Sort(testContainers)
			if err != nil {
				b.Fatalf("Unexpected error: %v", err)
			}

			names := make([]string, len(testContainers))
			for j, c := range testContainers {
				names[j] = c.Name()
			}

			results[i] = names
		}

		// Check stability - all results should be identical
		for i := 1; i < b.N; i++ {
			if len(results[0]) != len(results[i]) {
				b.Fatalf("Inconsistent result lengths: %d vs %d", len(results[0]), len(results[i]))
			}

			for j := range results[0] {
				if results[0][j] != results[i][j] {
					b.Fatalf(
						"Sort not stable at position %d: %s vs %s",
						j,
						results[0][j],
						results[i][j],
					)
				}
			}
		}
	})
}

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

	parts := bytes.Split(data, []byte(","))
	for _, part := range parts {
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

		container := &SimpleContainer{
			name:  node,
			id:    fmt.Sprintf("fuzz-id-%d", i),
			links: links,
		}
		containers = append(containers, container)
		i++
	}

	return containers
}
