package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"testing/fstest"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	mockCmd "github.com/nicholas-fedor/watchtower/cmd/mocks"
	"github.com/nicholas-fedor/watchtower/internal/api"
	"github.com/nicholas-fedor/watchtower/internal/container"
	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/settings"
	dockerConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/docker"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	"github.com/nicholas-fedor/watchtower/internal/update"
)

// newTestLogger returns a logger that writes each event to the test's log, so a
// failing test shows what it logged.
//
// Parameters:
//   - t: Test that receives the log lines.
//
// Returns:
//   - *zerolog.Logger: Logger writing to t.
func newTestLogger(t *testing.T) *zerolog.Logger {
	t.Helper()

	// Frame 4 skips zerolog's write path, so each line names the log call.
	w := zerolog.TestWriter{T: t, Frame: 4}

	return new(zerolog.New(w))
}

// TestAwaitDockerClient verifies that the wait before the first container
// listing pauses the process once, for one second.
func TestAwaitDockerClient(t *testing.T) {
	t.Parallel()

	var slept []time.Duration

	proc := &process{
		log:   newTestLogger(t),
		sleep: func(d time.Duration) { slept = append(slept, d) },
	}

	proc.awaitDockerClient()

	assert.Equal(t, []time.Duration{time.Second}, slept)
}

func TestLifecycleFlags(t *testing.T) {
	cmd := &cobra.Command{}

	settings.RegisterAll(cmd)

	err := cmd.ParseFlags([]string{"--lifecycle-uid", "1000", "--lifecycle-gid", "1001"})
	require.NoError(t, err)

	cfg, _, err := settings.Load(newTestLogger(t), cmd, nil)
	require.NoError(t, err)

	assert.Equal(t, 1000, cfg.Lifecycle.UID, "lifecycle UID should be set to 1000")
	assert.Equal(t, 1001, cfg.Lifecycle.GID, "lifecycle GID should be set to 1001")
}

func TestGetAPIAddr(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     string
		expected string
	}{
		{
			name:     "empty host",
			host:     "",
			port:     "8080",
			expected: ":8080",
		},
		{
			name:     "IPv4 host",
			host:     "127.0.0.1",
			port:     "8080",
			expected: "127.0.0.1:8080",
		},
		{
			name:     "IPv6 host",
			host:     "::1",
			port:     "8080",
			expected: "[::1]:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := api.GetAPIAddr(tt.host, tt.port)
			assert.Equal(t, tt.expected, result)

			// Verify the formatted address is a valid TCP address
			_, err := net.ResolveTCPAddr("tcp", result)
			assert.NoError(t, err, "formatted address should be a valid TCP address")
		})
	}
}

// TestValidateRollingRestartDependenciesAcceptsCancelableContext verifies that
// update.ValidateRollingRestartDependencies properly accepts and uses a cancelable context.
func TestValidateRollingRestartDependenciesAcceptsCancelableContext(t *testing.T) {
	// Create a mock client
	mockClient := mockCmd.NewMockDockerClient(t)

	// Create a filter that accepts all containers
	filter := filters.Filter(func(_ filters.FilterableContainer) bool { return true })

	// Test with cancelable context - context should not be canceled
	t.Run("cancelable context without cancellation", func(t *testing.T) {
		ctx := t.Context()

		// Mock expects ListContainers to be called with the cancelable context
		mockClient.EXPECT().ListContainers(ctx, mock.Anything, mock.Anything).Return([]*container.Container{}, nil).Once()

		err := update.ValidateRollingRestartDependencies(newTestLogger(t), ctx, mockClient, filter, true)

		require.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	// Test that canceled context is properly propagated
	t.Run("canceled context is propagated to client", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel immediately
		cancel()

		// Mock expects ListContainers to be called with canceled context
		mockClient.EXPECT().ListContainers(ctx, mock.Anything, mock.Anything).Return(nil, context.Canceled).Once()

		err := update.ValidateRollingRestartDependencies(newTestLogger(t), ctx, mockClient, filter, true)

		// The function should return the error from ListContainers
		require.Error(t, err)
		mockClient.AssertExpectations(t)
	})

	// Test with timeout context
	t.Run("timeout context is propagated to client", func(t *testing.T) {
		// Use a short but non-trivial timeout. Windows timer resolution can
		// delay sub-15ms deadlines, so wait by polling rather than a fixed sleep.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()

		deadline := time.Now().Add(500 * time.Millisecond)
		for ctx.Err() == nil {
			require.True(t, time.Now().Before(deadline), "context deadline should expire within wait window")
			time.Sleep(5 * time.Millisecond)
		}

		require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)

		// Mock expects ListContainers to be called with timed out context
		mockClient.EXPECT().ListContainers(ctx, mock.Anything, mock.Anything).Return(nil, context.DeadlineExceeded).Once()

		err := update.ValidateRollingRestartDependencies(newTestLogger(t), ctx, mockClient, filter, true)

		// The function should return the error from ListContainers
		require.Error(t, err)
		mockClient.AssertExpectations(t)
	})
}

