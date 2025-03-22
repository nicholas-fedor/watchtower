// Package lifecycle provides tests for the lifecycle hook execution logic in Watchtower.
// It verifies the behavior of pre-check, post-check, pre-update, and post-update commands
// under various conditions, including success, errors, and edge cases.
package lifecycle

import (
	"errors"
	"testing"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockClient is a manual mock for the Client interface.
type mockClient struct {
	mock.Mock
}

func (m *mockClient) ListContainers(filter types.Filter) ([]types.Container, error) {
	args := m.Called(filter)
	containers, ok := args.Get(0).([]types.Container)

	if !ok && args.Get(0) != nil {
		panic("invalid type assertion for ListContainers")
	}

	if err := args.Error(1); err != nil {
		return nil, err
	}

	return containers, nil
}

func (m *mockClient) GetContainer(containerID types.ContainerID) (types.Container, error) {
	args := m.Called(containerID)
	container, ok := args.Get(0).(types.Container)

	if !ok && args.Get(0) != nil {
		panic("invalid type assertion for GetContainer")
	}

	err := args.Error(1)
	if err != nil {
		return nil, err
	}

	return container, nil
}

func (m *mockClient) StopContainer(container types.Container, timeout time.Duration) error {
	return m.Called(container, timeout).Error(0)
}

func (m *mockClient) StartContainer(container types.Container) (types.ContainerID, error) {
	args := m.Called(container)
	containerID, ok := args.Get(0).(types.ContainerID)

	if !ok && args.Get(0) != nil {
		panic("invalid type assertion for StartContainer")
	}

	err := args.Error(1)
	if err != nil {
		return "", err
	}

	return containerID, nil
}

func (m *mockClient) RenameContainer(container types.Container, newName string) error {
	return m.Called(container, newName).Error(0)
}

func (m *mockClient) IsContainerStale(container types.Container, params types.UpdateParams) (bool, types.ImageID, error) {
	args := m.Called(container, params)
	imageID, _ := args.Get(1).(types.ImageID) // Nil is valid

	err := args.Error(2)
	if err != nil {
		return false, "", err
	}

	return args.Bool(0), imageID, nil
}

func (m *mockClient) ExecuteCommand(containerID types.ContainerID, command string, timeout int) (bool, error) {
	args := m.Called(containerID, command, timeout)

	err := args.Error(1)
	if err != nil {
		return false, err
	}

	return args.Bool(0), nil
}

func (m *mockClient) RemoveImageByID(imageID types.ImageID) error {
	return m.Called(imageID).Error(0)
}

func (m *mockClient) WarnOnHeadPullFailed(container types.Container) bool {
	return m.Called(container).Bool(0)
}

// mockContainer creates a *container.Container for testing.
//
//nolint:exhaustruct // Omit fields irrelevant to tests
func mockContainer(options ...func(*container.Container)) *container.Container {
	c := container.NewContainer(
		&dockercontainer.InspectResponse{
			ContainerJSONBase: &dockercontainer.ContainerJSONBase{
				ID:         "container_id",
				HostConfig: &dockercontainer.HostConfig{},
				Name:       "/test-container", // Ensure valid name
			},
			Config: &dockercontainer.Config{
				Labels: map[string]string{},
			},
		},
		nil, // No image info needed for these tests
	)
	// Apply default state to avoid nil pointer issues
	c.ContainerInfo().State = &dockercontainer.State{Running: false}

	for _, opt := range options {
		opt(c)
	}

	return c
}

// withLabels sets labels on a container.
func withLabels(labels map[string]string) func(*container.Container) {
	return func(c *container.Container) {
		c.ContainerInfo().Config.Labels = labels
	}
}

// withContainerState sets the state on a container.
func withContainerState(state dockercontainer.State) func(*container.Container) {
	return func(c *container.Container) {
		c.ContainerInfo().State = &state
	}
}

var (
	errListingFailed = errors.New("listing failed")
	errExecFailed    = errors.New("exec failed")
	errNotFound      = errors.New("not found")
)

// TestExecutePreChecks tests the ExecutePreChecks function.
// It verifies that pre-check commands are executed for all filtered containers,
// handles listing errors gracefully, and logs appropriately.
//
//nolint:paralleltest,exhaustruct // Omit fields irrelevant to tests
func TestExecutePreChecks(t *testing.T) {
	tests := []struct {
		name           string
		setupClient    func(*mockClient)
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name: "successful execution",
			setupClient: func(c *mockClient) {
				c.On("ListContainers", mock.Anything).Return([]types.Container{
					mockContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.pre-check": "pre-check",
					})),
					mockContainer(),
				}, nil)
				c.On("ExecuteCommand", types.ContainerID("container_id"), "pre-check", 1).Return(true, nil)
			},
			expectedLogs:   2,
			expectedLogMsg: "Executing pre-check command",
		},
		{
			name: "listing error",
			setupClient: func(c *mockClient) {
				c.On("ListContainers", mock.Anything).Return(nil, errListingFailed)
			},
			expectedLogs:   1,
			expectedLogMsg: "Failed to list containers for pre-checks",
		},
	}

	//nolint:paralleltest
	for _, testClient := range tests {
		t.Run(testClient.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)

			client := new(mockClient)
			testClient.setupClient(client)
			hook.Reset()

			ExecutePreChecks(client, types.UpdateParams{
				Filter:         func(types.FilterableContainer) bool { return true },
				LifecycleHooks: true,
			})

			assert.Len(t, hook.Entries, testClient.expectedLogs, "log entry count mismatch")

			if len(hook.Entries) > 0 {
				assert.Contains(t, hook.Entries[0].Message, testClient.expectedLogMsg, "first log message mismatch")
			} else {
				t.Errorf("No log entries captured; expected %d with message %q", testClient.expectedLogs, testClient.expectedLogMsg)
			}

			client.AssertExpectations(t)
			hook.Reset()
		})
	}
}

