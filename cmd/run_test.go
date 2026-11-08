package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"
	dockerNetwork "github.com/moby/moby/api/types/network"

	"github.com/nicholas-fedor/watchtower/internal/container"
	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	mockContainerClient "github.com/nicholas-fedor/watchtower/internal/container/client/mocks"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	"github.com/nicholas-fedor/watchtower/internal/settings"
	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
	updateConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/update"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// runExitHelperEnv selects a run exit case when the test binary is re-executed
// as a subprocess. Empty means the parent process.
const runExitHelperEnv = "WATCHTOWER_CMD_RUN_EXIT_HELPER"

// Container labels used by the run fixtures.
const (
	watchtowerLabelKey   = "com.centurylinklabs.watchtower"
	scopeLabelKey        = "com.centurylinklabs.watchtower.scope"
	orchestratorLabelKey = "com.centurylinklabs.watchtower.ephemeral-orchestrator"
)

// runStopTimeout bounds the wait for run to return after its process context
// is canceled. It covers the HTTP API's shutdown grace period.
const runStopTimeout = 10 * time.Second

// apiToken is the HTTP API token the run tests serve the API with.
const apiToken = "test-token"

// errRunTestDocker is returned by the Docker client in the run tests.
var errRunTestDocker = errors.New("docker daemon error")

// withFilter matches the filter argument of a filtered ListContainers call.
// The mock records an unfiltered call with the context alone, and mock.Anything
// would also match that missing argument.
var withFilter = mock.MatchedBy(func([]types.Filter) bool { return true })

// runTestState is the process state run and runMain read from package
// variables, set by setRunState.
type runTestState struct {
	client    containerClient.Client
	current   types.Container
	currentID types.ContainerID
}

// setRunState sets the package variables run and runMain read, and restores
// every one of them when the test ends. Watchtower runs as the main process of
// its container. The Docker readiness sleep is skipped. The notifier has no
// services, so it sends nothing.
func setRunState(t *testing.T, state runTestState) {
	t.Helper()

	savedCfg := appCfg
	savedClient := client
	savedRateLimiter := rateLimiter
	savedNotifier := notifier
	savedCurrent := currentWatchtowerContainer
	savedCurrentID := currentWatchtowerContainerID
	savedCurrentUnknown := currentWatchtowerContainerUnknown
	savedRunUpdates := runUpdatesWithNotifications
	savedSleep := sleepFunc
	savedSignalContext := createSignalContext
	savedProcessID := processID
	savedParentProcessID := parentProcessID

	t.Cleanup(func() {
		appCfg = savedCfg
		client = savedClient
		rateLimiter = savedRateLimiter
		notifier = savedNotifier
		currentWatchtowerContainer = savedCurrent
		currentWatchtowerContainerID = savedCurrentID
		currentWatchtowerContainerUnknown = savedCurrentUnknown
		runUpdatesWithNotifications = savedRunUpdates
		sleepFunc = savedSleep
		createSignalContext = savedSignalContext
		processID = savedProcessID
		parentProcessID = savedParentProcessID
	})

	appCfg = settings.Config{}
	client = state.client
	rateLimiter = ratelimit.New()
	currentWatchtowerContainer = state.current
	currentWatchtowerContainerID = state.currentID
	currentWatchtowerContainerUnknown = false
	sleepFunc = func(time.Duration) {}
	processID = func() int { return 1 }
	parentProcessID = func() int { return 0 }

	created, err := notify.NewNotifier(new(zerolog.Nop()), notifyConfig.Notify{Level: "info"})
	require.NoError(t, err)

	notifier = created
	t.Cleanup(notifier.Close)
}

// newLoggerNotifier returns a notifier hooked to log whose logger:// service
// writes each notification it sends to log. It registers the hook that starts
// the sending worker and closes the notifier when the test ends.
//
// Parameters:
//   - t: Test that owns the notifier.
//   - log: Logger the notifier hooks and writes to.
//
// Returns:
//   - *notify.Notifier: The hooked notifier.
func newLoggerNotifier(t *testing.T, log *zerolog.Logger) *notify.Notifier {
	t.Helper()

	hooked, err := notify.NewNotifier(log, notifyConfig.Notify{Level: "info", URLs: []string{"logger://"}})
	require.NoError(t, err)
	t.Cleanup(hooked.Close)

	hooked.RegisterHook(log)

	return hooked
}