// TestNewDockerClient_FailureLeavesNoClient verifies that newDockerClient
// returns a nil client when the Docker client cannot be created. preRun and
// the exit path compare the client with nil, which an interface holding a nil
// pointer would not equal.
func TestNewDockerClient_FailureLeavesNoClient(t *testing.T) {
	t.Parallel()

	got, err := newDockerClient(t.Context(), newTestLogger(t), containerClient.ClientOptions{
		Docker: dockerConfig.Docker{Host: "tcp://[::1"},
	})
	require.Error(t, err)
	// assert.Nil counts an interface holding a nil pointer as nil, so the
	// interface is compared with nil directly.
	if got != nil {
		t.Errorf("newDockerClient returned a non-nil client with its error: %#v", got)
	}
}

func TestContextDeadlineExceededErrorHandling(t *testing.T) {
	tests := []struct {
		name               string
		err                error
		isDeadlineExceeded bool
		isCanceled         bool
	}{
		{
			name:               "DeadlineExceeded error",
			err:                context.DeadlineExceeded,
			isDeadlineExceeded: true,
			isCanceled:         false,
		},
		{
			name:               "Canceled error",
			err:                context.Canceled,
			isDeadlineExceeded: false,
			isCanceled:         true,
		},
		{
			name:               "Wrapped DeadlineExceeded",
			err:                fmt.Errorf("wrapped: %w", context.DeadlineExceeded),
			isDeadlineExceeded: true,
			isCanceled:         false,
		},
		{
			name:               "Wrapped Canceled",
			err:                fmt.Errorf("wrapped: %w", context.Canceled),
			isDeadlineExceeded: false,
			isCanceled:         true,
		},
		{
			name:               "Regular error",
			err:                errors.New("some error"),
			isDeadlineExceeded: false,
			isCanceled:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.isDeadlineExceeded, errors.Is(tt.err, context.DeadlineExceeded),
				"errors.Is should correctly identify DeadlineExceeded")
			assert.Equal(t, tt.isCanceled, errors.Is(tt.err, context.Canceled),
				"errors.Is should correctly identify Canceled")
		})
	}
}

