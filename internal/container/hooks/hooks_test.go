package hooks

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container"
	mockContainerClient "github.com/nicholas-fedor/watchtower/internal/container/client/mocks"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
)

// newDebugLog returns a debug-level logger that writes logfmt lines to the
// returned buffer, so tests can assert on what was logged.
//
// Returns:
//   - *zerolog.Logger: Logger writing to the returned buffer.
//   - *bytes.Buffer: Buffer holding the logged lines.
func newDebugLog() (*zerolog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}

	return logging.New(logging.LogfmtWriter(buf), logging.DebugLevel), buf
}

// mockedContainer creates a *container.Container for testing.
func mockedContainer(options ...func(*container.Container)) *container.Container {
	c := container.NewContainer(nil,
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

var (
	errListingFailed = errors.New("listing failed")
	errExecFailed    = errors.New("exec failed")
	errNotFound      = errors.New("not found")
)

// acceptNamed is a filter that accepts only the container named "accepted".
// The check-hook tests pass it so they can tell the supplied filter apart from
// any filter that accepts or rejects every container.
func acceptNamed(c filters.FilterableContainer) bool { return c.Name() == "accepted" }

// namedContainer returns a stopped container without labels named name. A
// container fixes its name when it is built. Renaming it afterwards has no
// effect on Name.
func namedContainer(name string) *container.Container {
	return container.NewContainer(nil,
		&dockerContainer.InspectResponse{
			ID:         "container_id",
			HostConfig: &dockerContainer.HostConfig{},
			Name:       "/" + name,
			State:      &dockerContainer.State{Running: false},
			Config:     &dockerContainer.Config{Labels: map[string]string{}},
		},
		nil,
	)
}

// assertFilterReached verifies that ListContainers received exactly one filter
// and that it decides like want for a container that want accepts and one it
// rejects.
func assertFilterReached(t *testing.T, captured []filters.Filter, want filters.Filter) {
	t.Helper()

	require.Len(t, captured, 1, "ListContainers should receive the hook's filter")

	for _, fixture := range []filters.FilterableContainer{
		namedContainer("accepted"),
		namedContainer("rejected"),
	} {
		assert.Equal(t, want(fixture), captured[0](fixture), "filter result for %s", fixture.Name())
	}
}

// TestExecutePreChecks verifies that ExecutePreChecks lists containers with
// the supplied filter and runs each pre-check command with the supplied
// default UID and GID.
func TestExecutePreChecks(t *testing.T) {
	tests := []struct {
		name           string
		setupClient    func(c *mockContainerClient.MockClient, capture func(mock.Arguments))
		expectedLogMsg string
	}{
		{
			name: "successful execution",
			setupClient: func(c *mockContainerClient.MockClient, capture func(mock.Arguments)) {
				c.On("ListContainers", mock.Anything, mock.Anything).Run(capture).Return([]*container.Container{
					mockedContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.pre-check": "pre-check",
					})),
					mockedContainer(),
				}, nil)
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "pre-check", 1, 1000, 1001).
					Return(true, nil)
			},
			expectedLogMsg: "Listing containers for pre-checks",
		},
		{
			name: "listing error",
			setupClient: func(c *mockContainerClient.MockClient, capture func(mock.Arguments)) {
				c.On("ListContainers", mock.Anything, mock.Anything).Run(capture).Return(nil, errListingFailed)
			},
			expectedLogMsg: "Listing containers for pre-checks",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log, logBuf := newDebugLog()
			client := mockContainerClient.NewMockClient(t)

			var captured []filters.Filter

			tt.setupClient(client, func(args mock.Arguments) {
				captured, _ = args.Get(1).([]filters.Filter)
			})
			ExecutePreChecks(log, context.Background(), client, acceptNamed, 1000, 1001, nil)

			assertFilterReached(t, captured, acceptNamed)

			output := logBuf.String()
			assert.NotEmpty(t, output, "expected log output")
			assert.Contains(t, output, tt.expectedLogMsg,
				"first log message mismatch",
			)
		})
	}
}