// TestExecutePostChecks tests the ExecutePostChecks function.
// It verifies that post-check commands are executed for all filtered containers,
// handles listing errors gracefully, and logs appropriately.
//
//nolint:paralleltest,exhaustruct // Omit fields irrelevant to tests
func TestExecutePostChecks(t *testing.T) {
	tests := []struct {
		name           string
		setupClient    func(*mockClient)
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name: "successful execution",
			setupClient: func(c *mockClient) {
				c.On("ListContainers", mock.Anything).Return([]types.Container{
					mockContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.post-check": "post-check",
					})),
					mockContainer(),
				}, nil)
				c.On("ExecuteCommand", types.ContainerID("container_id"), "post-check", 1).Return(true, nil)
			},
			expectedLogs:   2,
			expectedLogMsg: "Executing post-check command",
		},
		{
			name: "listing error",
			setupClient: func(c *mockClient) {
				c.On("ListContainers", mock.Anything).Return(nil, errListingFailed)
			},
			expectedLogs:   1,
			expectedLogMsg: "Failed to list containers for post-checks",
		},
	}

	//nolint:paralleltest
	for _, testClient := range tests {
		t.Run(testClient.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)

			client := new(mockClient)
			testClient.setupClient(client)
			hook.Reset()

			ExecutePostChecks(client, types.UpdateParams{
				Filter:         func(types.FilterableContainer) bool { return true },
				LifecycleHooks: true,
			})

			assert.Len(t, hook.Entries, testClient.expectedLogs)

			if len(hook.Entries) > 0 {
				assert.Contains(t, hook.Entries[0].Message, testClient.expectedLogMsg)
			} else {
				t.Errorf("No log entries captured; expected %d with message %q", testClient.expectedLogs, testClient.expectedLogMsg)
			}

			client.AssertExpectations(t)
			hook.Reset()
		})
	}
}

// TestExecutePreCheckCommand tests the ExecutePreCheckCommand function.
// It ensures the pre-check command is executed when present, skipped when absent,
// and errors are logged correctly.
//
//nolint:paralleltest,exhaustruct,dupl // Omit fields irrelevant to tests
func TestExecutePreCheckCommand(t *testing.T) {
	tests := []struct {
		name           string
		container      types.Container
		setupClient    func(*mockClient)
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name: "command present",
			container: mockContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.pre-check": "pre-check",
			})),
			setupClient: func(c *mockClient) {
				c.On("ExecuteCommand", types.ContainerID("container_id"), "pre-check", 1).Return(true, nil)
			},
			expectedLogs:   1,
			expectedLogMsg: "Executing pre-check command",
		},
		{
			name:           "no command",
			container:      mockContainer(),
			expectedLogs:   1,
			expectedLogMsg: "No pre-check command supplied",
		},
		{
			name: "command error",
			container: mockContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.pre-check": "pre-check",
			})),
			setupClient: func(c *mockClient) {
				c.On("ExecuteCommand", types.ContainerID("container_id"), "pre-check", 1).Return(false, errExecFailed)
			},
			expectedLogs:   2,
			expectedLogMsg: "Pre-check command failed",
		},
	}

	//nolint:paralleltest
	for _, testClient := range tests {
		t.Run(testClient.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)

			client := new(mockClient)
			if testClient.setupClient != nil {
				testClient.setupClient(client)
			}

			hook.Reset()

			ExecutePreCheckCommand(client, testClient.container)

			assert.Len(t, hook.Entries, testClient.expectedLogs)

			if len(hook.Entries) > 0 {
				assert.Contains(t, hook.LastEntry().Message, testClient.expectedLogMsg)
			} else {
				t.Errorf("No log entries captured; expected %d with message %q", testClient.expectedLogs, testClient.expectedLogMsg)
			}

			client.AssertExpectations(t)
			hook.Reset()
		})
	}
}

