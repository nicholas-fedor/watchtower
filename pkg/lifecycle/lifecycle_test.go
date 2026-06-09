package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// mockedContainer creates a *container.Container for testing.
func mockedContainer(options ...func(*container.Container)) *container.Container {
	c := container.NewContainer(
		&dockerContainer.InspectResponse{
			ID:         "container_id",
			HostConfig: &dockerContainer.HostConfig{},
			Name:       "/test-container",
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

// assertLogContains asserts that at least one log entry contains the given message.
func assertLogContains(t *testing.T, entries []logrus.Entry, msg string) {
	t.Helper()

	require.NotEmpty(t, entries, "expected at least one log entry")

	for _, entry := range entries {
		if strings.Contains(entry.Message, msg) {
			return
		}
	}

	t.Errorf("expected a log entry containing %q", msg)
}

var (
	errListingFailed = errors.New("listing failed")
	errExecFailed    = errors.New("exec failed")
	errNotFound      = errors.New("not found")
)

// TestExecutePreChecks tests the ExecutePreChecks function.
func TestExecutePreChecks(t *testing.T) {
	tests := []struct {
		name           string
		setupClient    func(*mockContainer.MockClient)
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name: "successful execution",
			setupClient: func(c *mockContainer.MockClient) {
				c.On("ListContainers", mock.Anything, mock.Anything).Return([]types.Container{
					mockedContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.pre-check": "pre-check",
					})),
					mockedContainer(),
				}, nil)
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "pre-check", 1, 0, 0).
					Return(true, nil)
			},
			expectedLogs:   17, // Listing, Found, host-pre-check label not found x2 + skip x2, UID not found x2, UID not set x2, GID not found x2, GID not set x2, Execute, Label not found, Skip
			expectedLogMsg: "Listing containers for pre-checks",
		},
		{
			name: "listing error",
			setupClient: func(c *mockContainer.MockClient) {
				c.On("ListContainers", mock.Anything, mock.Anything).Return(nil, errListingFailed)
			},
			expectedLogs:   2, // Listing, Error
			expectedLogMsg: "Listing containers for pre-checks",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)

			client := mockContainer.NewMockClient(t)
			tt.setupClient(client)
			hook.Reset()

			ExecutePreChecks(context.Background(), client, types.UpdateParams{
				Filter:         func(types.FilterableContainer) bool { return true },
				LifecycleHooks: true,
				LifecycleUID:   0,
				LifecycleGID:   0,
			})

			assert.Len(t, hook.Entries, tt.expectedLogs, "log entry count mismatch")

			if len(hook.Entries) > 0 {
				assert.Contains(
					t,
					hook.Entries[0].Message,
					tt.expectedLogMsg,
					"first log message mismatch",
				)
			} else {
				t.Errorf(
					"No log entries captured; expected %d with message %q",
					tt.expectedLogs,
					tt.expectedLogMsg,
				)
			}

			hook.Reset()
		})
	}
}

// TestExecutePostChecks tests the ExecutePostChecks function.
func TestExecutePostChecks(t *testing.T) {
	tests := []struct {
		name           string
		setupClient    func(*mockContainer.MockClient)
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name: "successful execution",
			setupClient: func(c *mockContainer.MockClient) {
				c.On("ListContainers", mock.Anything, mock.Anything).Return([]types.Container{
					mockedContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.post-check": "post-check",
					})),
					mockedContainer(),
				}, nil)
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "post-check", 1, 0, 0).
					Return(true, nil)
			},
			expectedLogs:   17, // Listing, Found, host-post-check label not found x2 + skip x2, UID not found x2, UID not set x2, GID not found x2, GID not set x2, Execute, Label not found, Skip
			expectedLogMsg: "Listing containers for post-checks",
		},
		{
			name: "listing error",
			setupClient: func(c *mockContainer.MockClient) {
				c.On("ListContainers", mock.Anything, mock.Anything).Return(nil, errListingFailed)
			},
			expectedLogs:   2, // Listing, Error
			expectedLogMsg: "Listing containers for post-checks",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)

			client := mockContainer.NewMockClient(t)
			tt.setupClient(client)
			hook.Reset()

			ExecutePostChecks(context.Background(), client, types.UpdateParams{
				Filter:         func(types.FilterableContainer) bool { return true },
				LifecycleHooks: true,
				LifecycleUID:   0,
				LifecycleGID:   0,
			})

			assert.Len(t, hook.Entries, tt.expectedLogs)

			if len(hook.Entries) > 0 {
				assert.Contains(t, hook.Entries[0].Message, tt.expectedLogMsg)
			} else {
				t.Errorf(
					"No log entries captured; expected %d with message %q",
					tt.expectedLogs,
					tt.expectedLogMsg,
				)
			}

			hook.Reset()
		})
	}
}