// TestExecutePostChecks verifies that ExecutePostChecks lists containers with
// the supplied filter and runs each post-check command with the supplied
// default UID and GID.
func TestExecutePostChecks(t *testing.T) {
	tests := []struct {
		name           string
		setupClient    func(c *mockContainerClient.MockClient, capture func(mock.Arguments))
		expectedLogMsg string
	}{
		{
			name: "successful execution",
			setupClient: func(c *mockContainerClient.MockClient, capture func(mock.Arguments)) {
				c.On("ListContainers", mock.Anything, mock.Anything).Run(capture).Return([]*container.Container{
					mockedContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.post-check": "post-check",
					})),
					mockedContainer(),
				}, nil)
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "post-check", 1, 1000, 1001).
					Return(true, nil)
			},
			expectedLogMsg: "Listing containers for post-checks",
		},
		{
			name: "listing error",
			setupClient: func(c *mockContainerClient.MockClient, capture func(mock.Arguments)) {
				c.On("ListContainers", mock.Anything, mock.Anything).Run(capture).Return(nil, errListingFailed)
			},
			expectedLogMsg: "Listing containers for post-checks",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log, logBuf := newDebugLog()
			client := mockContainerClient.NewMockClient(t)

			var captured []filters.Filter

			tt.setupClient(client, func(args mock.Arguments) {
				captured, _ = args.Get(1).([]filters.Filter)
			})
			ExecutePostChecks(log, context.Background(), client, acceptNamed, 1000, 1001, nil)

			assertFilterReached(t, captured, acceptNamed)

			output := logBuf.String()
			assert.NotEmpty(t, output, "expected log output")
			assert.Contains(t, output, tt.expectedLogMsg)
		})
	}
}

func TestExecutePreChecks_UsesListedContainers(t *testing.T) {
	log, logBuf := newDebugLog()
	client := mockContainerClient.NewMockClient(t)
	listed := []*container.Container{
		mockedContainer(withLabels(map[string]string{
			"com.centurylinklabs.watchtower.lifecycle.pre-check": "pre-check",
		})),
	}

	client.On("ExecuteCommand", mock.Anything, mock.Anything, "pre-check", 1, 0, 0).
		Return(true, nil)

	ExecutePreChecks(log, context.Background(), client, func(filters.FilterableContainer) bool { return true }, 0, 0, listed)

	client.AssertNotCalled(t, "ListContainers", mock.Anything, mock.Anything)
	assert.Contains(t, logBuf.String(), "Found containers for pre-checks")
}

func TestExecutePostChecks_UsesListedContainers(t *testing.T) {
	log, logBuf := newDebugLog()
	client := mockContainerClient.NewMockClient(t)
	listed := []*container.Container{
		mockedContainer(withLabels(map[string]string{
			"com.centurylinklabs.watchtower.lifecycle.post-check": "post-check",
		})),
	}

	client.On("ExecuteCommand", mock.Anything, mock.Anything, "post-check", 1, 0, 0).
		Return(true, nil)

	ExecutePostChecks(log, context.Background(), client, func(filters.FilterableContainer) bool { return true }, 0, 0, listed)

	client.AssertNotCalled(t, "ListContainers", mock.Anything, mock.Anything)
	assert.Contains(t, logBuf.String(), "Found containers for post-checks")
}