// runFixtureContainer returns a running container with the given ID, name,
// creation time, and labels. Its restart policy is "unless-stopped", so
// disabling its restarts sends a policy update.
func runFixtureContainer(id, name, created string, labels map[string]string) types.Container {
	return container.NewContainer(nil,
		&dockerContainer.InspectResponse{
			ID:      id,
			Name:    name,
			Image:   "sha256:" + id,
			Created: created,
			State:   &dockerContainer.State{Running: true},
			Config: &dockerContainer.Config{
				Image:  "nickfedor/watchtower:latest",
				Labels: labels,
			},
			HostConfig: &dockerContainer.HostConfig{
				RestartPolicy: dockerContainer.RestartPolicy{Name: dockerContainer.RestartPolicyUnlessStopped},
			},
		},
		&dockerImage.InspectResponse{ID: "sha256:" + id},
	)
}

// currentWatchtower returns the Watchtower container the tests run as.
func currentWatchtower() types.Container {
	return runFixtureContainer("c0c0", "/watchtower", "2026-10-08T12:00:00Z",
		map[string]string{watchtowerLabelKey: "true"})
}

// currentWatchtowerWithPorts returns the Watchtower container the tests run
// as, publishing its port 8080 on the host.
func currentWatchtowerWithPorts() types.Container {
	port := dockerNetwork.MustParsePort("8080/tcp")

	return container.NewContainer(nil,
		&dockerContainer.InspectResponse{
			ID:      "c0c0",
			Name:    "/watchtower",
			Image:   "sha256:c0c0",
			Created: "2026-10-08T12:00:00Z",
			State:   &dockerContainer.State{Running: true},
			Config: &dockerContainer.Config{
				Image:  "nickfedor/watchtower:latest",
				Labels: map[string]string{watchtowerLabelKey: "true"},
			},
			HostConfig: &dockerContainer.HostConfig{
				RestartPolicy: dockerContainer.RestartPolicy{Name: dockerContainer.RestartPolicyUnlessStopped},
				PortBindings: dockerNetwork.PortMap{
					port: {{HostIP: netip.MustParseAddr("0.0.0.0"), HostPort: port.Port()}},
				},
			},
		},
		&dockerImage.InspectResponse{ID: "sha256:c0c0"},
	)
}

// runCalls returns the Docker client calls in the order they were made, each
// with the container, image, or listing it targeted.
func runCalls(client *mockContainerClient.MockClient) []string {
	calls := make([]string, 0, len(client.Calls))

	for _, call := range client.Calls {
		entry := call.Method

		switch call.Method {
		case "ListContainers":
			if len(call.Arguments) > 1 {
				entry += " filtered"
			} else {
				entry += " all"
			}
		case "SetRestartPolicy":
			entry += " " + containerLabel(call.Arguments.Get(1)) + " " +
				string(call.Arguments.Get(2).(dockerContainer.RestartPolicy).Name)
		case "StopAndRemoveContainer":
			entry += fmt.Sprintf(" %s %s", containerLabel(call.Arguments.Get(1)), call.Arguments.Get(2))
		case "RemoveImageByID":
			entry += " " + string(call.Arguments.Get(1).(types.ImageID))
		case "GetCurrentWatchtowerContainer":
			entry += " " + call.Arguments.Get(1).(types.ContainerID).ShortID()
		}

		calls = append(calls, entry)
	}

	return calls
}

// containerLabel names a container argument by its name, or a container ID
// argument by its short ID. It returns "none" for a nil container or an empty
// ID.
func containerLabel(arg any) string {
	if id, ok := arg.(types.ContainerID); ok {
		if id == "" {
			return "none"
		}

		return id.ShortID()
	}

	c, ok := arg.(types.Container)
	if !ok || c == nil {
		return "none"
	}

	return strings.TrimPrefix(c.Name(), "/")
}

// runUntilCanceled runs the command in a synctest bubble until the scheduler
// is idle, then cancels the process context the way a signal would.
func runUntilCanceled(t *testing.T, args ...string) {
	t.Helper()

	cmd := newTestRootCommand(t, args...)

	synctest.Test(t, func(t *testing.T) {
		cancel, done := runInBackground(t, newRunProcess(), cmd)

		synctest.Wait()

		select {
		case <-done:
			require.FailNow(t, "run returned before the scheduler started waiting")
		default:
		}

		cancel()
		<-done
	})
}

// newRunProcess returns a process that logs nothing.
func newRunProcess() *process {
	return &process{log: new(zerolog.Nop())}
}

// localAPIArgs picks a free loopback address for the HTTP API and returns it
// with the command arguments that serve the API there. Those arguments set the
// API token and come before args.
//
// Parameters:
//   - t: Test that fails when no address is free.
//   - args: Further command arguments.
//
// Returns:
//   - string: The address in host:port form.
//   - []string: The API arguments followed by args.
func localAPIArgs(t *testing.T, args ...string) (string, []string) {
	t.Helper()

	var listenConfig net.ListenConfig

	ln, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	host, port, err := net.SplitHostPort(addr)
	require.NoError(t, err)

	return addr, append([]string{
		"--http-api-token", apiToken,
		"--http-api-host", host,
		"--http-api-port", port,
	}, args...)
}