// TestExecutePreCheckCommand tests the ExecutePreCheckCommand function.
func TestExecutePreCheckCommand(t *testing.T) {
	tests := []struct {
		name           string
		container      types.Container
		setupClient    func(*mockContainer.MockClient)
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name: "command present",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.pre-check": "pre-check",
			})),
			setupClient: func(c *mockContainer.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "pre-check", 1, 0, 0).
					Return(true, nil)
			},
			expectedLogs:   5, // UID not found, UID not set, GID not found, GID not set, Execute
			expectedLogMsg: "Executing pre-check command",
		},
		{
			name:           "no command",
			container:      mockedContainer(),
			expectedLogs:   6, // Command label not found, UID not found, UID not set, GID not found, GID not set, No command
			expectedLogMsg: "No pre-check command supplied",
		},
		{
			name: "command error",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.pre-check": "pre-check",
			})),
			setupClient: func(c *mockContainer.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "pre-check", 1, 0, 0).
					Return(false, errExecFailed)
			},
			expectedLogs:   6, // UID not found, UID not set, GID not found, GID not set, Execute, Error
			expectedLogMsg: "Pre-check command failed",
		},
		{
			name: "container UID/GID override",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.pre-check": "pre-check",
				"com.centurylinklabs.watchtower.lifecycle.uid":       "1000",
				"com.centurylinklabs.watchtower.lifecycle.gid":       "1001",
			})),
			setupClient: func(c *mockContainer.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "pre-check", 1, 1000, 1001).
					Return(true, nil)
			},
			expectedLogs:   5, // UID found, UID set, GID found, GID set, Execute
			expectedLogMsg: "Executing pre-check command",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)

			client := mockContainer.NewMockClient(t)
			if tt.setupClient != nil {
				tt.setupClient(client)
			}

			hook.Reset()

			ExecutePreCheckCommand(context.Background(), client, tt.container, 0, 0)

			assert.Len(t, hook.Entries, tt.expectedLogs)

			if len(hook.Entries) > 0 {
				assert.Contains(t, hook.LastEntry().Message, tt.expectedLogMsg)
			} else {
				t.Errorf(
					"No log entries captured; expected %d with message %q",
					tt.expectedLogs,
					tt.expectedLogMsg,
				)
			}

			hook.Reset()
		})
	}
}

// TestExecuteHostPreCheckCommand tests the ExecuteHostPreCheckCommand function.
func TestExecuteHostPreCheckCommand(t *testing.T) {
	tests := []struct {
		name           string
		container      types.Container
		expectedLogMsg string
	}{
		{
			name:           "no command",
			container:      mockedContainer(),
			expectedLogMsg: "No host pre-check command supplied",
		},
		{
			name: "command succeeds",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.host-pre-check": "true",
			})),
			expectedLogMsg: "Executing host pre-check command",
		},
		{
			name: "command fails",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.host-pre-check": "exit 1",
			})),
			expectedLogMsg: "Host pre-check command failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)
			hook.Reset()

			ExecuteHostPreCheckCommand(context.Background(), tt.container)

			assertLogContains(t, hook.Entries, tt.expectedLogMsg)

			hook.Reset()
		})
	}
}

// TestExecuteHostPostCheckCommand tests the ExecuteHostPostCheckCommand function.
func TestExecuteHostPostCheckCommand(t *testing.T) {
	tests := []struct {
		name           string
		container      types.Container
		expectedLogMsg string
	}{
		{
			name:           "no command",
			container:      mockedContainer(),
			expectedLogMsg: "No host post-check command supplied",
		},
		{
			name: "command succeeds",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.host-post-check": "true",
			})),
			expectedLogMsg: "Executing host post-check command",
		},
		{
			name: "command fails",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.host-post-check": "exit 1",
			})),
			expectedLogMsg: "Host post-check command failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)
			hook.Reset()

			ExecuteHostPostCheckCommand(context.Background(), tt.container)

			assertLogContains(t, hook.Entries, tt.expectedLogMsg)

			hook.Reset()
		})
	}
}