func TestExecutePreChecks_EmptyListedDoesNotRelist(t *testing.T) {
	log, _ := newDebugLog()
	client := mockContainerClient.NewMockClient(t)
	listed := []*container.Container{}

	ExecutePreChecks(log, context.Background(), client, func(filters.FilterableContainer) bool { return true }, 0, 0, listed)

	client.AssertNotCalled(t, "ListContainers", mock.Anything, mock.Anything)
	client.AssertNotCalled(t, "ExecuteCommand", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestExecutePostChecks_EmptyListedDoesNotRelist(t *testing.T) {
	log, _ := newDebugLog()
	client := mockContainerClient.NewMockClient(t)
	listed := []*container.Container{}

	ExecutePostChecks(log, context.Background(), client, func(filters.FilterableContainer) bool { return true }, 0, 0, listed)

	client.AssertNotCalled(t, "ListContainers", mock.Anything, mock.Anything)
	client.AssertNotCalled(t, "ExecuteCommand", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestExecutePreCheckCommand tests the executePreCheckCommand function.
func TestExecutePreCheckCommand(t *testing.T) {
	tests := []struct {
		name           string
		container      *container.Container
		setupClient    func(*mockContainerClient.MockClient)
		expectedLogMsg string
	}{
		{
			name: "command present",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.pre-check": "pre-check",
			})),
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "pre-check", 1, 0, 0).
					Return(true, nil)
			},
			expectedLogMsg: "Executing pre-check command",
		},
		{
			name: "timeout label",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.pre-check":         "pre-check",
				"com.centurylinklabs.watchtower.lifecycle.pre-check-timeout": "5",
			})),
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "pre-check", 5, 0, 0).
					Return(true, nil)
			},
			expectedLogMsg: "Executing pre-check command",
		},
		{
			name: "zero timeout label runs without a timeout",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.pre-check":         "pre-check",
				"com.centurylinklabs.watchtower.lifecycle.pre-check-timeout": "0",
			})),
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "pre-check", 0, 0, 0).
					Return(true, nil)
			},
			expectedLogMsg: "Executing pre-check command",
		},
		{
			name:           "no command",
			container:      mockedContainer(),
			expectedLogMsg: "No pre-check command supplied",
		},
		{
			name: "command error",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.pre-check": "pre-check",
			})),
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "pre-check", 1, 0, 0).
					Return(false, errExecFailed)
			},
			expectedLogMsg: "Pre-check command failed",
		},
		{
			name: "container UID/GID override",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.pre-check": "pre-check",
				"com.centurylinklabs.watchtower.lifecycle.uid":       "1000",
				"com.centurylinklabs.watchtower.lifecycle.gid":       "1001",
			})),
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "pre-check", 1, 1000, 1001).
					Return(true, nil)
			},
			expectedLogMsg: "Executing pre-check command",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log, logBuf := newDebugLog()

			client := mockContainerClient.NewMockClient(t)
			if tt.setupClient != nil {
				tt.setupClient(client)
			}

			executePreCheckCommand(log, context.Background(), client, tt.container, 0, 0)

			output := logBuf.String()
			assert.NotEmpty(t, output, "expected log output")
			assert.Contains(t, output, tt.expectedLogMsg)
		})
	}
}

// TestExecutePostCheckCommand tests the executePostCheckCommand function.
func TestExecutePostCheckCommand(t *testing.T) {
	tests := []struct {
		name           string
		container      *container.Container
		setupClient    func(*mockContainerClient.MockClient)
		expectedLogMsg string
	}{
		{
			name: "command present",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.post-check": "post-check",
			})),
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "post-check", 1, 0, 0).
					Return(true, nil)
			},
			expectedLogMsg: "Executing post-check command",
		},
		{
			name: "timeout label",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.post-check":         "post-check",
				"com.centurylinklabs.watchtower.lifecycle.post-check-timeout": "5",
			})),
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "post-check", 5, 0, 0).
					Return(true, nil)
			},
			expectedLogMsg: "Executing post-check command",
		},
		{
			name: "zero timeout label runs without a timeout",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.post-check":         "post-check",
				"com.centurylinklabs.watchtower.lifecycle.post-check-timeout": "0",
			})),
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "post-check", 0, 0, 0).
					Return(true, nil)
			},
			expectedLogMsg: "Executing post-check command",
		},
		{
			name:           "no command",
			container:      mockedContainer(),
			expectedLogMsg: "No post-check command supplied",
		},
		{
			name: "command error",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.post-check": "post-check",
			})),
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "post-check", 1, 0, 0).
					Return(false, errExecFailed)
			},
			expectedLogMsg: "Post-check command failed",
		},
		{
			name: "container UID/GID override",
			container: mockedContainer(withLabels(map[string]string{
				"com.centurylinklabs.watchtower.lifecycle.post-check": "post-check",
				"com.centurylinklabs.watchtower.lifecycle.uid":        "2000",
				"com.centurylinklabs.watchtower.lifecycle.gid":        "2001",
			})),
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "post-check", 1, 2000, 2001).
					Return(true, nil)
			},
			expectedLogMsg: "Executing post-check command",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log, logBuf := newDebugLog()

			client := mockContainerClient.NewMockClient(t)
			if tt.setupClient != nil {
				tt.setupClient(client)
			}

			executePostCheckCommand(log, context.Background(), client, tt.container, 0, 0)

			output := logBuf.String()
			assert.NotEmpty(t, output, "expected log output")
			assert.Contains(t, output, tt.expectedLogMsg)
		})
	}
}