// waitForAPI waits up to 5 seconds for the HTTP API at addr to accept
// connections, and fails the test when it does not.
//
// Parameters:
//   - t: Test that fails when the API does not listen in time.
//   - addr: Address the API listens on, in host:port form.
func waitForAPI(t *testing.T, addr string) {
	t.Helper()

	var dialer net.Dialer

	require.Eventually(t, func() bool {
		conn, err := dialer.DialContext(t.Context(), "tcp", addr)
		if err != nil {
			return false
		}

		_ = conn.Close()

		return true
	}, 5*time.Second, 20*time.Millisecond, "the HTTP API did not start listening")
}

// runInBackground runs the command in its own goroutine, with a process
// context that the returned cancel ends the way a signal would. When the test
// ends, it cancels that context and waits for run to return, so run stops
// before setRunState restores the package state it reads. A run still going
// after 10 seconds fails the test, and the wait goes on. Cleanups the caller
// registers later run first, so they can release anything run waits on.
//
// Parameters:
//   - t: Test that owns the run.
//   - proc: Process to run the command with.
//   - cmd: Command to run.
//
// Returns:
//   - context.CancelFunc: Cancels the process context.
//   - <-chan struct{}: Closed when run returns.
func runInBackground(t *testing.T, proc *process, cmd *cobra.Command) (context.CancelFunc, <-chan struct{}) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	createSignalContext = func(context.Context, ...os.Signal) (context.Context, context.CancelFunc) {
		return ctx, cancel
	}

	done := make(chan struct{})

	go func() {
		defer close(done)

		assert.NoError(t, proc.run(cmd, nil))
	}()

	t.Cleanup(func() {
		cancel()

		select {
		case <-done:
		case <-time.After(runStopTimeout):
			t.Error("run did not stop within 10 seconds of the test ending")
			<-done
		}
	})

	return cancel, done
}

// newAPIRequest returns a request to path on the HTTP API at addr, made with
// ctx and authorized with the API token. The client closes its connection
// after the response, so the API can stop without waiting for an idle one.
//
// Parameters:
//   - t: Test that fails when the request cannot be built.
//   - ctx: Context for the request.
//   - method: HTTP method of the request.
//   - addr: Address the API listens on, in host:port form.
//   - path: Path of the endpoint.
//
// Returns:
//   - *http.Request: The request.
func newAPIRequest(t *testing.T, ctx context.Context, method, addr, path string) *http.Request {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, method, "http://"+addr+path, nil)
	require.NoError(t, err)

	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Close = true

	return req
}