// TestExecutePostCheckCommand tests the ExecutePostCheckCommand function.
// It ensures the post-check command is executed when present, skipped when absent,
// and errors are logged correctly.
//
//nolint:paralleltest,exhaustruct,dupl // Omit fields irrelevant to tests
func TestExecutePostCheckCommand(t *testing.T) {
	tests := []struct {
		name           string
		container      types.Container
		setupClient    func(*mockClient)
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name: "command present",
			container: mockContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.post-check": "post-check",
			})),
			setupClient: func(c *mockClient) {
				c.On("ExecuteCommand", types.ContainerID("container_id"), "post-check", 1).Return(true, nil)
			},
			expectedLogs:   1,
			expectedLogMsg: "Executing post-check command",
		},
		{
			name:           "no command",
			container:      mockContainer(),
			expectedLogs:   1,
			expectedLogMsg: "No post-check command supplied",
		},
		{
			name: "command error",
			container: mockContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.post-check": "post-check",
			})),
			setupClient: func(c *mockClient) {
				c.On("ExecuteCommand", types.ContainerID("container_id"), "post-check", 1).Return(false, errExecFailed)
			},
			expectedLogs:   2,
			expectedLogMsg: "Post-check command failed",
		},
	}

	//nolint:paralleltest
	for _, testClient := range tests {
		t.Run(testClient.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)

			client := new(mockClient)
			if testClient.setupClient != nil {
				testClient.setupClient(client)
			}

			hook.Reset()

			ExecutePostCheckCommand(client, testClient.container)

			assert.Len(t, hook.Entries, testClient.expectedLogs)

			if len(hook.Entries) > 0 {
				assert.Contains(t, hook.LastEntry().Message, testClient.expectedLogMsg)
			} else {
				t.Errorf("No log entries captured; expected %d with message %q", testClient.expectedLogs, testClient.expectedLogMsg)
			}

			client.AssertExpectations(t)
			hook.Reset()
		})
	}
}