// containerWithLabels returns a container that logs to log and carries labels.
// Its state is running when running is true.
func containerWithLabels(log *zerolog.Logger, running bool, labels map[string]string) *container.Container {
	return container.NewContainer(log,
		&dockerContainer.InspectResponse{
			ID:         "container_id",
			HostConfig: &dockerContainer.HostConfig{},
			Name:       "/test-container",
			State:      &dockerContainer.State{Running: running},
			Config:     &dockerContainer.Config{Labels: labels},
		},
		nil,
	)
}

// TestExecuteCheckCommand_NoCommandSkipsLabels verifies that a check hook
// without a command reads neither its timeout label nor the lifecycle UID and
// GID labels. Check hooks run for every filtered container on each scan, so
// reading the labels would log a debug line per container and warn about an
// invalid label for a hook that never runs.
func TestExecuteCheckCommand_NoCommandSkipsLabels(t *testing.T) {
	t.Parallel()

	log, logBuf := newDebugLog()
	c := containerWithLabels(log, false, map[string]string{
		"com.centurylinklabs.watchtower.lifecycle.pre-check-timeout":  "-1",
		"com.centurylinklabs.watchtower.lifecycle.post-check-timeout": "-1",
		"com.centurylinklabs.watchtower.lifecycle.uid":                "-5",
		"com.centurylinklabs.watchtower.lifecycle.gid":                "-5",
	})
	client := mockContainerClient.NewMockClient(t)

	executePreCheckCommand(log, context.Background(), client, c, 0, 0)
	executePostCheckCommand(log, context.Background(), client, c, 0, 0)

	output := logBuf.String()
	assert.Contains(t, output, "No pre-check command supplied")
	assert.Contains(t, output, "No post-check command supplied")
	assert.NotContains(t, output, "timeout")
	assert.NotContains(t, output, "Invalid")
}

// TestExecutePreUpdateCommand_SkippedHookReadsNoLabels verifies that a
// pre-update hook that does not run reads none of its labels. That covers a
// container without a pre-update command and a stopped container, so an
// invalid timeout, UID or GID label logs no warning for a hook that never runs.
func TestExecutePreUpdateCommand_SkippedHookReadsNoLabels(t *testing.T) {
	t.Parallel()

	invalidLabels := map[string]string{
		"com.centurylinklabs.watchtower.lifecycle.pre-update-timeout": "-1",
		"com.centurylinklabs.watchtower.lifecycle.uid":                "-5",
		"com.centurylinklabs.watchtower.lifecycle.gid":                "-5",
	}

	tests := []struct {
		name    string
		running bool
		command string
		wantLog string
	}{
		{name: "no command", running: true, wantLog: "No pre-update command supplied"},
		{name: "stopped container", running: false, command: "pre-update", wantLog: "Container is not running"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			labels := maps.Clone(invalidLabels)
			if tt.command != "" {
				labels["com.centurylinklabs.watchtower.lifecycle.pre-update"] = tt.command
			}

			log, logBuf := newDebugLog()
			c := containerWithLabels(log, tt.running, labels)
			client := mockContainerClient.NewMockClient(t)

			skip, err := ExecutePreUpdateCommand(log, context.Background(), client, c, 0, 0)
			require.NoError(t, err)
			assert.False(t, skip)

			output := logBuf.String()
			assert.Contains(t, output, tt.wantLog)
			assert.NotContains(t, output, "timeout")
			assert.NotContains(t, output, "Invalid")
		})
	}
}

// TestExecutePostUpdateCommand_NoCommandReadsNoLabels verifies that a
// post-update hook without a command reads none of its labels, so an invalid
// timeout, UID or GID label logs no warning for a hook that never runs.
func TestExecutePostUpdateCommand_NoCommandReadsNoLabels(t *testing.T) {
	t.Parallel()

	log, logBuf := newDebugLog()
	c := containerWithLabels(log, true, map[string]string{
		"com.centurylinklabs.watchtower.lifecycle.post-update-timeout": "-1",
		"com.centurylinklabs.watchtower.lifecycle.uid":                 "-5",
		"com.centurylinklabs.watchtower.lifecycle.gid":                 "-5",
	})
	client := mockContainerClient.NewMockClient(t)
	client.EXPECT().GetContainer(mock.Anything, container.ContainerID("test")).Return(c, nil).Once()

	ExecutePostUpdateCommand(log, context.Background(), client, "test", 0, 0)

	output := logBuf.String()
	assert.Contains(t, output, "No post-update command supplied")
	assert.NotContains(t, output, "timeout")
	assert.NotContains(t, output, "Invalid")
}