// TestRun_RunOnce records the Docker calls of a run-once update with no
// containers to update. The restart policy is disabled at the end in a
// container. A host binary has none to disable.
func TestRun_RunOnce(t *testing.T) {
	tests := []struct {
		name    string
		current types.Container
		// hostBinary runs Watchtower outside a container.
		hostBinary bool
		want       []string
	}{
		{
			name:       "outside a container",
			hostBinary: true,
			want:       []string{"GetVersion", "ListContainers filtered"},
		},
		{
			// The old-instance cleanup lists the containers again to find the
			// current one's scope, and skips when it is not listed.
			name:    "in a container",
			current: currentWatchtower(),
			want:    []string{"GetVersion", "ListContainers filtered", "ListContainers filtered", "SetRestartPolicy c0c0 no"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := mockContainerClient.NewMockClient(t)
			mockClient.EXPECT().GetVersion().Return("1.47")
			mockClient.EXPECT().ListContainers(mock.Anything, withFilter).Return(nil, nil)
			mockClient.EXPECT().SetRestartPolicy(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

			state := runTestState{client: mockClient, current: tt.current}
			if tt.current != nil {
				state.currentID = tt.current.ID()
			}

			setRunState(t, state)

			if tt.hostBinary {
				processID = func() int { return 4242 }
			}

			require.NoError(t, newRunProcess().run(newTestRootCommand(t, "--run-once"), nil))

			assert.Equal(t, tt.want, runCalls(mockClient))
		})
	}
}

// TestRun_Continuous records the Docker calls of continuous mode from startup
// until the process context is canceled, including the cleanup of older
// Watchtower instances and of orchestrator containers.
func TestRun_Continuous(t *testing.T) {
	older := runFixtureContainer("01d0", "/watchtower-older", "2026-10-01T12:00:00Z",
		map[string]string{watchtowerLabelKey: "true"})
	// The tests run in a synctest bubble, whose clock starts at midnight UTC on
	// 2000-01-01. One orchestrator was created a minute earlier and may still be
	// handing off, and the other an hour earlier and is orphaned.
	inFlight := runFixtureContainer("0c41", "/watchtower-orchestrator-new", "1999-12-31T23:59:00Z",
		map[string]string{orchestratorLabelKey: "true", scopeLabelKey: "other"})
	orchestrator := runFixtureContainer("0c40", "/watchtower-orchestrator", "1999-12-31T23:00:00Z",
		map[string]string{orchestratorLabelKey: "true", scopeLabelKey: "other"})

	tests := []struct {
		name string
		args []string
		// knownID keeps the current container's ID while leaving the container
		// itself unknown, as when its lookup failed.
		knownID bool
		// uncached runs without a current container, as outside a container or
		// after a failed lookup.
		uncached bool
		setup    func(client *mockContainerClient.MockClient, current types.Container)
		want     []string
	}{
		{
			name: "no other containers",
			setup: func(client *mockContainerClient.MockClient, current types.Container) {
				client.EXPECT().ListContainers(mock.Anything, withFilter).Return([]types.Container{current}, nil)
				client.EXPECT().ListContainers(mock.Anything).Return([]types.Container{current}, nil)
			},
			want: []string{"ListContainers filtered", "ListContainers all", "GetVersion"},
		},
		{
			// The removed image is reported to a slice that is then discarded.
			name: "older instance and its image are removed",
			args: []string{"--cleanup"},
			setup: func(client *mockContainerClient.MockClient, current types.Container) {
				client.EXPECT().ListContainers(mock.Anything, withFilter).Return([]types.Container{current, older}, nil)
				client.EXPECT().StopAndRemoveContainer(mock.Anything, older, 10*time.Minute).Return(nil)
				client.EXPECT().RemoveImageByID(mock.Anything, older.ImageID(), older.ImageName()).Return(nil)
				client.EXPECT().ListContainers(mock.Anything).Return([]types.Container{current}, nil)
			},
			want: []string{
				"ListContainers filtered",
				"StopAndRemoveContainer watchtower-older 10m0s",
				"RemoveImageByID sha256:01d0",
				"ListContainers all",
				"GetVersion",
			},
		},
		{
			// Without the current container, the cleanup cannot tell which
			// instance to keep, so it removes none of them.
			name:     "older instance is kept outside a container",
			args:     []string{"--cleanup"},
			uncached: true,
			setup: func(client *mockContainerClient.MockClient, current types.Container) {
				client.EXPECT().ListContainers(mock.Anything, withFilter).Return([]types.Container{current, older}, nil)
				client.EXPECT().ListContainers(mock.Anything).Return([]types.Container{current, older}, nil)
			},
			want: []string{"ListContainers filtered", "ListContainers all", "GetVersion"},
		},
		{
			name:     "older instance is kept when the current container lookup failed",
			args:     []string{"--cleanup"},
			uncached: true,
			knownID:  true,
			setup: func(client *mockContainerClient.MockClient, current types.Container) {
				client.EXPECT().ListContainers(mock.Anything, withFilter).Return([]types.Container{current, older}, nil)
				client.EXPECT().ListContainers(mock.Anything).Return([]types.Container{current, older}, nil)
			},
			want: []string{"ListContainers filtered", "ListContainers all", "GetVersion"},
		},
		{
			name: "orchestrator still handing off is kept",
			args: []string{"--scope", "prod"},
			setup: func(client *mockContainerClient.MockClient, current types.Container) {
				client.EXPECT().ListContainers(mock.Anything, withFilter).Return([]types.Container{current}, nil)
				client.EXPECT().ListContainers(mock.Anything).Return([]types.Container{current, inFlight}, nil)
			},
			want: []string{"ListContainers filtered", "ListContainers all", "GetVersion"},
		},
		{
			name: "orphaned orchestrator from another scope is removed",
			args: []string{"--scope", "prod"},
			setup: func(client *mockContainerClient.MockClient, current types.Container) {
				client.EXPECT().ListContainers(mock.Anything, withFilter).Return([]types.Container{current}, nil)
				client.EXPECT().ListContainers(mock.Anything).Return([]types.Container{current, orchestrator}, nil)
				client.EXPECT().StopAndRemoveContainer(mock.Anything, orchestrator, time.Duration(0)).Return(nil)
			},
			want: []string{
				"ListContainers filtered",
				"ListContainers all",
				"StopAndRemoveContainer watchtower-orchestrator 0s",
				"GetVersion",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := currentWatchtower()

			mockClient := mockContainerClient.NewMockClient(t)
			tt.setup(mockClient, current)
			mockClient.EXPECT().GetVersion().Return("1.47")

			state := runTestState{client: mockClient, current: current, currentID: current.ID()}
			if tt.uncached {
				state.current = nil
				state.currentID = ""
			}

			if tt.knownID {
				state.currentID = current.ID()
			}

			setRunState(t, state)

			runUntilCanceled(t, tt.args...)

			assert.Equal(t, tt.want, runCalls(mockClient))
		})
	}
}

// TestRun_PublishedPortsBlockSelfUpdates verifies that continuous mode does
// not let an update replace a Watchtower container that publishes host ports,
// unless self-updates are ephemeral. A Watchtower container whose self-update
// is skipped is never checked for a newer image, so the client mock fails the
// test if it is checked when it should not be.
func TestRun_PublishedPortsBlockSelfUpdates(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		ports     bool
		wantCheck bool
	}{
		{name: "published ports", ports: true, wantCheck: false},
		{
			name:      "published ports with ephemeral self-updates",
			args:      []string{"--ephemeral-self-update"},
			ports:     true,
			wantCheck: true,
		},
		{name: "no published ports", ports: false, wantCheck: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := currentWatchtower()
			if tt.ports {
				current = currentWatchtowerWithPorts()
			}

			mockClient := mockContainerClient.NewMockClient(t)
			mockClient.EXPECT().ListContainers(mock.Anything, withFilter).Return([]types.Container{current}, nil)
			mockClient.EXPECT().ListContainers(mock.Anything).Return([]types.Container{current}, nil)
			mockClient.EXPECT().GetVersion().Return("1.47")

			if tt.wantCheck {
				mockClient.EXPECT().IsContainerStale(mock.Anything, current, mock.Anything).
					Return(false, current.ImageID(), "", nil).Once()
			}

			setRunState(t, runTestState{client: mockClient, current: current, currentID: current.ID()})

			runUntilCanceled(t, append([]string{"--update-on-start"}, tt.args...)...)
		})
	}
}