// TestContainerLookupWithTimeoutContext verifies that container lookup functions properly
// handle timeout contexts using synctest.
func TestContainerLookupWithTimeoutContext(t *testing.T) {
	// Test case 1: Context deadline exceeded - verify DeadlineExceeded error is propagated
	t.Run("DeadlineExceeded error is properly propagated", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			// Set HOSTNAME so container detection doesn't fail early
			t.Setenv("HOSTNAME", "test-hostname")

			// Create a mock client
			mockClient := mockCmd.NewMockDockerClient(t)

			// Create a context that's already timed out
			ctx, cancel := context.WithTimeout(context.Background(), 0)
			defer cancel()

			// Wait for context to actually timeout
			time.Sleep(time.Millisecond)

			// Verify context has expired
			require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)

			// Mock expects GetCurrentContainerID to be called with expired context
			// and should return DeadlineExceeded error
			mockClient.EXPECT().ListContainers(ctx).Return(nil, context.DeadlineExceeded).Once()

			// Call GetCurrentContainerID which internally uses the client
			// The function will eventually call ListContainers with our expired context
			_, err := containerClient.GetCurrentContainerID(newTestLogger(t), ctx, fstest.MapFS{}, mockClient)

			// Verify error is propagated
			require.Error(t, err)
			require.ErrorIs(t, err, context.DeadlineExceeded)

			mockClient.AssertExpectations(t)
		})
	})

	// Test case 2: Context canceled - verify Canceled error is propagated
	t.Run("Canceled context is properly propagated", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			// Set HOSTNAME so container detection doesn't fail early
			t.Setenv("HOSTNAME", "test-hostname")

			// Create a mock client
			mockClient := mockCmd.NewMockDockerClient(t)

			ctx, cancel := context.WithCancel(context.Background())

			// Cancel immediately
			cancel()

			// Verify context is canceled
			require.ErrorIs(t, ctx.Err(), context.Canceled)

			// Mock expects GetCurrentContainerID to be called with canceled context
			mockClient.EXPECT().ListContainers(ctx).Return(nil, context.Canceled).Once()

			// Call GetCurrentContainerID
			_, err := containerClient.GetCurrentContainerID(newTestLogger(t), ctx, fstest.MapFS{}, mockClient)

			// Verify error is propagated
			require.Error(t, err)
			require.ErrorIs(t, err, context.Canceled)

			mockClient.AssertExpectations(t)
		})
	})
}

