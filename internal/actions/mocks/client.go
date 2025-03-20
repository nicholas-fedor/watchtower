package mocks

import (
	"errors"
	"time"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// MockClient is a mock that passes as a watchtower Client.
type MockClient struct {
	TestData      *TestData
	pullImages    bool
	removeVolumes bool
	Stopped       map[string]bool // Track stopped containers
}

// TestData is the data used to perform the test.
type TestData struct {
	TriedToRemoveImageCount int
	NameOfContainerToKeep   string
	Containers              []types.Container
	Staleness               map[string]bool
}

// TriedToRemoveImage is a test helper function to check whether RemoveImageByID has been called.
func (testdata *TestData) TriedToRemoveImage() bool {
	return testdata.TriedToRemoveImageCount > 0
}

// CreateMockClient creates a mock watchtower Client for usage in tests.
func CreateMockClient(data *TestData, pullImages bool, removeVolumes bool) MockClient {
	return MockClient{
		data,
		pullImages,
		removeVolumes,
		make(map[string]bool),
	}
}

// ListContainers is a mock method returning the provided container testdata.
func (client MockClient) ListContainers(_ types.Filter) ([]types.Container, error) {
	return client.TestData.Containers, nil
}

// StopContainer is a mock method.
func (client MockClient) StopContainer(c types.Container, _ time.Duration) error {
	client.Stopped[string(c.ID())] = true

	return nil
}

func (client MockClient) IsContainerRunning(c types.Container) bool {
	return !client.Stopped[string(c.ID())]
}

// StartContainer is a mock method.
func (client MockClient) StartContainer(_ types.Container) (types.ContainerID, error) {
	return "", nil
}

// RenameContainer is a mock method.
func (client MockClient) RenameContainer(_ types.Container, _ string) error {
	return nil
}

// RemoveImageByID increments the TriedToRemoveImageCount on being called.
func (client MockClient) RemoveImageByID(_ types.ImageID) error {
	client.TestData.TriedToRemoveImageCount++

	return nil
}

// GetContainer is a mock method.
func (client MockClient) GetContainer(_ types.ContainerID) (types.Container, error) {
	return client.TestData.Containers[0], nil
}

// ExecuteCommand is a mock method.
func (client MockClient) ExecuteCommand(_ types.ContainerID, command string, _ int) (SkipUpdate bool, err error) {
	switch command {
	case "/PreUpdateReturn0.sh":
		return false, nil
	case "/PreUpdateReturn1.sh":
		return false, errors.New("command exited with code 1")
	case "/PreUpdateReturn75.sh":
		return true, nil
	default:
		return false, nil
	}
}

// IsContainerStale is true if not explicitly stated in TestData for the mock client.
func (client MockClient) IsContainerStale(cont types.Container, params types.UpdateParams) (bool, types.ImageID, error) {
	stale, found := client.TestData.Staleness[cont.Name()]
	if !found {
		stale = true
	}

	return stale, "", nil
}

// WarnOnHeadPullFailed is always true for the mock client.
func (client MockClient) WarnOnHeadPullFailed(_ types.Container) bool {
	return true
}