// TestRun_ScheduledUpdate verifies that continuous mode passes its schedule to
// the scheduler. With an hourly schedule and no update at startup, nothing
// checks the current container for a newer image before the hour. The first
// scheduled update checks it once the hour has passed.
func TestRun_ScheduledUpdate(t *testing.T) {
	current := currentWatchtower()

	// checks counts the staleness checks of the current container.
	var checks atomic.Int32

	mockClient := mockContainerClient.NewMockClient(t)
	mockClient.EXPECT().ListContainers(mock.Anything, withFilter).Return([]types.Container{current}, nil)
	mockClient.EXPECT().ListContainers(mock.Anything).Return([]types.Container{current}, nil)
	mockClient.EXPECT().GetVersion().Return("1.47")
	mockClient.EXPECT().IsContainerStale(mock.Anything, current, mock.Anything).
		RunAndReturn(func(context.Context, types.Container, updateConfig.Policy) (bool, types.ImageID, string, error) {
			checks.Add(1)

			return false, current.ImageID(), "", nil
		}).Maybe()

	setRunState(t, runTestState{client: mockClient, current: current, currentID: current.ID()})

	cmd := newTestRootCommand(t, "--schedule", "@every 1h")

	synctest.Test(t, func(t *testing.T) {
		cancel, done := runInBackground(t, newRunProcess(), cmd)

		time.Sleep(59 * time.Minute)
		synctest.Wait()
		assert.Zero(t, checks.Load(), "an update ran before the hour")

		time.Sleep(2 * time.Minute)
		synctest.Wait()
		assert.Equal(t, int32(1), checks.Load(), "the scheduled update did not run once after the hour")

		cancel()
		<-done
	})
}

// TestRun_UpdatesAfterCleanup verifies that once startup has removed an older
// Watchtower instance, continuous mode runs no update at startup and its first
// scheduled run leaves Watchtower's own container alone. This instance is the
// older one's replacement, so it was just updated. The client mock fails the
// test if the current container is checked for a newer image.
func TestRun_UpdatesAfterCleanup(t *testing.T) {
	current := currentWatchtower()
	older := runFixtureContainer("01d0", "/watchtower-older", "2026-10-01T12:00:00Z",
		map[string]string{watchtowerLabelKey: "true"})

	// scheduledListings counts the filtered container listings after the
	// startup cleanup's, which show that the scheduled run happened.
	var scheduledListings atomic.Int32

	mockClient := mockContainerClient.NewMockClient(t)
	mockClient.EXPECT().ListContainers(mock.Anything, withFilter).Return([]types.Container{current, older}, nil).Once()
	mockClient.EXPECT().StopAndRemoveContainer(mock.Anything, older, 10*time.Minute).Return(nil)
	mockClient.EXPECT().RemoveImageByID(mock.Anything, older.ImageID(), older.ImageName()).Return(nil)
	mockClient.EXPECT().ListContainers(mock.Anything).Return([]types.Container{current}, nil)
	mockClient.EXPECT().GetVersion().Return("1.47")
	mockClient.EXPECT().ListContainers(mock.Anything, withFilter).
		RunAndReturn(func(context.Context, ...types.Filter) ([]types.Container, error) {
			scheduledListings.Add(1)

			return []types.Container{current}, nil
		})

	setRunState(t, runTestState{client: mockClient, current: current, currentID: current.ID()})

	cmd := newTestRootCommand(t, "--cleanup", "--update-on-start", "--schedule", "@every 1h")

	synctest.Test(t, func(t *testing.T) {
		cancel, done := runInBackground(t, newRunProcess(), cmd)

		synctest.Wait()
		assert.Zero(t, scheduledListings.Load(), "an update ran at startup")

		time.Sleep(61 * time.Minute)
		synctest.Wait()
		assert.Positive(t, scheduledListings.Load(), "the scheduled run did not happen")

		cancel()
		<-done
	})
}