func TestRootCommand_FlagParsing(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "valid stop-timeout",
			args:    []string{"--stop-timeout", "30s"},
			wantErr: false,
		},
		{
			name:    "zero stop-timeout",
			args:    []string{"--stop-timeout", "0s"},
			wantErr: false,
		},
		{
			name:    "valid http-api-port",
			args:    []string{"--http-api-port", "9090"},
			wantErr: false,
		},
		{
			name:    "valid http-api-host",
			args:    []string{"--http-api-host", "127.0.0.1"},
			wantErr: false,
		},
		{
			// Hostnames parse as flag values but fail ValidateAPIHost at run time.
			name:    "http-api-host hostname parses as flag",
			args:    []string{"--http-api-host", "localhost"},
			wantErr: false,
		},
		{
			name:    "valid rolling-restart",
			args:    []string{"--rolling-restart"},
			wantErr: false,
		},
		{
			name:    "valid include-stopped",
			args:    []string{"--include-stopped"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRootCommand()
			settings.RegisterAll(cmd)
			cmd.SetArgs(tt.args)

			err := cmd.ParseFlags(tt.args)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestCommands_ErrorOutput verifies what Cobra prints when a command fails. A
// flag or argument that does not parse is printed with the usage text, while
// an error the command returns is left to Execute to report.
func TestCommands_ErrorOutput(t *testing.T) {
	tests := []struct {
		name string
		args []string
		// wantOutput lists text Cobra prints. Empty means it prints nothing.
		wantOutput []string
		// wantExitError reports whether the command returns an exitError.
		wantExitError bool
	}{
		{
			name:       "unknown flag",
			args:       []string{"--bogus"},
			wantOutput: []string{"Error: unknown flag: --bogus", "Usage:"},
		},
		{
			name:          "root command failure",
			args:          []string{"--log-format", "bogus"},
			wantExitError: true,
		},
		{
			name:       "notify-upgrade argument",
			args:       []string{"notify-upgrade", "extra"},
			wantOutput: []string{`Error: unknown command "extra" for "watchtower notify-upgrade"`, "Usage:"},
		},
		{
			name:          "notify-upgrade failure",
			args:          []string{"notify-upgrade", "--log-format", "bogus"},
			wantExitError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newExecuteCommand(t, newTestProcess(fstest.MapFS{}), tt.args...)

			var out bytes.Buffer

			root.SetOut(&out)
			root.SetErr(&out)

			err := root.Execute()
			require.Error(t, err)

			_, isExitError := errors.AsType[*exitError](err)
			assert.Equal(t, tt.wantExitError, isExitError)

			if len(tt.wantOutput) == 0 {
				assert.Empty(t, out.String())
			}

			for _, want := range tt.wantOutput {
				assert.Contains(t, out.String(), want)
			}
		})
	}
}

// TestIsContainerMainProcess verifies which processes count as the main
// process of a container. Processes started with docker exec have parent PID 0
// on most kernels and parent PID 1 on OpenVZ, where PID 1 still does not list
// them among its children. A host binary can have an init system as its parent.
func TestIsContainerMainProcess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		pid         int
		ppid        int
		inContainer bool
		// children is PID 1's children list, or nil when the kernel has none.
		children *string
		want     bool
	}{
		{name: "PID 1", pid: 1, ppid: 0, inContainer: true, want: true},
		{name: "child of the container's init", pid: 7, ppid: 1, inContainer: true, children: new("7\n"), want: true},
		{name: "one of several children of the container's init", pid: 7, ppid: 1, inContainer: true, children: new("3 7 12"), want: true},
		{name: "started with docker exec", pid: 8, ppid: 0, inContainer: true, children: new("7"), want: false},
		{name: "started with docker exec on OpenVZ", pid: 8, ppid: 1, inContainer: true, children: new("7"), want: false},
		{name: "PID 1 has no children", pid: 8, ppid: 1, inContainer: true, children: new(""), want: false},
		{name: "a listed PID only shares digits", pid: 7, ppid: 1, inContainer: true, children: new("17 70"), want: false},
		{name: "kernel without the children list", pid: 7, ppid: 1, inContainer: true, want: false},
		{name: "started by another process in the container", pid: 9, ppid: 8, inContainer: true, children: new("7"), want: false},
		{name: "host binary under an init system", pid: 4242, ppid: 1, inContainer: false, children: new("4242"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fsys := fstest.MapFS{}
			if tt.inContainer {
				fsys[".dockerenv"] = &fstest.MapFile{}
			}

			if tt.children != nil {
				fsys["proc/1/task/1/children"] = &fstest.MapFile{Data: []byte(*tt.children)}
			}

			proc := &process{
				fsys: fsys,
				pid:  func() int { return tt.pid },
				ppid: func() int { return tt.ppid },
			}

			assert.Equal(t, tt.want, proc.isContainerMainProcess())
		})
	}
}

// TestExitError_Error verifies the text of an exit error with and without a
// message and a cause.
func TestExitError_Error(t *testing.T) {
	t.Parallel()

	cause := errors.New("cause")

	tests := []struct {
		name string
		err  *exitError
		want string
	}{
		{name: "message and cause", err: &exitError{err: cause, msg: "Failed", code: 1}, want: "Failed: cause"},
		{name: "message only", err: &exitError{err: nil, msg: "Failed", code: 1}, want: "Failed"},
		{name: "cause only", err: &exitError{err: cause, msg: "", code: 1}, want: "cause"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.err.Error())
			assert.Equal(t, tt.err.err, errors.Unwrap(tt.err))
		})
	}
}