// TestExecuteHostPreUpdateCommand tests the ExecuteHostPreUpdateCommand function.
func TestExecuteHostPreUpdateCommand(t *testing.T) {
	tests := []struct {
		name           string
		container      types.Container
		expectedSkip   bool
		expectError    bool
		expectedLogMsg string
	}{
		{
			name:           "no command",
			container:      mockedContainer(),
			expectedLogMsg: "No host pre-update command supplied",
		},
		{
			name: "command succeeds",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.host-pre-update": "true",
			})),
			expectedLogMsg: "Host pre-update command executed",
		},
		{
			name: "exit code 75 requests skip",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.host-pre-update": "exit 75",
			})),
			expectedSkip:   true,
			expectedLogMsg: "Host pre-update command executed",
		},
		{
			name: "command fails",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.host-pre-update": "exit 1",
			})),
			expectError:    true,
			expectedLogMsg: "Host pre-update command failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)
			hook.Reset()

			skip, err := ExecuteHostPreUpdateCommand(context.Background(), tt.container)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.expectedSkip, skip)
			assertLogContains(t, hook.Entries, tt.expectedLogMsg)

			hook.Reset()
		})
	}
}

// TestExecuteHostPostUpdateCommand tests the ExecuteHostPostUpdateCommand function.
func TestExecuteHostPostUpdateCommand(t *testing.T) {
	tests := []struct {
		name           string
		setupClient    func(*mockContainer.MockClient)
		expectedLogMsg string
	}{
		{
			name: "command succeeds",
			setupClient: func(c *mockContainer.MockClient) {
				c.On("GetContainer", mock.Anything, types.ContainerID("test")).
					Return(mockedContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.host-post-update": "true",
					})), nil)
			},
			expectedLogMsg: "Executing host post-update command",
		},
		{
			name: "no command",
			setupClient: func(c *mockContainer.MockClient) {
				c.On("GetContainer", mock.Anything, types.ContainerID("test")).Return(mockedContainer(), nil)
			},
			expectedLogMsg: "No host post-update command supplied",
		},
		{
			name: "container retrieval error",
			setupClient: func(c *mockContainer.MockClient) {
				c.On("GetContainer", mock.Anything, types.ContainerID("test")).Return(nil, errNotFound)
			},
			expectedLogMsg: "Failed to get container for host post-update",
		},
		{
			name: "command fails",
			setupClient: func(c *mockContainer.MockClient) {
				c.On("GetContainer", mock.Anything, types.ContainerID("test")).
					Return(mockedContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.host-post-update": "exit 1",
					})), nil)
			},
			expectedLogMsg: "Host post-update command failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)

			client := mockContainer.NewMockClient(t)
			tt.setupClient(client)
			hook.Reset()

			ExecuteHostPostUpdateCommand(context.Background(), client, types.ContainerID("test"))

			assertLogContains(t, hook.Entries, tt.expectedLogMsg)

			hook.Reset()
		})
	}
}

// TestExecutePostCheckCommand tests the ExecutePostCheckCommand function.
func TestExecutePostCheckCommand(t *testing.T) {
	tests := []struct {
		name           string
		container      types.Container
		setupClient    func(*mockContainer.MockClient)
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name: "command present",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.post-check": "post-check",
			})),
			setupClient: func(c *mockContainer.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "post-check", 1, 0, 0).
					Return(true, nil)
			},
			expectedLogs:   5, // UID not found, UID not set, GID not found, GID not set, Execute
			expectedLogMsg: "Executing post-check command",
		},
		{
			name:           "no command",
			container:      mockedContainer(),
			expectedLogs:   6, // Command label not found, UID not found, UID not set, GID not found, GID not set, No command
			expectedLogMsg: "No post-check command supplied",
		},
		{
			name: "command error",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.post-check": "post-check",
			})),
			setupClient: func(c *mockContainer.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "post-check", 1, 0, 0).
					Return(false, errExecFailed)
			},
			expectedLogs:   6, // UID not found, UID not set, GID not found, GID not set, Execute, Error
			expectedLogMsg: "Post-check command failed",
		},
		{
			name: "container UID/GID override",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.post-check": "post-check",
				"com.centurylinklabs.watchtower.lifecycle.uid":        "2000",
				"com.centurylinklabs.watchtower.lifecycle.gid":        "2001",
			})),
			setupClient: func(c *mockContainer.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "post-check", 1, 2000, 2001).
					Return(true, nil)
			},
			expectedLogs:   5, // UID found, UID set, GID found, GID set, Execute
			expectedLogMsg: "Executing post-check command",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)

			client := mockContainer.NewMockClient(t)
			if tt.setupClient != nil {
				tt.setupClient(client)
			}

			hook.Reset()

			ExecutePostCheckCommand(context.Background(), client, tt.container, 0, 0)

			assert.Len(t, hook.Entries, tt.expectedLogs)

			if len(hook.Entries) > 0 {
				assert.Contains(t, hook.LastEntry().Message, tt.expectedLogMsg)
			} else {
				t.Errorf(
					"No log entries captured; expected %d with message %q",
					tt.expectedLogs,
					tt.expectedLogMsg,
				)
			}

			hook.Reset()
		})
	}
}

