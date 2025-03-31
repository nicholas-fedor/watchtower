// Package lifecycle provides tests for the lifecycle hook execution logic in Watchtower.
// It verifies the behavior of pre-check, post-check, pre-update, and post-update commands
// under various conditions, including success, errors, and edge cases.
package lifecycle

import (
	"errors"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/container/mocks" // Add this import
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// mockContainer creates a *container.Container for testing.
func mockContainer(options ...func(*container.Container)) *container.Container {
	c := container.NewContainer(
		&dockerContainer.InspectResponse{
			ContainerJSONBase: &dockerContainer.ContainerJSONBase{
				ID:         "container_id",
				HostConfig: &dockerContainer.HostConfig{},
				Name:       "/test-container", // Ensure valid name
			},
			Config: &dockerContainer.Config{
				Labels: map[string]string{},
			},
		},
		nil, // No image info needed for these tests
	)
	// Apply default state to avoid nil pointer issues
	c.ContainerInfo().State = &dockerContainer.State{Running: false}

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
func withContainerState(state dockerContainer.State) func(*container.Container) {
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
func TestExecutePreChecks(t *testing.T) {
	tests := []struct {
		name           string
		setupClient    func(*mocks.MockClient)
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name: "successful execution",
			setupClient: func(c *mocks.MockClient) {
				c.On("ListContainers", mock.Anything).Return([]types.Container{
					mockContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.pre-check": "pre-check",
					})),
					mockContainer(),
				}, nil)
				c.On("ExecuteCommand", types.ContainerID("container_id"), "pre-check", 1).
					Return(true, nil)
			},
			expectedLogs:   2,
			expectedLogMsg: "Executing pre-check command",
		},
		{
			name: "listing error",
			setupClient: func(c *mocks.MockClient) {
				c.On("ListContainers", mock.Anything).Return(nil, errListingFailed)
			},
			expectedLogs:   1,
			expectedLogMsg: "Failed to list containers for pre-checks",
		},
	}

	for _, testClient := range tests {
		t.Run(testClient.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)

			client := mocks.NewMockClient(t)
			testClient.setupClient(client)
			hook.Reset()

			ExecutePreChecks(client, types.UpdateParams{
				Filter:         func(types.FilterableContainer) bool { return true },
				LifecycleHooks: true,
			})

			assert.Len(t, hook.Entries, testClient.expectedLogs, "log entry count mismatch")

			if len(hook.Entries) > 0 {
				assert.Contains(
					t,
					hook.Entries[0].Message,
					testClient.expectedLogMsg,
					"first log message mismatch",
				)
			} else {
				t.Errorf("No log entries captured; expected %d with message %q", testClient.expectedLogs, testClient.expectedLogMsg)
			}

			hook.Reset()
		})
	}
}

func TestExecutePostChecks(t *testing.T) {
	tests := []struct {
		name           string
		setupClient    func(*mocks.MockClient)
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name: "successful execution",
			setupClient: func(c *mocks.MockClient) {
				c.On("ListContainers", mock.Anything).Return([]types.Container{
					mockContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.post-check": "post-check",
					})),
					mockContainer(),
				}, nil)
				c.On("ExecuteCommand", types.ContainerID("container_id"), "post-check", 1).
					Return(true, nil)
			},
			expectedLogs:   2,
			expectedLogMsg: "Executing post-check command",
		},
		{
			name: "listing error",
			setupClient: func(c *mocks.MockClient) {
				c.On("ListContainers", mock.Anything).Return(nil, errListingFailed)
			},
			expectedLogs:   1,
			expectedLogMsg: "Failed to list containers for post-checks",
		},
	}

	for _, testClient := range tests {
		t.Run(testClient.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)

			client := mocks.NewMockClient(t)
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

			hook.Reset()
		})
	}
}

func TestExecutePreCheckCommand(t *testing.T) {
	tests := []struct {
		name           string
		container      types.Container
		setupClient    func(*mocks.MockClient)
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name: "command present",
			container: mockContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.pre-check": "pre-check",
			})),
			setupClient: func(c *mocks.MockClient) {
				c.On("ExecuteCommand", types.ContainerID("container_id"), "pre-check", 1).
					Return(true, nil)
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
			setupClient: func(c *mocks.MockClient) {
				c.On("ExecuteCommand", types.ContainerID("container_id"), "pre-check", 1).
					Return(false, errExecFailed)
			},
			expectedLogs:   2,
			expectedLogMsg: "Pre-check command failed",
		},
	}

	for _, testClient := range tests {
		t.Run(testClient.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)

			client := mocks.NewMockClient(t)
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

			hook.Reset()
		})
	}
}

func TestExecutePostCheckCommand(t *testing.T) {
	tests := []struct {
		name           string
		container      types.Container
		setupClient    func(*mocks.MockClient)
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name: "command present",
			container: mockContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.post-check": "post-check",
			})),
			setupClient: func(c *mocks.MockClient) {
				c.On("ExecuteCommand", types.ContainerID("container_id"), "post-check", 1).
					Return(true, nil)
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
			setupClient: func(c *mocks.MockClient) {
				c.On("ExecuteCommand", types.ContainerID("container_id"), "post-check", 1).
					Return(false, errExecFailed)
			},
			expectedLogs:   2,
			expectedLogMsg: "Post-check command failed",
		},
	}

	for _, testClient := range tests {
		t.Run(testClient.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)

			client := mocks.NewMockClient(t)
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

			hook.Reset()
		})
	}
}