func TestRootCommand_HTTPAPIEndpointsFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "endpoints all",
			args: []string{"--http-api-endpoints=all"},
			want: []string{"all"},
		},
		{
			name: "endpoints list comma",
			args: []string{"--http-api-endpoints=health,metrics"},
			want: []string{"health", "metrics"},
		},
		{
			name: "endpoints repeated flags",
			args: []string{"--http-api-endpoints=health", "--http-api-endpoints=metrics"},
			want: []string{"health", "metrics"},
		},
		{
			name: "legacy update still registered",
			args: []string{"--http-api-update"},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRootCommand()
			settings.RegisterAll(cmd)
			cmd.SetArgs(tt.args)

			err := cmd.ParseFlags(tt.args)
			require.NoError(t, err)

			got, err := cmd.Flags().GetStringSlice("http-api-endpoints")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			// Branch-only endpoint booleans must not be registered.
			assert.Nil(t, cmd.Flags().Lookup("http-api-events"))
			assert.Nil(t, cmd.Flags().Lookup("http-api-check"))
			assert.Nil(t, cmd.Flags().Lookup("http-api-full"))

			// Main legacy flags remain for deprecation.
			assert.NotNil(t, cmd.Flags().Lookup("http-api-update"))
			assert.NotNil(t, cmd.Flags().Lookup("http-api-metrics"))
			assert.NotNil(t, cmd.Flags().Lookup("http-api-containers"))
		})
	}
}

func TestValidateAPIHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		host    string
		wantErr bool
	}{
		{name: "empty", host: "", wantErr: false},
		{name: "ipv4", host: "127.0.0.1", wantErr: false},
		{name: "ipv6", host: "::1", wantErr: false},
		{name: "hostname", host: "localhost", wantErr: true},
		{name: "dns name", host: "api.example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := settings.ValidateAPIHost(tt.host)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAnyHTTPAPIConfig(t *testing.T) {
	t.Parallel()

	assert.False(t, settings.AnyHTTPAPIConfig(settings.RunConfig{}))
	assert.True(t, settings.AnyHTTPAPIConfig(settings.RunConfig{APIToken: "secret"}))
	assert.True(t, settings.AnyHTTPAPIConfig(settings.RunConfig{TLSCertPath: "/cert.pem"}))
	assert.True(t, settings.AnyHTTPAPIConfig(settings.RunConfig{CORSAllowedOrigins: []string{"https://app.example.com"}}))
	assert.True(t, settings.AnyHTTPAPIConfig(settings.RunConfig{APIHostChanged: true}))
	assert.True(t, settings.AnyHTTPAPIConfig(settings.RunConfig{APIPortChanged: true}))
	assert.True(t, settings.AnyHTTPAPIConfig(settings.RunConfig{APIRateLimitChanged: true}))
}

func TestHTTPAPIEndpointsEnabled(t *testing.T) {
	t.Parallel()

	assert.False(t, settings.HTTPAPIEndpointsEnabled(settings.RunConfig{}))
	assert.True(t, settings.HTTPAPIEndpointsEnabled(settings.RunConfig{EnableHealthAPI: true}))
	assert.True(t, settings.HTTPAPIEndpointsEnabled(settings.RunConfig{EnableUpdateAPI: true}))
}

func TestRootCommand_ReviveStoppedFlagParsing(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "revive-stopped enabled",
			args:     []string{"--revive-stopped"},
			expected: true,
		},
		{
			name:     "revive-stopped default",
			args:     []string{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRootCommand()
			settings.RegisterAll(cmd)
			cmd.SetArgs(tt.args)

			err := cmd.ParseFlags(tt.args)
			require.NoError(t, err)

			value, err := cmd.PersistentFlags().GetBool("revive-stopped")
			require.NoError(t, err)
			assert.Equal(t, tt.expected, value)
		})
	}
}