// TestExecutePreUpdateCommand tests the ExecutePreUpdateCommand function.
func TestExecutePreUpdateCommand(t *testing.T) {
	tests := []struct {
		name           string
		container      *container.Container
		setupClient    func(*mockContainerClient.MockClient)
		expectedResult bool
		expectedErr    bool
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
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "pre-update", 2, 0, 0).
					Return(true, nil)
			},
			expectedResult: true,
			expectedLogMsg: "Pre-update command executed",
		},
		{
			name: "no command",
			container: mockedContainer(
				withContainerState(dockerContainer.State{Running: true}),
			),
			expectedResult: false,
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
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "pre-update", 2, 0, 0).
					Return(false, errExecFailed)
			},
			expectedResult: true,
			expectedErr:    true,
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
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "pre-update", 2, 3000, 3001).
					Return(true, nil)
			},
			expectedResult: true,
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
	container      *container.Container
	setupClient    func(*mockContainerClient.MockClient)
	expectedResult bool
	expectedErr    bool
	expectedLogMsg string
},
) {
	t.Helper()

	log, logBuf := newDebugLog()

	client := mockContainerClient.NewMockClient(t)
	if tt.setupClient != nil {
		tt.setupClient(client)
	}

	result, err := ExecutePreUpdateCommand(log, context.Background(), client, tt.container, 0, 0)

	assert.Equal(t, tt.expectedResult, result)

	if tt.expectedErr {
		// The error names the failed hook once. Its callers name the container.
		require.ErrorIs(t, err, errPreUpdateFailed)
		require.ErrorIs(t, err, errExecFailed)
		require.EqualError(t, err, "pre-update command failed: "+errExecFailed.Error())
	} else {
		require.NoError(t, err)
	}

	output := logBuf.String()
	assert.NotEmpty(t, output, "expected log output")
	assert.Contains(t, output, tt.expectedLogMsg)
}

// TestExecutePostUpdateCommand tests the ExecutePostUpdateCommand function.
func TestExecutePostUpdateCommand(t *testing.T) {
	tests := []struct {
		name           string
		containerID    container.ContainerID
		setupClient    func(*mockContainerClient.MockClient)
		expectedLogMsg string
	}{
		{
			name:        "command present",
			containerID: "test",
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("GetContainer", mock.Anything, container.ContainerID("test")).
					Return(mockedContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.post-update": "post-update",
					})), nil)
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "post-update", 1, 0, 0).
					Return(true, nil)
			},
			expectedLogMsg: "Executing post-update command",
		},
		{
			name:        "no command",
			containerID: "test",
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("GetContainer", mock.Anything, container.ContainerID("test")).Return(mockedContainer(), nil)
			},
			expectedLogMsg: "No post-update command supplied",
		},
		{
			name:        "container retrieval error",
			containerID: "test",
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("GetContainer", mock.Anything, container.ContainerID("test")).Return(nil, errNotFound)
			},
			expectedLogMsg: "Failed to get container",
		},
		{
			name:        "command error",
			containerID: "test",
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("GetContainer", mock.Anything, container.ContainerID("test")).
					Return(mockedContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.post-update": "post-update",
					})), nil)
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "post-update", 1, 0, 0).
					Return(false, errExecFailed)
			},
			expectedLogMsg: "Post-update command failed",
		},
		{
			name:        "container UID/GID override",
			containerID: "test",
			setupClient: func(c *mockContainerClient.MockClient) {
				c.On("GetContainer", mock.Anything, container.ContainerID("test")).
					Return(mockedContainer(withLabels(map[string]string{
						"com.centurylinklabs.watchtower.lifecycle.post-update": "post-update",
						"com.centurylinklabs.watchtower.lifecycle.uid":         "4000",
						"com.centurylinklabs.watchtower.lifecycle.gid":         "4001",
					})), nil)
				c.On("ExecuteCommand", mock.Anything, mock.Anything, "post-update", 1, 4000, 4001).
					Return(true, nil)
			},
			expectedLogMsg: "Executing post-update command",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log, logBuf := newDebugLog()
			client := mockContainerClient.NewMockClient(t)
			tt.setupClient(client)
			ExecutePostUpdateCommand(log, context.Background(), client, tt.containerID, 0, 0)

			output := logBuf.String()
			assert.NotEmpty(t, output, "expected log output")
			assert.Contains(t, output, tt.expectedLogMsg)
		})
	}
}
