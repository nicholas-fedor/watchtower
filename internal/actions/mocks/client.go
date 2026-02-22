// Package mocks provides mock implementations for testing Watchtower components.
package mocks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	dockerContainer "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// MockClient is a mock implementation of a Watchtower Client for testing purposes.
// It simulates container operations with configurable behavior defined by TestData.
type MockClient struct {
	TestData      *TestData
	pullImages    bool
	removeVolumes bool
	Stopped       map[string]bool // Track stopped containers by ID.
	ctx           context.Context // Context for cancellation simulation
}

// TestData holds configuration data for MockClient’s test behavior.
// It defines container states, staleness, and mock operation results.
type TestData struct {
	TriedToRemoveImageCount      int                                   // Number of times RemoveImageByID was called.
	RenameContainerCount         int                                   // Number of times RenameContainer was called.
	StopContainerCount           int                                   // Number of times StopContainer was called.
	StartContainerCount          int                                   // Number of times StartContainer was called.
	UpdateContainerCount         int                                   // Number of times UpdateContainer was called.
	IsContainerStaleCount        int                                   // Number of times IsContainerStale was called.
	WaitForContainerHealthyCount int                                   // Number of times WaitForContainerHealthy was called.
	NameOfContainerToKeep        string                                // Name of the container to avoid stopping.
	Containers                   []types.Container                     // List of mock containers.
	ContainersByID               map[types.ContainerID]types.Container // Map of containers by ID.
	Staleness                    map[string]bool                       // Map of container names to staleness status.
	IsContainerStaleError        error                                 // Error to return from IsContainerStale (for testing).
	ListContainersError          error                                 // Error to return from ListContainers (for testing).
	StopContainerError           error                                 // Error to return from StopContainer (for testing).
	StartContainerError          error                                 // Error to return from StartContainer (for testing).
	UpdateContainerError         error                                 // Error to return from UpdateContainer (for testing).
	StopContainerFailCount       int                                   // Number of times StopContainer should fail before succeeding.
	RemoveImageError             error                                 // Error to return from RemoveImageByID (for testing).
	FailedImageIDs               []types.ImageID                       // List of image IDs that should fail removal.
	StopOrder                    []string                              // Order in which containers were stopped.
	StartOrder                   []string                              // Order in which containers were started.
	SimulatedLatency             time.Duration                         // Simulated latency for operations (default 0 for fast tests, set for context cancellation tests).
}

// TriedToRemoveImage checks if RemoveImageByID has been invoked.
// It returns true if the count is greater than zero, aiding test assertions.
func (testdata *TestData) TriedToRemoveImage() bool {
	return testdata.TriedToRemoveImageCount > 0
}

// CreateMockClient constructs a new MockClient instance for testing.
// It initializes the client with provided test data, pull and volume removal flags,
// and an empty map for tracking stopped containers.
func CreateMockClient(data *TestData, pullImages, removeVolumes bool) MockClient {
	return CreateMockClientWithContext(context.Background(), data, pullImages, removeVolumes)
}

// CreateMockClientWithContext constructs a new MockClient instance with context for testing.
// It initializes the client with provided context, test data, pull and volume removal flags,
// and an empty map for tracking stopped containers.
func CreateMockClientWithContext(
	ctx context.Context,
	data *TestData,
	pullImages bool,
	removeVolumes bool,
) MockClient {
	if data.ContainersByID == nil {
		data.ContainersByID = make(map[types.ContainerID]types.Container)
	}
	for _, c := range data.Containers {
		data.ContainersByID[c.ID()] = c
	}

	return MockClient{
		TestData:      data,
		pullImages:    pullImages,
		removeVolumes: removeVolumes,
		Stopped:       make(map[string]bool),
		ctx:           ctx,
	}
}

// ListContainers returns containers from TestData, optionally filtered.
func (client MockClient) ListContainers(ctx context.Context, filter ...types.Filter) ([]types.Container, error) {
	// Simulate latency for context cancellation testing when configured
	if client.TestData.SimulatedLatency > 0 {
		time.Sleep(client.TestData.SimulatedLatency)
	}

	// Check passed context for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Also check client.ctx for backward compatibility
	if client.ctx != nil {
		select {
		case <-client.ctx.Done():
			return nil, client.ctx.Err()
		default:
		}
	}

	if client.TestData.ListContainersError != nil {
		return nil, client.TestData.ListContainersError
	}

	containers := client.TestData.Containers

	if len(filter) > 0 && filter[0] != nil {
		filtered := []types.Container{}

		for _, c := range containers {
			if filter[0](c) {
				filtered = append(filtered, c)
			}
		}

		return filtered, nil
	}

	return containers, nil
}