// TestExecutePreUpdateCommand tests the ExecutePreUpdateCommand function.
func TestExecutePreUpdateCommand(t *testing.T) {
	tests := []struct {
		name           string
		container      types.Container
		setupClient    func(*mockContainer.MockClient)
		expectedResult bool
		expectedErr    bool
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name: "command present and running",
			container: mockedContainer(
				withContainerState(dockerContainer.State{Running: true}),
				withLabels(map[string]string{
					"com.centurylinklabs.watchtower.lifecycle.pre-update":         "pre-update",
					"com.centurylinklabs.watchtower.lifecycle.pre-update-timeout": "2",
				}),
			),
			setupClient: func(c *mockContainer.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "pre-update", 2, 0, 0).
					Return(true, nil)
			},
			expectedResult: true,
			expectedLogs:   7, // Timeout, UID not found, UID not set, GID not found, GID not set, Execute, Success
			expectedLogMsg: "Pre-update command executed",
		},
		{
			name: "no command",
			container: mockedContainer(
				withContainerState(dockerContainer.State{Running: true}),
			),
			expectedResult: false,
			expectedLogs:   4, // Timeout label not found, Default timeout, Command label not found, Skipping
			expectedLogMsg: "No pre-update command supplied",
		},
		{
			name: "not running",
			container: mockedContainer(
				withContainerState(dockerContainer.State{Running: false}),
				withLabels(map[string]string{
					"com.centurylinklabs.watchtower.lifecycle.pre-update":         "pre-update",
					"com.centurylinklabs.watchtower.lifecycle.pre-update-timeout": "2",
				}),
			),
			expectedResult: false,
			expectedLogs:   2, // Timeout, Skip
			expectedLogMsg: "Container is not running",
		},
		{
			name: "command error",
			container: mockedContainer(
				withContainerState(dockerContainer.State{Running: true}),
				withLabels(map[string]string{
					"com.centurylinklabs.watchtower.lifecycle.pre-update":         "pre-update",
					"com.centurylinklabs.watchtower.lifecycle.pre-update-timeout": "2",
				}),
			),
			setupClient: func(c *mockContainer.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "pre-update", 2, 0, 0).
					Return(false, errExecFailed)
			},
			expectedResult: true,
			expectedErr:    true,
			expectedLogs:   7, // Timeout, UID not found, UID not set, GID not found, GID not set, Execute, Error
			expectedLogMsg: "Pre-update command failed",
		},
		{
			name: "container UID/GID override",
			container: mockedContainer(
				withContainerState(dockerContainer.State{Running: true}),
				withLabels(map[string]string{
					"com.centurylinklabs.watchtower.lifecycle.pre-update":         "pre-update",
					"com.centurylinklabs.watchtower.lifecycle.pre-update-timeout": "2",
					"com.centurylinklabs.watchtower.lifecycle.uid":                "3000",
					"com.centurylinklabs.watchtower.lifecycle.gid":                "3001",
				}),
			),
			setupClient: func(c *mockContainer.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "pre-update", 2, 3000, 3001).
					Return(true, nil)
			},
			expectedResult: true,
			expectedLogs:   7, // Timeout, UID found, UID set, GID found, GID set, Execute, Success
			expectedLogMsg: "Pre-update command executed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runPreUpdateTest(t, tt)
		})
	}
}