// TestExecutePreUpdateCommand tests the ExecutePreUpdateCommand function.
// It verifies command execution, skipping for non-running containers, and error handling.
//
//nolint:paralleltest,exhaustruct // Omit fields irrelevant to tests
func TestExecutePreUpdateCommand(t *testing.T) {
	tests := []struct {
		name           string
		container      types.Container
		setupClient    func(*mockClient)
		expectedResult bool
		expectedErr    bool
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name: "command present and running",
			container: mockContainer(
				withContainerState(dockercontainer.State{Running: true}),
				withLabels(map[string]string{
					"com.centurylinklabs.watchtower.lifecycle.pre-update":         "pre-update",
					"com.centurylinklabs.watchtower.lifecycle.pre-update-timeout": "2",
				}),
			),
			setupClient: func(c *mockClient) {
				c.On("ExecuteCommand", types.ContainerID("container_id"), "pre-update", 2).Return(true, nil)
			},
			expectedResult: true,
			expectedLogs:   1,
			expectedLogMsg: "Executing pre-update command",
		},
		{
			name:           "no command",
			container:      mockContainer(withContainerState(dockercontainer.State{Running: true})),
			expectedResult: false,
			expectedLogs:   1,
			expectedLogMsg: "No pre-update command supplied",
		},
		{
			name: "not running",
			container: mockContainer(
				withContainerState(dockercontainer.State{Running: false}),
				withLabels(map[string]string{ // Add a command to reach the running check
					"com.centurylinklabs.watchtower.lifecycle.pre-update":         "pre-update",
					"com.centurylinklabs.watchtower.lifecycle.pre-update-timeout": "2",
				}),
			),
			expectedResult: false,
			expectedLogs:   1,
			expectedLogMsg: "Container is not running",
		},
		{
			name: "command error",
			container: mockContainer(
				withContainerState(dockercontainer.State{Running: true}),
				withLabels(map[string]string{
					"com.centurylinklabs.watchtower.lifecycle.pre-update":         "pre-update",
					"com.centurylinklabs.watchtower.lifecycle.pre-update-timeout": "2",
				}),
			),
			setupClient: func(c *mockClient) {
				c.On("ExecuteCommand", types.ContainerID("container_id"), "pre-update", 2).Return(false, errExecFailed)
			},
			expectedResult: true,
			expectedErr:    true,
			expectedLogs:   2,
			expectedLogMsg: "Pre-update command failed",
		},
	}

	//nolint:paralleltest
	for _, testClient := range tests {
		t.Run(testClient.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)

			client := new(mockClient)
			if testClient.setupClient != nil {
				testClient.setupClient(client)
			}

			hook.Reset()

			result, err := ExecutePreUpdateCommand(client, testClient.container)

			assert.Equal(t, testClient.expectedResult, result)

			if testClient.expectedErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "pre-update command execution failed")
			} else {
				assert.NoError(t, err) //nolint
			}

			assert.Len(t, hook.Entries, testClient.expectedLogs)

			if len(hook.Entries) > 0 {
				assert.Contains(t, hook.LastEntry().Message, testClient.expectedLogMsg)
			} else {
				t.Errorf("No log entries captured; expected %d with message %q", testClient.expectedLogs, testClient.expectedLogMsg)
			}

			client.AssertExpectations(t)
			hook.Reset()
		})
	}
}

// TestExecutePostUpdateCommand tests the ExecutePostUpdateCommand function.
// It verifies command execution, container retrieval errors, and logging behavior.
//
//nolint:paralleltest
func TestExecutePostUpdateCommand(t *testing.T) {
	tests := []struct {
		name           string
		containerID    types.ContainerID
		setupClient    func(*mockClient)
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name:        "command present",
			containerID: "test",
			setupClient: func(c *mockClient) {
				c.On("GetContainer", types.ContainerID("test")).Return(mockContainer(withLabels(map[string]string{
					"com.centurylinklabs.watchtower.lifecycle.post-update": "post-update",
				})), nil)
				c.On("ExecuteCommand", types.ContainerID("test"), "post-update", 1).Return(true, nil)
			},
			expectedLogs:   1,
			expectedLogMsg: "Executing post-update command",
		},
		{
			name:        "no command",
			containerID: "test",
			setupClient: func(c *mockClient) {
				c.On("GetContainer", types.ContainerID("test")).Return(mockContainer(), nil)
			},
			expectedLogs:   1,
			expectedLogMsg: "No post-update command supplied",
		},
		{
			name:        "container retrieval error",
			containerID: "test",
			setupClient: func(c *mockClient) {
				c.On("GetContainer", types.ContainerID("test")).Return(nil, errNotFound)
			},
			expectedLogs:   1,
			expectedLogMsg: "Failed to get container",
		},
		{
			name:        "command error",
			containerID: "test",
			setupClient: func(c *mockClient) {
				c.On("GetContainer", types.ContainerID("test")).Return(mockContainer(withLabels(map[string]string{
					"com.centurylinklabs.watchtower.lifecycle.post-update": "post-update",
				})), nil)
				c.On("ExecuteCommand", types.ContainerID("test"), "post-update", 1).Return(false, errExecFailed)
			},
			expectedLogs:   2,
			expectedLogMsg: "Post-update command failed",
		},
	}

	//nolint:paralleltest
	for _, testClient := range tests {
		t.Run(testClient.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)

			client := new(mockClient)
			testClient.setupClient(client)
			hook.Reset()

			ExecutePostUpdateCommand(client, testClient.containerID)

			assert.Len(t, hook.Entries, testClient.expectedLogs)

			if len(hook.Entries) > 0 {
				assert.Contains(t, hook.LastEntry().Message, testClient.expectedLogMsg)
			} else {
				t.Errorf("No log entries captured; expected %d with message %q", testClient.expectedLogs, testClient.expectedLogMsg)
			}

			client.AssertExpectations(t)
			hook.Reset()
		})
	}
}