// TestRun_WaitsForAPIDrain verifies that continuous mode with the HTTP API in
// the background does not return until the API has finished a request it was
// serving when the process was told to stop. The request lists containers, so
// it does not hold the update lock that the scheduler also waits for. A line
// the request logs while the API drains still reaches the notifier, which
// closes only after the API has stopped.
func TestRun_WaitsForAPIDrain(t *testing.T) {
	current := currentWatchtower()

	entered := make(chan struct{})
	release := make(chan struct{})

	releaseRequest := sync.OnceFunc(func() { close(release) })

	notifierLog, notifierLogs := logcapture.New(zerolog.InfoLevel)
	drainNotifier := newLoggerNotifier(t, notifierLog)

	mockClient := mockContainerClient.NewMockClient(t)
	// The startup cleanup of older instances lists the containers first.
	mockClient.EXPECT().ListContainers(mock.Anything, withFilter).Return([]types.Container{current}, nil).Once()
	mockClient.EXPECT().ListContainers(mock.Anything).Return([]types.Container{current}, nil)
	// The API request then lists them, and stays open until released.
	mockClient.EXPECT().ListContainers(mock.Anything, withFilter).
		RunAndReturn(func(context.Context, ...types.Filter) ([]types.Container, error) {
			close(entered)
			<-release

			// The request logs through the hooked logger while the API drains.
			notifierLog.Warn().Msg("Logged while the HTTP API drains")

			return []types.Container{}, nil
		}).Once()
	mockClient.EXPECT().GetVersion().Return("1.47").Maybe()

	setRunState(t, runTestState{client: mockClient, current: current, currentID: current.ID()})

	notifier = drainNotifier

	addr, args := localAPIArgs(t, "--http-api-endpoints=containers", "--http-api-periodic-polls")
	cmd := newTestRootCommand(t, args...)

	cancelRun, done := runInBackground(t, newRunProcess(), cmd)

	// Release the request even when the test fails early, so the API and run
	// do not stay blocked after the test ends.
	t.Cleanup(releaseRequest)

	waitForAPI(t, addr)

	req := newAPIRequest(t, t.Context(), http.MethodGet, addr, "/v1/containers")

	statuses := make(chan int, 1)

	go func() {
		resp, doErr := http.DefaultClient.Do(req)
		if doErr != nil {
			statuses <- 0

			return
		}

		_ = resp.Body.Close()
		statuses <- resp.StatusCode
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the API request did not reach the Docker client")
	}

	// Stop the process the way a signal would. The scheduler returns, and run
	// then waits for the API to finish the request.
	cancelRun()

	select {
	case <-done:
		t.Fatal("run returned while the HTTP API was still serving a request")
	case <-time.After(300 * time.Millisecond):
	}

	releaseRequest()

	select {
	case status := <-statuses:
		assert.Equal(t, http.StatusOK, status)
	case <-time.After(5 * time.Second):
		t.Fatal("the API request did not finish after it was released")
	}

	select {
	case <-done:
	case <-time.After(runStopTimeout):
		t.Fatal("run did not return after the HTTP API finished its request")
	}

	// The process closes the notifier when it ends, after run returns. Closing
	// waits for the worker to send what is queued. A notifier closed during the
	// drain would have dropped the line instead.
	newRunProcess().exit(cmd, nil)

	assert.Contains(t, notifierLogs.String(), "Shoutrrr: Logged while the HTTP API drains")
}