// runPreUpdateTest executes a single pre-update command test case and validates its results.
func runPreUpdateTest(t *testing.T, tt struct {
	name           string
	container      types.Container
	setupClient    func(*mockContainer.MockClient)
	expectedResult bool
	expectedErr    bool
	expectedLogs   int
	expectedLogMsg string
},
) {
	t.Helper()

	hook := test.NewGlobal()

	logrus.SetLevel(logrus.DebugLevel)

	client := mockContainer.NewMockClient(t)
	if tt.setupClient != nil {
		tt.setupClient(client)
	}

	hook.Reset()

	result, err := ExecutePreUpdateCommand(context.Background(), client, tt.container, 0, 0)

	assert.Equal(t, tt.expectedResult, result)

	if tt.expectedErr {
		require.Error(t, err, "expected an error but got none")
		assert.Contains(
			t,
			err.Error(),
			"pre-update command execution failed",
			"error message mismatch",
		)
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
func TestExecutePostUpdateCommand(t *testing.T) {
	tests := []struct {
		name           string
		containerID    types.ContainerID
		setupClient    func(*mockContainer.MockClient)
		expectedLogs   int
		expectedLogMsg string
	}{
		{
			name:        "command present",
			containerID: "test",
			setupClient: func(c *mockContainer.MockClient) {
				c.On("GetContainer", mock.Anything, types.ContainerID("test")).
					Return(mockedContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.post-update": "post-update",
					})), nil)
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "post-update", 1, 0, 0).
					Return(true, nil)
			},
			expectedLogs:   8, // Retrieve, Timeout label not found, Default timeout, UID not found, UID not set, GID not found, GID not set, Execute
			expectedLogMsg: "Executing post-update command",
		},
		{
			name:        "no command",
			containerID: "test",
			setupClient: func(c *mockContainer.MockClient) {
				c.On("GetContainer", mock.Anything, types.ContainerID("test")).Return(mockedContainer(), nil)
			},
			expectedLogs:   9, // Retrieve, Timeout label not found, Default timeout, UID not found, UID not set, GID not found, GID not set, Command label not found, Skipping
			expectedLogMsg: "No post-update command supplied",
		},
		{
			name:        "container retrieval error",
			containerID: "test",
			setupClient: func(c *mockContainer.MockClient) {
				c.On("GetContainer", mock.Anything, types.ContainerID("test")).Return(nil, errNotFound)
			},
			expectedLogs:   2, // Retrieve, Error
			expectedLogMsg: "Failed to get container",
		},
		{
			name:        "command error",
			containerID: "test",
			setupClient: func(c *mockContainer.MockClient) {
				c.On("GetContainer", mock.Anything, types.ContainerID("test")).
					Return(mockedContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.post-update": "post-update",
					})), nil)
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "post-update", 1, 0, 0).
					Return(false, errExecFailed)
			},
			expectedLogs:   9, // Retrieve, Timeout label not found, Default timeout, UID not found, UID not set, GID not found, GID not set, Execute, Error
			expectedLogMsg: "Post-update command failed",
		},
		{
			name:        "container UID/GID override",
			containerID: "test",
			setupClient: func(c *mockContainer.MockClient) {
				c.On("GetContainer", mock.Anything, types.ContainerID("test")).
					Return(mockedContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.post-update": "post-update",
						"com.centurylinklabs.watchtower.lifecycle.uid":         "4000",
						"com.centurylinklabs.watchtower.lifecycle.gid":         "4001",
					})), nil)
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "post-update", 1, 4000, 4001).
					Return(true, nil)
			},
			expectedLogs:   8, // Retrieve, Timeout label not found, Default timeout, UID found, UID set, GID found, GID set, Execute
			expectedLogMsg: "Executing post-update command",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook := test.NewGlobal()

			logrus.SetLevel(logrus.DebugLevel)

			client := mockContainer.NewMockClient(t)
			tt.setupClient(client)
			hook.Reset()

			ExecutePostUpdateCommand(context.Background(), client, tt.containerID, 0, 0)

			assert.Len(t, hook.Entries, tt.expectedLogs)

			if len(hook.Entries) > 0 {
				assert.Contains(t, hook.LastEntry().Message, tt.expectedLogMsg)
			} else {
				t.Errorf(
					"No log entries captured; expected %d with message %q",
					tt.expectedLogs,
					tt.expectedLogMsg,
				)
			}

			hook.Reset()
		})
	}
}