// StopContainer simulates stopping a container by marking it in the Stopped map.
// It records the container’s ID as stopped, increments the StopContainerCount,
// and returns nil for simplicity.
func (client MockClient) StopContainer(ctx context.Context, c types.Container, _ time.Duration) error {
	client.TestData.StopContainerCount++

	// Simulate latency for context cancellation testing when configured
	if client.TestData.SimulatedLatency > 0 {
		time.Sleep(client.TestData.SimulatedLatency)
	}

	// Check passed context for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Also check client.ctx for backward compatibility
	if client.ctx != nil {
		select {
		case <-client.ctx.Done():
			return client.ctx.Err()
		default:
		}
	}

	if client.TestData.StopContainerError != nil &&
		client.TestData.StopContainerCount <= client.TestData.StopContainerFailCount {
		return client.TestData.StopContainerError
	}

	client.Stopped[string(c.ID())] = true
	client.TestData.StopOrder = append(client.TestData.StopOrder, c.Name())

	return nil
}

// StopAndRemoveContainer simulates stopping and removing a container by calling StopContainer followed by RemoveContainer.
// It properly simulates the stop-and-remove operation sequence while respecting error conditions.
func (client MockClient) StopAndRemoveContainer(ctx context.Context, c types.Container, timeout time.Duration) error {
	err := client.StopContainer(ctx, c, timeout)
	if err != nil {
		return err
	}

	return client.RemoveContainer(ctx, c)
}

// IsContainerRunning checks if a container is running based on the Stopped map.
// It returns true if the container’s ID is not marked as stopped, false otherwise.
func (client MockClient) IsContainerRunning(c types.Container) bool {
	return !client.Stopped[string(c.ID())]
}

// StartContainer simulates starting a container, returning the container's ID.
// It provides a minimal implementation for testing purposes.
// Returns the configured StartContainerError if set.
func (client MockClient) StartContainer(_ context.Context, c types.Container) (types.ContainerID, error) {
	client.TestData.StartContainerCount++

	// Simulate latency for context cancellation testing when configured
	if client.TestData.SimulatedLatency > 0 {
		time.Sleep(client.TestData.SimulatedLatency)
	}

	if client.ctx != nil {
		select {
		case <-client.ctx.Done():
			return "", client.ctx.Err()
		default:
		}
	}

	if client.TestData.StartContainerError != nil {
		return "", client.TestData.StartContainerError
	}

	client.TestData.StartOrder = append(client.TestData.StartOrder, c.Name())

	return c.ID(), nil
}

// RenameContainer simulates renaming a container, incrementing the RenameContainerCount.
// It returns nil to indicate success without modifying any state.
func (client MockClient) RenameContainer(_ context.Context, _ types.Container, _ string) error {
	client.TestData.RenameContainerCount++

	return nil
}

// UpdateContainer simulates updating a container's configuration.
// It increments the UpdateContainerCount and returns the configured error if set.
func (client MockClient) UpdateContainer(_ context.Context, _ types.Container, _ dockerContainer.UpdateConfig) error {
	client.TestData.UpdateContainerCount++

	return client.TestData.UpdateContainerError
}

// RemoveImageByID increments the count of image removal attempts in TestData.
// It simulates image cleanup and returns configured error if set or if image ID is in FailedImageIDs, nil otherwise.
func (client MockClient) RemoveImageByID(_ context.Context, imageID types.ImageID, _ string) error {
	client.TestData.TriedToRemoveImageCount++
	if slices.Contains(client.TestData.FailedImageIDs, imageID) {
		return client.TestData.RemoveImageError
	}

	return nil
}

// GetContainer returns the container with the specified ID from TestData.
// It provides a mock response for testing container retrieval.
func (client MockClient) GetContainer(_ context.Context, containerID types.ContainerID) (types.Container, error) {
	if c, ok := client.TestData.ContainersByID[containerID]; ok {
		return c, nil
	}
	return nil, fmt.Errorf("container not found: %s", containerID)
}