// TestRun_APISharesUpdateLock verifies that continuous mode gives the HTTP API
// the same update lock as the scheduler. A full update requested through the
// API while the update at startup runs is turned away with 429, instead of
// running beside it. The update at startup holds the lock while its first
// staleness check waits for the test.
func TestRun_APISharesUpdateLock(t *testing.T) {
	current := currentWatchtower()

	entered := make(chan struct{})
	release := make(chan struct{})

	mockClient := mockContainerClient.NewMockClient(t)
	mockClient.EXPECT().ListContainers(mock.Anything, withFilter).Return([]types.Container{current}, nil)
	mockClient.EXPECT().ListContainers(mock.Anything).Return([]types.Container{current}, nil)
	mockClient.EXPECT().GetVersion().Return("1.47").Maybe()
	// The update at startup's staleness check waits for the test. A second
	// update, which a separate lock would let run, finds the container fresh.
	mockClient.EXPECT().IsContainerStale(mock.Anything, current, mock.Anything).
		RunAndReturn(func(context.Context, types.Container, updateConfig.Policy) (bool, types.ImageID, string, error) {
			close(entered)
			<-release

			return false, current.ImageID(), "", nil
		}).Once()
	mockClient.EXPECT().IsContainerStale(mock.Anything, current, mock.Anything).
		Return(false, current.ImageID(), "", nil).Maybe()

	setRunState(t, runTestState{client: mockClient, current: current, currentID: current.ID()})

	addr, args := localAPIArgs(t, "--update-on-start", "--http-api-endpoints=update", "--http-api-periodic-polls")
	cmd := newTestRootCommand(t, args...)

	runInBackground(t, newRunProcess(), cmd)

	// Release the update when the test ends, before run is stopped.
	t.Cleanup(func() { close(release) })

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the update at startup did not reach the staleness check")
	}

	waitForAPI(t, addr)

	// Bound the request, so an API update that waits for the lock fails the
	// test instead of waiting for the cleanup that releases the lock.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	resp, err := http.DefaultClient.Do(newAPIRequest(t, ctx, http.MethodPost, addr, "/v1/update"))
	require.NoError(t, err)

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	require.NoError(t, err)
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode,
		"the API update ran beside the update at startup")
	// The rate limiter also answers 429, with no body.
	assert.Contains(t, string(body), "another update is already running")
}

// TestRun_APIOnlyWritesOneStartupMessage verifies that in API-only mode, where
// the HTTP API writes the startup message and serves until shutdown, the
// scheduler that runs after it at shutdown does not write the message again.
func TestRun_APIOnlyWritesOneStartupMessage(t *testing.T) {
	current := currentWatchtower()

	mockClient := mockContainerClient.NewMockClient(t)
	mockClient.EXPECT().ListContainers(mock.Anything, withFilter).Return([]types.Container{current}, nil)
	mockClient.EXPECT().ListContainers(mock.Anything).Return([]types.Container{current}, nil)
	mockClient.EXPECT().GetVersion().Return("1.47").Maybe()

	setRunState(t, runTestState{client: mockClient, current: current, currentID: current.ID()})

	addr, args := localAPIArgs(t, "--http-api-endpoints=update")
	cmd := newTestRootCommand(t, args...)

	log, output := logcapture.New(zerolog.InfoLevel)
	cancelRun, done := runInBackground(t, &process{log: log}, cmd)

	waitForAPI(t, addr)
	cancelRun()

	select {
	case <-done:
	case <-time.After(runStopTimeout):
		t.Fatal("run did not return after the process context was canceled")
	}

	assert.Equal(t, 1, strings.Count(output.String(), "HTTP API enabled and periodic updates disabled"),
		"the startup message was not written exactly once")
}

// TestRun_HealthCheck verifies that the health check mode returns without
// touching Docker when Watchtower is not the container's main process.
func TestRun_HealthCheck(t *testing.T) {
	mockClient := mockContainerClient.NewMockClient(t)
	setRunState(t, runTestState{client: mockClient})

	// A health check probe runs beside the container's main process.
	processID = func() int { return 42 }

	require.NoError(t, newRunProcess().run(newTestRootCommand(t, "--health-check"), nil))

	assert.Empty(t, runCalls(mockClient))
}

// TestRun_Exit verifies how run ends the process when it cannot continue. It
// disables the restart policy of the Watchtower container first and reports
// the exit in a notification. Watchtower holds every line until its startup
// message. Because that message never comes, closing the notifier at the exit
// is what sends the line.
func TestRun_Exit(t *testing.T) {
	if helperCase := os.Getenv(runExitHelperEnv); helperCase != "" {
		runExitHelper(t, helperCase)

		// Every helper case must end the process. Reaching here means it did not.
		os.Exit(2)
	}

	tests := []struct {
		helperCase string
		// wantOutput is what the exit writes. For a failure that reaches
		// notifications, it is the start of the notification, which gives the
		// logged message and its cause.
		wantOutput string
	}{
		{
			// Loading the configuration rejects the combination, so runMain's own
			// check for it is never reached through run. Because a real start
			// rejects it in preRun before notifications are set up, the case
			// runs without them.
			helperCase: "rolling-restart-with-monitor-only",
			wantOutput: "rolling-restart and monitor-only cannot both be enabled",
		},
		{
			// The run configuration is checked after the configuration loads.
			// A failure there happens again on the next start too.
			helperCase: "invalid-run-configuration",
			wantOutput: "Shoutrrr: Failed to build run configuration: invalid http-api-host",
		},
		{
			helperCase: "rolling-restart-validation-fails",
			wantOutput: "Shoutrrr: Rolling restart compatibility validation failed: " + errRunTestDocker.Error(),
		},
		{helperCase: "api-without-token", wantOutput: "Shoutrrr: API setup failed: API token is empty or unset"},
	}

	for _, tt := range tests {
		t.Run(tt.helperCase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRun_Exit$", "-test.v=false")

			cmd.Env = append(os.Environ(), runExitHelperEnv+"="+tt.helperCase)

			out, err := cmd.CombinedOutput()

			var exitErr *exec.ExitError

			require.ErrorAs(t, err, &exitErr, "output:\n%s", out)
			assert.Equal(t, 1, exitErr.ExitCode(), "output:\n%s", out)
			assert.Contains(t, string(out), "SetRestartPolicy c0c0 no",
				"the restart policy is disabled before the process ends")
			assert.Contains(t, string(out), tt.wantOutput)
		})
	}
}