// TestDisableRestarts verifies that disableRestarts sets the restart policy of
// Watchtower's own container to "no" and reports an error only when that
// fails, since the runtime may then start Watchtower again. A container whose
// policy is already "no" needs no update. A process other than the container's
// main process leaves the policy alone.
func TestDisableRestarts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// current is the inspected Watchtower container, or nil when it was
		// not inspected.
		current *container.Container
		// pid is the process ID Watchtower runs as.
		pid int
		// ppid is the parent process ID. The container's PID 1 lists PID 7
		// as its only child.
		ppid int
		// unknownID leaves Watchtower's own container ID unknown.
		unknownID bool
		// stopped cancels the process context, as a stop signal does.
		stopped    bool
		wantUpdate bool
		updateErr  error
		wantError  bool
	}{
		{name: "the update succeeds", current: currentWatchtower(), pid: 1, wantUpdate: true},
		{name: "the update fails", current: currentWatchtower(), pid: 1, wantUpdate: true, updateErr: errRunTestDocker, wantError: true},
		{name: "the container was not inspected", pid: 1, wantUpdate: true},
		{name: "the policy is already no", current: currentWatchtowerWithoutRestarts(), pid: 1, wantUpdate: false},
		{name: "not the container's main process", current: currentWatchtower(), pid: 42, wantUpdate: false},
		{name: "main process under an init", current: currentWatchtower(), pid: 7, ppid: 1, wantUpdate: true},
		{name: "probe started with docker exec on OpenVZ", current: currentWatchtower(), pid: 8, ppid: 1, wantUpdate: false},
		{name: "the container ID is not known", pid: 1, unknownID: true, wantUpdate: false, wantError: true},
		{name: "a stop signal is ending the process", current: currentWatchtower(), pid: 1, stopped: true, wantUpdate: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockClient := mockCmd.NewMockDockerClient(t)
			if tt.wantUpdate {
				mockClient.EXPECT().SetRestartPolicy(mock.Anything, container.ContainerID("c0c0"),
					dockerContainer.RestartPolicy{Name: dockerContainer.RestartPolicyDisabled}).
					Return(tt.updateErr).Once()
			}

			currentID := container.ContainerID("c0c0")
			if tt.unknownID {
				currentID = ""
			}

			proc := newRunProcess(t, runTestState{client: mockClient, current: tt.current, currentID: currentID})
			proc.fsys = fstest.MapFS{
				".dockerenv":             &fstest.MapFile{},
				"proc/1/task/1/children": &fstest.MapFile{Data: []byte("7")},
			}
			proc.pid = func() int { return tt.pid }
			proc.ppid = func() int { return tt.ppid }

			processCtx, cancel := context.WithCancel(t.Context())
			defer cancel()

			if tt.stopped {
				cancel()
			}

			log, logOutput := logcapture.New(zerolog.ErrorLevel)
			proc.log = log
			proc.disableRestarts(processCtx)

			if tt.wantError {
				assert.Contains(t, logOutput.String(), logging.RestartsKept)

				wantCause := errRunTestDocker
				if tt.unknownID {
					wantCause = errUnknownContainerID
				}

				assert.Contains(t, logOutput.String(), wantCause.Error())
			} else {
				assert.Empty(t, logOutput.String())
			}
		})
	}
}

// currentWatchtowerWithoutRestarts returns the Watchtower container the tests
// run as, with the restart policy "no".
func currentWatchtowerWithoutRestarts() *container.Container {
	return container.NewContainer(nil,
		&dockerContainer.InspectResponse{
			ID:         "c0c0",
			Name:       "/watchtower",
			Config:     &dockerContainer.Config{Labels: map[string]string{watchtowerLabelKey: "true"}},
			HostConfig: &dockerContainer.HostConfig{},
		},
		nil,
	)
}

// TestStoppedBy verifies which errors count as coming from a stop signal: only
// a cancellation, from a Docker call or wrapping the stop's cause, once the
// process context is canceled.
func TestStoppedBy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// stopped cancels the process context with a stop signal's cause.
		stopped bool
		err     error
		want    bool
	}{
		{name: "Docker call canceled by the stop", stopped: true, err: fmt.Errorf("list: %w", context.Canceled), want: true},
		{name: "step wrapping the stop's cause", stopped: true, err: fmt.Errorf("look up: %w", errRunTestStopSignal), want: true},
		{name: "unrelated failure during the stop", stopped: true, err: errRunTestDocker, want: false},
		{name: "cancellation without a stop", stopped: false, err: context.Canceled, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, stop := context.WithCancelCause(t.Context())
			defer stop(nil)

			if tt.stopped {
				stop(errRunTestStopSignal)
			}

			assert.Equal(t, tt.want, stoppedBy(ctx, tt.err))
		})
	}
}