func TestExecutePreUpdateCommand(t *testing.T) {
	tests := []struct {
		name           string
		container      types.Container
		setupClient    func(*mocks.MockClient)
		expectedResult bool
		expectedErr    bool
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name: "command present and running",
			container: mockContainer(
				withContainerState(dockerContainer.State{Running: true}),
				withLabels(map[string]string{
					"com.centurylinklabs.watchtower.lifecycle.pre-update":         "pre-update",
					"com.centurylinklabs.watchtower.lifecycle.pre-update-timeout": "2",
				}),
			),
			setupClient: func(c *mocks.MockClient) {
				c.On("ExecuteCommand", types.ContainerID("container_id"), "pre-update", 2).
					Return(true, nil)
			},
			expectedResult: true,
			expectedLogs:   1,
			expectedLogMsg: "Executing pre-update command",
		},
		{
			name:           "no command",
			container:      mockContainer(withContainerState(dockerContainer.State{Running: true})),
			expectedResult: false,
			expectedLogs:   1,
			expectedLogMsg: "No pre-update command supplied",
		},
		{
			name: "not running",
			container: mockContainer(
				withContainerState(dockerContainer.State{Running: false}),
				withLabels(map[string]string{
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
				withContainerState(dockerContainer.State{Running: true}),
				withLabels(map[string]string{
					"com.centurylinklabs.watchtower.lifecycle.pre-update":         "pre-update",
					"com.centurylinklabs.watchtower.lifecycle.pre-update-timeout": "2",
				}),
			),
			setupClient: func(c *mocks.MockClient) {
				c.On("ExecuteCommand", types.ContainerID("container_id"), "pre-update", 2).
					Return(false, errExecFailed)
			},
			expectedResult: true,
			expectedErr:    true,
			expectedLogs:   2,
			expectedLogMsg: "Error: Pre-update command failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runPreUpdateTest(t, tt)
		})
	}
}

// runPreUpdateTest executes a single pre-update command test case and validates its results.
// It sets up the mock client, executes the command, and asserts the expected outcomes.
//
// Parameters:
//   - t: The testing.T instance for assertions and test control.
//   - tt: The test case data containing setup and expectations.
func runPreUpdateTest(t *testing.T, tt struct {
	name           string
	container      types.Container
	setupClient    func(*mocks.MockClient)
	expectedResult bool
	expectedErr    bool
	expectedLogs   int
	expectedLogMsg string
},
) {
	t.Helper() // Mark as helper to improve stack traces

	hook := test.NewGlobal()

	logrus.SetLevel(logrus.DebugLevel)

	client := mocks.NewMockClient(t)
	if tt.setupClient != nil {
		tt.setupClient(client)
	}

	hook.Reset()

	result, err := ExecutePreUpdateCommand(client, tt.container)

	assert.Equal(t, tt.expectedResult, result)

	if tt.expectedErr {
		require.Error(t, err, "expected an error but got none")
		assert.Contains(t, err.Error(), "pre-update command execution failed")
	} else {
		require.NoError(t, err)
	}

	assert.Len(t, hook.Entries, tt.expectedLogs)

	if tt.expectedLogs > 0 {
		assert.Contains(t, hook.LastEntry().Message, tt.expectedLogMsg)
	}

	hook.Reset()
}

// TestExecutePostUpdateCommand tests the ExecutePostUpdateCommand function.
// It verifies command execution, container retrieval errors, and logging behavior.
func TestExecutePostUpdateCommand(t *testing.T) {
	tests := []struct {
		name           string
		containerID    types.ContainerID
		setupClient    func(*mocks.MockClient)
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name:        "command present",
			containerID: "test",
			setupClient: func(c *mocks.MockClient) {
				c.On("GetContainer", types.ContainerID("test")).
					Return(mockContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.post-update": "post-update",
					})), nil)
				c.On("ExecuteCommand", types.ContainerID("test"), "post-update", 1).
					Return(true, nil)
			},
			expectedLogs:   1,
			expectedLogMsg: "Executing post-update command",
		},
		{
			name:        "no command",
			containerID: "test",
			setupClient: func(c *mocks.MockClient) {
				c.On("GetContainer", types.ContainerID("test")).Return(mockContainer(), nil)
			},
			expectedLogs:   1,
			expectedLogMsg: "No post-update command supplied",
		},
		{
			name:        "container retrieval error",
			containerID: "test",
			setupClient: func(c *mocks.MockClient) {
				c.On("GetContainer", types.ContainerID("test")).Return(nil, errNotFound)
			},
			expectedLogs:   1,
			expectedLogMsg: "Failed to get container",
		},
		{
			name:        "command error",
			containerID: "test",
			setupClient: func(c *mocks.MockClient) {
				c.On("GetContainer", types.ContainerID("test")).
					Return(mockContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.post-update": "post-update",
					})), nil)
				c.On("ExecuteCommand", types.ContainerID("test"), "post-update", 1).
					Return(false, errExecFailed)
			},
			expectedLogs:   2,
			expectedLogMsg: "Post-update command failed",
		},
	}

	for _, testClient := range tests {
		t.Run(testClient.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)

			client := mocks.NewMockClient(t)
			testClient.setupClient(client)
			hook.Reset()

			ExecutePostUpdateCommand(client, testClient.containerID)

			assert.Len(t, hook.Entries, testClient.expectedLogs)

			if len(hook.Entries) > 0 {
				assert.Contains(t, hook.LastEntry().Message, testClient.expectedLogMsg)
			} else {
				t.Errorf("No log entries captured; expected %d with message %q", testClient.expectedLogs, testClient.expectedLogMsg)
			}

			hook.Reset()
		})
	}
}
