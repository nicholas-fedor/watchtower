// Package mocks provides mock implementations for testing Watchtower components.
package mocks

import (
	"errors"
	"fmt"
	"time"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// MockClient is a mock implementation of a Watchtower Client for testing purposes.
// It simulates container operations with configurable behavior defined by TestData.
type MockClient struct {
	TestData      *TestData
	pullImages    bool
	removeVolumes bool
	Stopped       map[string]bool // Track stopped containers by ID.
}

// TestData holds configuration data for MockClient’s test behavior.
// It defines container states, staleness, and mock operation results.
type TestData struct {
	TriedToRemoveImageCount int               // Number of times RemoveImageByID was called.
	NameOfContainerToKeep   string            // Name of the container to avoid stopping.
	Containers              []types.Container // List of mock containers.
	Staleness               map[string]bool   // Map of container names to staleness status.
}

// TriedToRemoveImage checks if RemoveImageByID has been invoked.
// It returns true if the count is greater than zero, aiding test assertions.
func (testdata *TestData) TriedToRemoveImage() bool {
	return testdata.TriedToRemoveImageCount > 0
}

// CreateMockClient constructs a new MockClient instance for testing.
// It initializes the client with provided test data, pull and volume removal flags,
// and an empty map for tracking stopped containers.
func CreateMockClient(data *TestData, pullImages bool, removeVolumes bool) MockClient {
	return MockClient{
		TestData:      data,
		pullImages:    pullImages,
		removeVolumes: removeVolumes,
		Stopped:       make(map[string]bool),
	}
}

// ListContainers returns the preconfigured list of containers from TestData.
// It ignores the filter parameter, providing all containers for test simplicity.
func (client MockClient) ListContainers(_ types.Filter) ([]types.Container, error) {
	return client.TestData.Containers, nil
}

// StopContainer simulates stopping a container by marking it in the Stopped map.
// It records the container’s ID as stopped and always returns nil for simplicity.
func (client MockClient) StopContainer(c types.Container, _ time.Duration) error {
	client.Stopped[string(c.ID())] = true

	return nil
}

// IsContainerRunning checks if a container is running based on the Stopped map.
// It returns true if the container’s ID is not marked as stopped, false otherwise.
func (client MockClient) IsContainerRunning(c types.Container) bool {
	return !client.Stopped[string(c.ID())]
}

// StartContainer simulates starting a container, returning an empty ID and no error.
// It provides a minimal implementation for testing purposes.
func (client MockClient) StartContainer(_ types.Container) (types.ContainerID, error) {
	return "", nil
}

// RenameContainer simulates renaming a container, always succeeding with no action.
// It returns nil to indicate success without modifying any state.
func (client MockClient) RenameContainer(_ types.Container, _ string) error {
	return nil
}

// RemoveImageByID increments the count of image removal attempts in TestData.
// It simulates image cleanup and always returns nil to indicate success.
func (client MockClient) RemoveImageByID(_ types.ImageID) error {
	client.TestData.TriedToRemoveImageCount++

	return nil
}

// GetContainer returns the first container from TestData, ignoring the provided ID.
// It provides a simple mock response for testing container retrieval.
func (client MockClient) GetContainer(_ types.ContainerID) (types.Container, error) {
	return client.TestData.Containers[0], nil
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
func (client MockClient) ExecuteCommand(_ types.ContainerID, command string, _ int) (bool, error) {
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
func (client MockClient) IsContainerStale(
	cont types.Container,
	_ types.UpdateParams,
) (bool, types.ImageID, error) {
	stale, found := client.TestData.Staleness[cont.Name()]
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