// GetCurrentWatchtowerContainer returns the container with the specified ID from TestData with imageInfo set to nil.
// It provides a mock response for testing current Watchtower container retrieval.
func (client MockClient) GetCurrentWatchtowerContainer(_ context.Context, containerID types.ContainerID) (types.Container, error) {
	if c, ok := client.TestData.ContainersByID[containerID]; ok {
		// Return a copy with imageInfo nil
		// Since it's a mock, just return the same container
		return c, nil
	}
	return nil, fmt.Errorf("container not found: %s", containerID)
}

// GetVersion returns a mock Docker API client version.
// It provides a static version string for testing purposes.
func (client MockClient) GetVersion() string {
	return "1.50"
}

// errCommandFailed is a static error indicating a command exited with a non-zero code.
// It is used in ExecuteCommand to provide consistent error reporting for test scenarios.
var errCommandFailed = errors.New("command exited with non-zero code")

// ExecuteCommand simulates executing a command in a container for testing lifecycle hooks.
// It returns a SkipUpdate boolean indicating whether to skip the update and an error if the command fails.
// The method uses predefined command behaviors to mimic real execution outcomes.
func (client MockClient) ExecuteCommand(
	_ context.Context,
	_ types.Container,
	command string,
	_ int,
	_ int,
	_ int,
) (bool, error) {
	switch command {
	case "/PreUpdateReturn0.sh":
		return false, nil // Command succeeds (exit 0), no skip.
	case "/PreUpdateReturn1.sh":
		return false, fmt.Errorf(
			"%w: %s",
			errCommandFailed,
			"code 1",
		) // Command fails (exit 1), no skip.
	case "/PreUpdateReturn75.sh":
		return true, nil // Command succeeds (exit 75), signals skip.
	default:
		return false, nil // Unknown commands succeed (exit 0), no skip.
	}
}

// IsContainerStale determines if a container is stale based on TestData’s Staleness map.
// It returns true if the container’s name isn’t explicitly marked as fresh, along with an empty ImageID and no error.
// If IsContainerStaleError is set, it returns that error instead.
func (client MockClient) IsContainerStale(
	ctx context.Context,
	container types.Container,
	_ types.UpdateParams,
) (bool, types.ImageID, error) {
	client.TestData.IsContainerStaleCount++

	// Simulate latency for context cancellation testing when configured
	if client.TestData.SimulatedLatency > 0 {
		time.Sleep(client.TestData.SimulatedLatency)
	}

	// Check passed context for cancellation
	select {
	case <-ctx.Done():
		return false, "", ctx.Err()
	default:
	}

	// Also check client.ctx for backward compatibility
	if client.ctx != nil {
		select {
		case <-client.ctx.Done():
			return false, "", client.ctx.Err()
		default:
		}
	}

	// Return configured error if set (for testing error conditions)
	if client.TestData.IsContainerStaleError != nil {
		return false, "", client.TestData.IsContainerStaleError
	}

	stale, found := client.TestData.Staleness[container.Name()]
	if !found {
		stale = true // Default to stale if not specified.
	}

	return stale, "", nil
}

// WarnOnHeadPullFailed always returns true for the mock client.
// It simulates a warning condition for HEAD pull failures in tests.
func (client MockClient) WarnOnHeadPullFailed(_ types.Container) bool {
	return true
}

// WaitForContainerHealthy simulates waiting for a container to become healthy.
// It increments the count and returns nil to indicate success.
func (client MockClient) WaitForContainerHealthy(_ context.Context, _ types.ContainerID, _ time.Duration) error {
	client.TestData.WaitForContainerHealthyCount++

	return nil
}

// RemoveContainer simulates removing a container.
// It returns nil to indicate success.
func (client MockClient) RemoveContainer(_ context.Context, _ types.Container) error {
	return nil
}

// GetInfo returns mock system information for testing.
// It provides a basic map with mock Docker/Podman info.
func (client MockClient) GetInfo(_ context.Context) (map[string]any, error) {
	return map[string]any{
		"Name":          "docker",
		"ServerVersion": "1.50",
		"OSType":        "linux",
	}, nil
}