// runExitHelper runs the named exit case in the re-executed test binary. The
// Docker client prints each restart policy change so the parent can check it.
func runExitHelper(t *testing.T, helperCase string) {
	t.Helper()

	current := currentWatchtower()

	mockClient := mockContainerClient.NewMockClient(t)
	mockClient.EXPECT().GetVersion().Return("1.47").Maybe()
	mockClient.EXPECT().SetRestartPolicy(mock.Anything, mock.Anything, mock.Anything).
		Run(func(_ context.Context, id types.ContainerID, policy dockerContainer.RestartPolicy) {
			fmt.Fprintf(os.Stderr, "SetRestartPolicy %s %s\n", containerLabel(id), policy.Name)
		}).Return(nil)

	var args []string

	// notified gives the case a notifier, as when the failure comes after
	// startup has set notifications up.
	notified := true

	switch helperCase {
	case "rolling-restart-with-monitor-only":
		args = []string{"--rolling-restart", "--monitor-only"}
		notified = false
	case "invalid-run-configuration":
		args = []string{"--http-api-host", "not-an-ip"}
	case "rolling-restart-validation-fails":
		args = []string{"--rolling-restart"}

		mockClient.EXPECT().ListContainers(mock.Anything, withFilter).Return(nil, errRunTestDocker)
	case "api-without-token":
		args = []string{"--http-api-update"}

		mockClient.EXPECT().ListContainers(mock.Anything, withFilter).Return([]types.Container{current}, nil)
		mockClient.EXPECT().ListContainers(mock.Anything).Return([]types.Container{current}, nil)
	default:
		panic("unknown run exit helper case " + helperCase)
	}

	setRunState(t, runTestState{client: mockClient, current: current, currentID: current.ID()})

	// Watchtower runs as the main process of its container.
	processID = func() int { return 1 }

	log := logging.New(os.Stderr, logging.InfoLevel)

	// The notifier holds every line until the startup message, as at startup.
	if notified {
		notifier = newLoggerNotifier(t, log)
	}

	cmd := newTestRootCommand(t, args...)

	proc := &process{log: log}
	proc.exit(cmd, proc.run(cmd, nil))
}

// TestRun_OldInstanceStops verifies that continuous mode stops on its own
// when an update finds that Watchtower runs in an old instance's container,
// after disabling that container's restart policy, instead of continuing to
// run until it receives a signal.
func TestRun_OldInstanceStops(t *testing.T) {
	oldSelf := runFixtureContainer("01d0", "/watchtower-old-01d0", "2026-10-01T12:00:00Z",
		map[string]string{watchtowerLabelKey: "true"})

	mockClient := mockContainerClient.NewMockClient(t)
	mockClient.EXPECT().ListContainers(mock.Anything, withFilter).Return([]types.Container{oldSelf}, nil)
	mockClient.EXPECT().ListContainers(mock.Anything).Return([]types.Container{oldSelf}, nil)
	mockClient.EXPECT().GetVersion().Return("1.47").Maybe()
	mockClient.EXPECT().SetRestartPolicy(mock.Anything, oldSelf.ID(),
		dockerContainer.RestartPolicy{Name: dockerContainer.RestartPolicyDisabled}).Return(nil)

	setRunState(t, runTestState{client: mockClient, current: oldSelf, currentID: oldSelf.ID()})

	cmd := newTestRootCommand(t, "--update-on-start")

	synctest.Test(t, func(t *testing.T) {
		_, done := runInBackground(t, newRunProcess(), cmd)

		synctest.Wait()

		select {
		case <-done:
		default:
			require.FailNow(t, "run kept running after detecting an old instance")
		}
	})
}
