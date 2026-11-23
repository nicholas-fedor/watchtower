package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"

	mockCmd "github.com/nicholas-fedor/watchtower/cmd/mocks"
	"github.com/nicholas-fedor/watchtower/internal/container"
	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	"github.com/nicholas-fedor/watchtower/internal/container/ephemeral"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/settings"
	dockerConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/docker"
)

// preRunExitHelperEnv selects a preRun exit case when the test binary is
// re-executed as a subprocess. Empty means the parent process.
const preRunExitHelperEnv = "WATCHTOWER_CMD_PRE_RUN_EXIT_HELPER"

// selfID is the container ID the preRun tests detect for Watchtower itself.
var selfID = container.ContainerID(strings.Repeat("c", 64))

// selfDetection is what the self container ID detection can read: the
// mountinfo and cgroup files, and the HOSTNAME variable. Empty means absent.
type selfDetection struct {
	mountinfo string
	cgroup    string
	hostname  string
	// marker creates the Docker container marker file.
	marker bool
}

// mountinfoWithSelf is a mountinfo file that names the Watchtower container.
func mountinfoWithSelf() string {
	return "1234 24 0:52 /var/lib/docker/containers/" + string(selfID) +
		"/hostname /etc/hostname rw,relatime - ext4 /dev/sda1 rw\n"
}

// cgroupWithSelf is a cgroup file that names the Watchtower container.
func cgroupWithSelf() string {
	return "12:memory:/docker/" + string(selfID) + "\n"
}

// newPreRunProcess returns a test process for preRun, holding only what
// Execute sets before preRun runs.
//
// Its client factory returns client, and self container ID detection reads
// only detection. The Docker variables preRun reads and sets are cleared and
// restored when the test ends.
//
// Parameters:
//   - t: Test that owns the process.
//   - client: Docker client the process's client factory returns.
//   - detection: Files self container ID detection reads.
//
// Returns:
//   - *process: The process to run preRun with.
func newPreRunProcess(t *testing.T, client DockerClient, detection selfDetection) *process {
	t.Helper()

	// io/fs paths are relative to the root, so the absolute paths drop their
	// leading slash.
	fsys := fstest.MapFS{}
	writeFile := func(path, content string) {
		fsys[strings.TrimPrefix(path, "/")] = &fstest.MapFile{Data: []byte(content)}
	}

	if detection.marker {
		writeFile("/.dockerenv", "")
	}

	if detection.mountinfo != "" {
		writeFile("/proc/self/mountinfo", detection.mountinfo)
	}

	if detection.cgroup != "" {
		writeFile(fmt.Sprintf("/proc/%d/cgroup", os.Getpid()), detection.cgroup)
	}

	proc := newTestProcess(fsys)
	proc.newClient = func(context.Context, *zerolog.Logger, containerClient.ClientOptions) (DockerClient, error) {
		return client, nil
	}

	// preRun creates the notifier, except for a health check or a startup that
	// fails first.
	t.Cleanup(func() {
		if proc.notifier != nil {
			proc.notifier.Close()
		}
	})

	for _, key := range []string{"HOSTNAME", "DOCKER_HOST", "DOCKER_TLS_VERIFY", "DOCKER_API_VERSION", "DOCKER_CERT_PATH"} {
		t.Setenv(key, "")
		require.NoError(t, os.Unsetenv(key))
	}

	if detection.hostname != "" {
		t.Setenv("HOSTNAME", detection.hostname)
	}

	return proc
}

// selfContainer returns the Watchtower container preRun detects, with the
// given name and container hostname. Its restart policy is "unless-stopped",
// so disabling its restarts sends a policy update.
func selfContainer(name, hostname string) *container.Container {
	return container.NewContainer(nil,
		&dockerContainer.InspectResponse{
			ID:      string(selfID),
			Name:    name,
			Image:   "sha256:" + string(selfID),
			Created: "2026-10-08T12:00:00Z",
			State:   &dockerContainer.State{Running: true},
			Config: &dockerContainer.Config{
				Hostname: hostname,
				Image:    "nickfedor/watchtower:latest",
				Labels:   map[string]string{watchtowerLabelKey: "true"},
			},
			HostConfig: &dockerContainer.HostConfig{
				RestartPolicy: dockerContainer.RestartPolicy{Name: dockerContainer.RestartPolicyUnlessStopped},
			},
		},
		&dockerImage.InspectResponse{ID: "sha256:" + string(selfID)},
	)
}

// Context matchers for the inspect of the current container. withDeadline
// matches a context that has a deadline, and withoutDeadline one that has not.
var (
	withDeadline = mock.MatchedBy(func(ctx context.Context) bool {
		_, ok := ctx.Deadline()

		return ok
	})
	withoutDeadline = mock.MatchedBy(func(ctx context.Context) bool {
		_, ok := ctx.Deadline()

		return !ok
	})
)

// TestPreRun_SelfDetection records how preRun finds the Watchtower container
// it runs in, and what it keeps and warns about when detection or the lookup
// fails. A process in a container whose ID is not known is marked unknown. A
// process outside a container is not. A failed lookup keeps the detected ID.
func TestPreRun_SelfDetection(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		detection selfDetection
		setup     func(client *mockCmd.MockDockerClient)
		wantID    container.ContainerID
		wantName  string
		wantCalls []string
		// wantUnknown is whether the own container is marked unknown.
		wantUnknown bool
		// wantWarning is the start of the warning preRun logs, and wantError
		// the error it carries. Empty means neither startup warning is logged.
		wantWarning string
		wantError   string
	}{
		{
			name:      "outside a container",
			wantCalls: []string{},
		},
		{
			name:        "detection fails in a container",
			detection:   selfDetection{marker: true},
			wantCalls:   []string{},
			wantUnknown: true,
			wantWarning: "Could not identify Watchtower's own container",
			wantError:   "failed to detect current container ID",
		},
		{
			name:      "found from mountinfo",
			detection: selfDetection{mountinfo: mountinfoWithSelf()},
			setup: func(client *mockCmd.MockDockerClient) {
				client.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, selfID).
					Return(selfContainer("/watchtower", ""), nil)
			},
			wantID:    selfID,
			wantName:  "watchtower",
			wantCalls: []string{"GetCurrentWatchtowerContainer cccccccccccc"},
		},
		{
			name:      "found from cgroup",
			detection: selfDetection{cgroup: cgroupWithSelf()},
			setup: func(client *mockCmd.MockDockerClient) {
				client.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, selfID).
					Return(selfContainer("/watchtower", ""), nil)
			},
			wantID:    selfID,
			wantName:  "watchtower",
			wantCalls: []string{"GetCurrentWatchtowerContainer cccccccccccc"},
		},
		{
			name:      "found from hostname",
			detection: selfDetection{hostname: "wt-host"},
			setup: func(client *mockCmd.MockDockerClient) {
				self := selfContainer("/watchtower", "wt-host")
				client.EXPECT().ListContainers(mock.Anything).Return([]*container.Container{self}, nil)
				client.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, selfID).Return(self, nil)
			},
			wantID:    selfID,
			wantName:  "watchtower",
			wantCalls: []string{"ListContainers all", "GetCurrentWatchtowerContainer cccccccccccc"},
		},
		{
			// A scope set explicitly does not depend on the container's label,
			// so preRun continues without the container. The inspect bounds
			// its own retries, so preRun passes a context without a deadline.
			name:      "lookup fails with an explicit scope and the ID is kept",
			args:      []string{"--scope", "prod"},
			detection: selfDetection{mountinfo: mountinfoWithSelf()},
			setup: func(client *mockCmd.MockDockerClient) {
				client.EXPECT().GetCurrentWatchtowerContainer(withoutDeadline, selfID).Return(nil, errRunTestDocker)
			},
			wantID:      selfID,
			wantCalls:   []string{"GetCurrentWatchtowerContainer cccccccccccc"},
			wantWarning: "Could not inspect Watchtower's own container",
			wantError:   errRunTestDocker.Error(),
		},
		{
			// A health check runs no updates, so it continues without a scope.
			// Its inspect shares the lookup's deadline instead of waiting for
			// every retry. Because a health check probe runs often, its
			// warning stays in the log and never reaches notifications.
			name:      "lookup fails during a health check and the ID is kept",
			args:      []string{"--health-check"},
			detection: selfDetection{mountinfo: mountinfoWithSelf()},
			setup: func(client *mockCmd.MockDockerClient) {
				client.EXPECT().GetCurrentWatchtowerContainer(withDeadline, selfID).
					Return(nil, fmt.Errorf("inspect: %w", context.DeadlineExceeded))
			},
			wantID:      selfID,
			wantCalls:   []string{"GetCurrentWatchtowerContainer cccccccccccc"},
			wantWarning: "Could not inspect Watchtower's own container",
			wantError:   context.DeadlineExceeded.Error(),
		},
		{
			// The daemon has no container with the detected ID, so Watchtower
			// has none of its own there. preRun treats that as a failed
			// detection and does not try again.
			name:      "detected ID names no container and is cleared",
			detection: selfDetection{mountinfo: mountinfoWithSelf(), marker: true},
			setup: func(client *mockCmd.MockDockerClient) {
				client.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, selfID).
					Return(nil, fmt.Errorf("inspect: %w", cerrdefs.ErrNotFound))
			},
			wantCalls:   []string{"GetCurrentWatchtowerContainer cccccccccccc"},
			wantUnknown: true,
			wantWarning: "Could not identify Watchtower's own container",
			wantError:   cerrdefs.ErrNotFound.Error(),
		},
		{
			// A health check probe can run in an old container during a
			// self-update. It changes no containers, so it neither starts an
			// orphaned replacement that the handoff is about to start nor
			// disables the old container's restarts.
			name:      "old instance continues in a health check",
			args:      []string{"--health-check"},
			detection: selfDetection{mountinfo: mountinfoWithSelf()},
			setup: func(client *mockCmd.MockDockerClient) {
				client.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, selfID).
					Return(selfContainer("/watchtower-old-cccccccccccc", ""), nil)
			},
			wantID:    selfID,
			wantName:  "watchtower-old-cccccccccccc",
			wantCalls: []string{"GetCurrentWatchtowerContainer cccccccccccc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := mockCmd.NewMockDockerClient(t)
			if tt.setup != nil {
				tt.setup(mockClient)
			}

			proc := newPreRunProcess(t, mockClient, tt.detection)

			readStderr := captureStderr(t)
			args := slices.Concat(tt.args, []string{"--log-level", "warn", "--log-format", "logfmt"})

			require.NoError(t, proc.preRun(newTestRootCommand(t, args...), nil))

			logOutput := readStderr()
			if tt.wantWarning == "" {
				assert.NotContains(t, logOutput, "Could not identify Watchtower's own container")
				assert.NotContains(t, logOutput, "Could not inspect Watchtower's own container")
			} else {
				assert.Contains(t, logOutput, tt.wantWarning)
				assert.Contains(t, logOutput, tt.wantError)
			}

			assert.Equal(t, tt.wantID, proc.currentContainerID)
			assert.Equal(t, tt.wantUnknown, proc.currentContainerUnknown())
			assert.Equal(t, tt.wantName, containerName(proc.currentContainer))
			assert.Equal(t, tt.wantCalls, runCalls(mockClient))
			assert.Same(t, mockClient, proc.client, "preRun uses the client from the client factory")

			if slices.Contains(tt.args, "--health-check") {
				assert.Nil(t, proc.notifier, "preRun sets up no notifications")
			} else {
				assert.NotNil(t, proc.notifier, "preRun creates the notifier")
			}
		})
	}
}

// captureStderr points [os.Stderr] at a file until the test ends. The logger
// preRun builds then writes there, and the returned function reads what it
// wrote. The test must not run in parallel, since [os.Stderr] is process-wide.
func captureStderr(t *testing.T) func() string {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "stderr")
	require.NoError(t, err)

	saved := os.Stderr
	os.Stderr = file

	t.Cleanup(func() {
		os.Stderr = saved
		_ = file.Close()
	})

	return func() string {
		data, err := os.ReadFile(file.Name())
		require.NoError(t, err)

		return string(data)
	}
}

// containerName names a container, or returns "" when it is nil.
func containerName(c *container.Container) string {
	if c == nil {
		return ""
	}

	return strings.TrimPrefix(c.Name(), "/")
}

// TestPreRun_Configuration verifies that preRun loads the configuration from
// the flags, passes the Docker connection settings to the Docker client, and
// exports them to the environment, where Compose rebuilds read them.
func TestPreRun_Configuration(t *testing.T) {
	mockClient := mockCmd.NewMockDockerClient(t)
	proc := newPreRunProcess(t, mockClient, selfDetection{})

	var gotOpts containerClient.ClientOptions

	proc.newClient = func(_ context.Context, _ *zerolog.Logger, opts containerClient.ClientOptions) (DockerClient, error) {
		gotOpts = opts

		return mockClient, nil
	}

	require.NoError(t, proc.preRun(newTestRootCommand(t,
		"--scope", "prod",
		"--host", "tcp://docker.example.com:2376",
		"--tlsverify",
		"--api-version", "1.47",
		"--cert-path", "/certs",
	), nil))

	assert.Equal(t, "prod", proc.cfg.Filter.Scope)
	assert.Equal(t, dockerConfig.Docker{
		Host:       "tcp://docker.example.com:2376",
		TLSVerify:  true,
		APIVersion: "1.47",
		CertPath:   "/certs",
	}, gotOpts.Docker)
	assert.Equal(t, "tcp://docker.example.com:2376", os.Getenv("DOCKER_HOST"))
	assert.Equal(t, "1", os.Getenv("DOCKER_TLS_VERIFY"))
	assert.Equal(t, "1.47", os.Getenv("DOCKER_API_VERSION"))
	assert.Equal(t, "/certs", os.Getenv("DOCKER_CERT_PATH"))
}

// TestPreRun_loadsSettingsOnce verifies that a run of the root command loads
// its settings once, in preRun, with the positional container names. A
// warning from loading the settings is then logged once, before notifications
// are set up, so no notification carries it. The logger:// service writes each
// notification to the same output, so a notified copy would appear there too.
func TestPreRun_loadsSettingsOnce(t *testing.T) {
	mockClient := mockCmd.NewMockDockerClient(t)
	mockClient.EXPECT().GetVersion().Return("1.47")
	mockClient.EXPECT().ListContainers(mock.Anything, withFilter).Return(nil, nil)

	proc := newPreRunProcess(t, mockClient, selfDetection{})

	// Outside a container, so the run has no restart policy to disable.
	proc.pid = func() int { return 4242 }

	readStderr := captureStderr(t)

	require.NoError(t, newExecuteCommand(t, proc,
		"--run-once", "--monitor-only", "--no-pull", "--notification-url", "logger://", "web",
	).Execute())

	// Closing the notifier sends what it still holds.
	proc.notifier.Close()

	assert.Equal(t, []string{"web"}, proc.cfg.Filter.Names)
	assert.Equal(t, 1, strings.Count(readStderr(), "Combining monitor-only and no-pull might result in no updates"))
}

// TestPreRun_scopeLabel verifies that a run without an explicit scope takes
// the scope from Watchtower's own scope label and builds its filter for that
// scope. A replacement from a self-update keeps the scope of the instance it
// replaced this way.
func TestPreRun_scopeLabel(t *testing.T) {
	self := selfContainer("/watchtower", "")
	self.SetLabel(scopeLabelKey, "prod")

	mockClient := mockCmd.NewMockDockerClient(t)
	mockClient.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, selfID).Return(self, nil)
	mockClient.EXPECT().GetVersion().Return("1.47").Maybe()
	mockClient.EXPECT().ListContainers(mock.Anything, withFilter).Return(nil, nil).Maybe()
	mockClient.EXPECT().ListLabeledContainers(mock.Anything, mock.Anything).Return(nil, nil).Maybe()
	mockClient.EXPECT().SetRestartPolicy(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	proc := newPreRunProcess(t, mockClient, selfDetection{mountinfo: mountinfoWithSelf()})

	require.NoError(t, newExecuteCommand(t, proc, "--run-once", "--log-level", "error").Execute())

	assert.Equal(t, "prod", proc.cfg.Filter.Scope)
	assert.Equal(t, "Only checking containers in scope prod", proc.cfg.Filter.Desc)

	inScope := runFixtureContainer("a1a1", "/app", "2026-10-01T12:00:00Z", map[string]string{scopeLabelKey: "prod"})
	unscoped := runFixtureContainer("b2b2", "/other", "2026-10-01T12:00:00Z", nil)

	assert.True(t, proc.cfg.Filter.Predicate(inScope), "a container in the label's scope is selected")
	assert.False(t, proc.cfg.Filter.Predicate(unscoped), "a container without a scope is not selected")
}

// TestPreRun_RegistryTLSSettings verifies that the container client and the
// update parameters receive registry TLS settings that follow the
// registry-tls-skip and registry-tls-min-version configuration options,
// whether set by flag or by environment variable, with a flag taking
// precedence over its environment variable.
func TestPreRun_RegistryTLSSettings(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		env            map[string]string
		wantSkip       bool
		wantMinVersion string
	}{
		{
			// The minimum version flag defaults to TLS1.2, the same minimum the
			// registry client applies when the setting is empty.
			name:           "neither set",
			wantMinVersion: "TLS1.2",
		},
		{
			name:           "flags",
			args:           []string{"--registry-tls-skip", "--registry-tls-min-version", "TLS1.3"},
			wantSkip:       true,
			wantMinVersion: "TLS1.3",
		},
		{
			name: "environment variables",
			env: map[string]string{
				"WATCHTOWER_REGISTRY_TLS_SKIP":        "true",
				"WATCHTOWER_REGISTRY_TLS_MIN_VERSION": "TLS1.3",
			},
			wantSkip:       true,
			wantMinVersion: "TLS1.3",
		},
		{
			name: "flags override environment variables",
			args: []string{"--registry-tls-skip=false", "--registry-tls-min-version", "TLS1.3"},
			env: map[string]string{
				"WATCHTOWER_REGISTRY_TLS_SKIP":        "true",
				"WATCHTOWER_REGISTRY_TLS_MIN_VERSION": "TLS1.2",
			},
			wantSkip:       false,
			wantMinVersion: "TLS1.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := mockCmd.NewMockDockerClient(t)
			proc := newPreRunProcess(t, mockClient, selfDetection{})

			var clientOptions containerClient.ClientOptions

			proc.newClient = func(_ context.Context, _ *zerolog.Logger, opts containerClient.ClientOptions) (DockerClient, error) {
				clientOptions = opts

				return mockClient, nil
			}

			cmd := newTestRootCommand(t, tt.args...)

			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			require.NoError(t, proc.preRun(cmd, nil))

			assert.Equal(t, tt.wantSkip, clientOptions.Registry.TLSSkip)
			assert.Equal(t, tt.wantMinVersion, clientOptions.Registry.TLSMinVersion)
			assert.Equal(t, clientOptions.Registry, proc.cfg.UpdateParams(settings.RunOverrides{}).Registry)
		})
	}
}

// TestPreRun_PorcelainKeepsExplicitSettings verifies that the porcelain
// configuration option only fills in the notification template, report, and
// log-stdout settings that were not set, and treats a setting from an
// environment variable the same as one from a flag.
func TestPreRun_PorcelainKeepsExplicitSettings(t *testing.T) {
	const custom = "{{range .}}{{.Message}}{{end}}"

	tests := []struct {
		name          string
		args          []string
		env           map[string]string
		wantTemplate  string
		wantReport    bool
		wantLogStdout bool
	}{
		{
			name:          "porcelain alone",
			env:           map[string]string{"WATCHTOWER_PORCELAIN": "v1"},
			wantTemplate:  "porcelain.v1.summary-no-log",
			wantReport:    true,
			wantLogStdout: true,
		},
		{
			name:          "settings from flags",
			args:          []string{"--porcelain", "v1", "--notification-template", custom, "--notification-report=false", "--notification-log-stdout=false"},
			wantTemplate:  custom,
			wantReport:    false,
			wantLogStdout: false,
		},
		{
			name: "settings from environment variables",
			env: map[string]string{
				"WATCHTOWER_PORCELAIN":               "v1",
				"WATCHTOWER_NOTIFICATION_TEMPLATE":   custom,
				"WATCHTOWER_NOTIFICATION_REPORT":     "false",
				"WATCHTOWER_NOTIFICATION_LOG_STDOUT": "false",
			},
			wantTemplate:  custom,
			wantReport:    false,
			wantLogStdout: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proc := newPreRunProcess(t, mockCmd.NewMockDockerClient(t), selfDetection{})

			cmd := newTestRootCommand(t, tt.args...)

			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			require.NoError(t, proc.preRun(cmd, nil))

			assert.Equal(t, tt.wantTemplate, proc.cfg.Notify.Template)
			assert.Equal(t, tt.wantReport, proc.cfg.Notify.Report)
			assert.Equal(t, tt.wantLogStdout, proc.cfg.Notify.LogStdout)
		})
	}
}

// TestPreRun_Exit verifies how preRun ends the process when it cannot or
// must not continue.
func TestPreRun_Exit(t *testing.T) {
	if helperCase := os.Getenv(preRunExitHelperEnv); helperCase != "" {
		preRunExitHelper(t, helperCase)

		// Every helper case must end the process. Reaching here means it did not.
		os.Exit(2)
	}

	tests := []struct {
		helperCase   string
		wantExitCode int
		// wantOutput lists lines the output must contain, in this order.
		wantOutput []string
		// unwantedOutput lists text the output must not contain.
		unwantedOutput []string
		// wantOnce lists text the output must contain exactly once.
		wantOnce []string
	}{
		{
			// A startup failure happens again on every start, so the main
			// process disables its restarts before it exits. The Docker client
			// is created for that when startup has not created it yet.
			helperCase:   "invalid-environment",
			wantExitCode: 1,
			wantOutput: []string{
				"SetRestartPolicy cccccccccccc no",
				"apply environment configuration:",
				"Failed to load configuration",
			},
		},
		{
			helperCase:   "invalid-log-format",
			wantExitCode: 1,
			wantOutput: []string{
				"SetRestartPolicy cccccccccccc no",
				"set up logging:",
				"Failed to load configuration",
			},
		},
		{
			// Because the notification settings are part of the configuration
			// that did not load, the failure sends no notification.
			helperCase:     "invalid-configuration",
			wantExitCode:   1,
			wantOutput:     []string{"SetRestartPolicy cccccccccccc no", "Failed to load configuration"},
			unwantedOutput: []string{"Shoutrrr:"},
		},
		{
			// The client connects to the daemon that the host flag names. The
			// process is exiting, so it waits only briefly for the daemon.
			helperCase:   "invalid-log-format-with-host-flag",
			wantExitCode: 1,
			wantOutput: []string{
				"newClient host=tcp://socket-proxy:2375 bounded=true",
				"SetRestartPolicy cccccccccccc no",
				"Failed to load configuration",
			},
		},
		{
			// TLS verification without a certificate path would connect in
			// plain HTTP, so neither startup nor the exit path connects.
			helperCase:   "tls-verify-without-cert-path",
			wantExitCode: 1,
			wantOutput: []string{
				logging.RestartsKept,
				"Failed to load configuration",
				"tlsverify requires cert-path for a remote Docker host",
			},
			unwantedOutput: []string{"newClient", "SetRestartPolicy"},
		},
		{
			// Without a Docker client, the restart policy cannot be changed, so
			// Watchtower reports that before the startup failure. Startup
			// already waited for the daemon, so the exit does not connect
			// again. It sends no notification, because notifications are not
			// set up yet and the failure would repeat on every restart.
			helperCase:   "docker-client-error",
			wantExitCode: 1,
			wantOutput: []string{
				logging.RestartsKept,
				"Failed to initialize Docker client",
			},
			unwantedOutput: []string{"SetRestartPolicy", "Shoutrrr:"},
			wantOnce:       []string{"newClient"},
		},
		{
			// Without its own container ID, Watchtower cannot change its
			// restart policy, so it reports that before the startup failure.
			helperCase:   "invalid-log-format-without-container-id",
			wantExitCode: 1,
			wantOutput: []string{
				logging.RestartsKept,
				"Failed to load configuration",
			},
			unwantedOutput: []string{"SetRestartPolicy"},
		},
		{
			// A process other than the main one, such as a health check probe,
			// leaves the container's restart policy alone.
			helperCase:     "invalid-configuration-outside-main-process",
			wantExitCode:   1,
			wantOutput:     []string{"Failed to load configuration"},
			unwantedOutput: []string{"newClient", "SetRestartPolicy"},
		},
		{
			// A flag that does not parse fails on every start too.
			helperCase:   "unknown-flag",
			wantExitCode: 1,
			wantOutput:   []string{"SetRestartPolicy cccccccccccc no", "Failed to execute root command"},
		},
		{
			// The flags before the one that does not parse are read.
			helperCase:   "host-flag-before-unknown-flag",
			wantExitCode: 1,
			wantOutput: []string{
				"newClient host=tcp://socket-proxy:2375",
				"SetRestartPolicy cccccccccccc no",
				"Failed to execute root command",
			},
		},
		{
			// A subcommand such as notify-upgrade leaves the restart policy alone.
			helperCase:     "subcommand-error",
			wantExitCode:   1,
			wantOutput:     []string{"Failed to execute root command"},
			unwantedOutput: []string{"newClient", "SetRestartPolicy", logging.RestartsKept},
		},
		{
			// A stop signal during the wait for the Docker daemon ends startup
			// as a stop ends a run, with code 0 and no failure reported. The
			// runtime stopped Watchtower on purpose, so its restarts are kept.
			helperCase:     "stopped-while-waiting-for-docker",
			wantExitCode:   0,
			wantOutput:     []string{"newClient host="},
			unwantedOutput: []string{"SetRestartPolicy", "Failed to initialize Docker client", logging.RestartsKept},
		},
		{
			// A failure that does not come from the stop is reported, even when
			// a stop signal arrives at the same time.
			helperCase:     "unrelated-failure-during-stop",
			wantExitCode:   1,
			wantOutput:     []string{"Failed to initialize Docker client"},
			unwantedOutput: []string{"SetRestartPolicy"},
		},
		{
			// An inspect failure the stop did not cause is still reported,
			// although restarts are kept while the process stops.
			helperCase:     "independent-inspect-failure-during-stop",
			wantExitCode:   1,
			wantOutput:     []string{logging.ScopeLabelUnreadable},
			unwantedOutput: []string{"SetRestartPolicy"},
		},
		{
			// A stop during the inspect of Watchtower's own container ends
			// startup before the failed inspect is reported, so no scope
			// label failure is logged.
			helperCase:   "stopped-while-inspecting-own-container",
			wantExitCode: 0,
			unwantedOutput: []string{
				"SetRestartPolicy",
				logging.ScopeLabelUnreadable,
				logging.SelfUninspected,
				logging.RestartsKept,
			},
		},
		{
			// The orchestrator reports its own failures. Its container is
			// removed when it exits, so its restart policy is left alone.
			helperCase:     "orchestrator-without-settings",
			wantExitCode:   1,
			wantOnce:       []string{"Failed to read orchestrator environment variables"},
			unwantedOutput: []string{"SetRestartPolicy", "Failed to execute root command", logging.RestartsKept},
		},
		{
			// A completed handoff ends the orchestrator before the rest of
			// startup runs.
			helperCase:     "orchestrator-handoff-completes",
			wantExitCode:   0,
			unwantedOutput: []string{"SetRestartPolicy", "Orchestration failed"},
		},
		{
			helperCase:   "invalid-restart-of-old-instance",
			wantExitCode: 0,
			wantOutput:   []string{"SetRestartPolicy cccccccccccc no"},
		},
		{
			// A run-once instance in an old container retires before its run.
			helperCase:   "invalid-restart-of-old-instance-in-run-once",
			wantExitCode: 0,
			wantOutput:   []string{"SetRestartPolicy cccccccccccc no"},
		},
		{
			// A failed policy update is logged at debug only. An old container
			// that the runtime starts again exits as old once more, until the
			// instance that replaced it removes it.
			helperCase:     "invalid-restart-of-old-instance-restarts-kept",
			wantExitCode:   0,
			wantOutput:     []string{"SetRestartPolicy cccccccccccc no"},
			unwantedOutput: []string{logging.RestartsKept, errRunTestDocker.Error()},
		},
		{
			// An old instance that starts its orphaned replacement sends the
			// recovery in one notification before it exits.
			helperCase:   "orphan-recovery-by-old-instance",
			wantExitCode: 0,
			wantOutput: []string{
				"SetRestartPolicy cccccccccccc no",
				"Recovered orphaned Watchtower container: watchtower (0f0f0f0f0f0f)",
			},
			wantOnce: []string{"Recovered orphaned Watchtower container: watchtower (0f0f0f0f0f0f)"},
		},
		{
			// An old instance still starts its orphaned replacement and exits
			// when Shoutrrr rejects a notification URL.
			helperCase:     "orphan-recovery-with-rejected-notification-url",
			wantExitCode:   0,
			wantOutput:     []string{logging.OrphanRecovered, "SetRestartPolicy cccccccccccc no"},
			unwantedOutput: []string{"Failed to initialize notifications"},
		},
		{
			// A notification URL that Shoutrrr rejects fails on every start,
			// so the main process disables its restarts before it exits.
			helperCase:   "rejected-notification-url",
			wantExitCode: 1,
			wantOutput: []string{
				"SetRestartPolicy cccccccccccc no",
				"Failed to initialize notifications",
				"notification URL 1: discord:",
			},
			unwantedOutput: []string{"secrettoken"},
		},
		{
			// Without an explicit scope, the scope label is unknown. preRun then
			// disables the restart policy and exits rather than running without
			// a scope.
			helperCase:   "unreadable-scope-label",
			wantExitCode: 1,
			wantOutput: []string{
				"SetRestartPolicy cccccccccccc no",
				"Exiting rather than running without a scope",
			},
		},
		{
			// When the restart policy cannot be disabled, preRun still exits, and
			// it reports that the runtime may start Watchtower again.
			helperCase:   "unreadable-scope-label-restarts-kept",
			wantExitCode: 1,
			wantOutput: []string{
				"SetRestartPolicy cccccccccccc no",
				logging.RestartsKept,
				"Exiting rather than running without a scope",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.helperCase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPreRun_Exit$", "-test.v=false")

			cmd.Env = append(os.Environ(), preRunExitHelperEnv+"="+tt.helperCase)

			out, err := cmd.CombinedOutput()

			exitCode := 0

			var exitErr *exec.ExitError

			switch {
			case errors.As(err, &exitErr):
				exitCode = exitErr.ExitCode()
			default:
				require.NoError(t, err, "output:\n%s", out)
			}

			assert.Equal(t, tt.wantExitCode, exitCode, "output:\n%s", out)

			// The lines must appear in order, so each search starts after the
			// previous match.
			rest := string(out)

			for _, want := range tt.wantOutput {
				var found bool

				_, rest, found = strings.Cut(rest, want)
				require.True(t, found, "missing %q after the earlier lines in output:\n%s", want, out)
			}

			for _, unwanted := range tt.unwantedOutput {
				assert.NotContains(t, string(out), unwanted, "output:\n%s", out)
			}

			for _, once := range tt.wantOnce {
				assert.Equal(t, 1, strings.Count(string(out), once), "count of %q in output:\n%s", once, out)
			}

			assert.NotContains(t, string(out), "PASS", "the helper must end the process")
		})
	}
}

// preRunExitHelper runs the named exit case in the re-executed test binary.
// The Docker client prints each restart policy change so the parent can
// check it.
func preRunExitHelper(t *testing.T, helperCase string) {
	t.Helper()

	var (
		args      []string
		env       map[string]string
		policyErr error
		clientErr error
		// execute runs the whole command tree, as Execute does, instead of
		// preRun alone.
		execute bool
	)

	// processCtx is the process context. stopProcess cancels it, as a stop
	// signal does.
	processCtx, stopProcess := context.WithCancel(t.Context())
	defer stopProcess()

	// Watchtower runs as PID 1, the main process of its own container, unless
	// a case says otherwise.
	detection := selfDetection{mountinfo: mountinfoWithSelf(), marker: true}
	pid := 1

	mockClient := mockCmd.NewMockDockerClient(t)
	mockClient.EXPECT().SetRestartPolicy(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, id container.ContainerID, policy dockerContainer.RestartPolicy) error {
			fmt.Fprintf(os.Stderr, "SetRestartPolicy %s %s\n", containerLabel(id), policy.Name)

			return policyErr
		}).Maybe()

	switch helperCase {
	case "invalid-environment":
		env = map[string]string{"WATCHTOWER_HTTP_API_RATE_LIMIT": "many"}
	case "invalid-log-format":
		args = []string{"--log-format", "bogus"}
	case "invalid-configuration":
		args = []string{"--rolling-restart", "--monitor-only", "--notification-url", "logger://"}
	case "invalid-log-format-without-container-id":
		args = []string{"--log-format", "bogus"}
		detection = selfDetection{}
	case "tls-verify-without-cert-path":
		args = []string{"--host", "tcp://socket-proxy:2376", "--tlsverify"}
	case "invalid-log-format-with-host-flag":
		args = []string{"--host", "tcp://socket-proxy:2375", "--log-format", "bogus"}
	case "invalid-configuration-outside-main-process":
		args = []string{"--rolling-restart", "--monitor-only"}
		pid = 42
	case "docker-client-error":
		args = []string{"--notification-url", "logger://"}
		clientErr = errRunTestDocker
	case "unknown-flag":
		args = []string{"--not-a-watchtower-flag"}
		execute = true
	case "host-flag-before-unknown-flag":
		args = []string{"--host", "tcp://socket-proxy:2375", "--not-a-watchtower-flag"}
		execute = true
	case "subcommand-error":
		args = []string{"notify-upgrade", "--not-a-watchtower-flag"}
		execute = true
	case "stopped-while-waiting-for-docker":
		stopProcess()
	case "unrelated-failure-during-stop":
		stopProcess()

		clientErr = errRunTestDocker
	case "independent-inspect-failure-during-stop":
		detection = selfDetection{mountinfo: mountinfoWithSelf()}

		mockClient.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, selfID).
			RunAndReturn(func(context.Context, container.ContainerID) (*container.Container, error) {
				stopProcess()

				return nil, errRunTestDocker
			}).Once()
	case "stopped-while-inspecting-own-container":
		detection = selfDetection{mountinfo: mountinfoWithSelf()}

		mockClient.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, selfID).
			RunAndReturn(func(ctx context.Context, _ container.ContainerID) (*container.Container, error) {
				stopProcess()

				return nil, ctx.Err()
			}).Once()
	case "orchestrator-without-settings":
		args = []string{"--self-update-orchestrator"}
		detection = selfDetection{}
	case "orchestrator-handoff-completes":
		args = []string{"--self-update-orchestrator"}
		env = map[string]string{
			"WT_ORCHESTRATOR_OLD_ID":        string(selfID),
			"WT_ORCHESTRATOR_NEW_IMAGE":     "nickfedor/watchtower:latest",
			"WT_ORCHESTRATOR_ORIGINAL_NAME": "watchtower",
		}

		old := selfContainer("/watchtower", "")
		replacementID := container.ContainerID(strings.Repeat("0e", 32))
		replacement := container.NewContainer(nil,
			&dockerContainer.InspectResponse{
				ID:     string(replacementID),
				Name:   "/watchtower",
				State:  &dockerContainer.State{Running: true},
				Config: &dockerContainer.Config{Labels: map[string]string{watchtowerLabelKey: "true"}},
			},
			nil,
		)

		mockClient.EXPECT().GetContainer(mock.Anything, selfID).Return(old, nil).Once()
		mockClient.EXPECT().StopContainer(mock.Anything, old, mock.Anything).Return(nil).Once()
		mockClient.EXPECT().RenameContainer(mock.Anything, old, mock.Anything).Return(nil).Once()
		mockClient.EXPECT().StartContainer(mock.Anything, old).Return(replacementID, nil).Once()
		mockClient.EXPECT().GetContainer(mock.Anything, replacementID).Return(replacement, nil).Once()
		mockClient.EXPECT().RemoveContainer(mock.Anything, old).Return(nil).Once()
	case "invalid-restart-of-old-instance", "invalid-restart-of-old-instance-in-run-once",
		"invalid-restart-of-old-instance-restarts-kept":
		old := selfContainer("/watchtower-old-cccccccccccc", "")
		detection = selfDetection{mountinfo: mountinfoWithSelf()}

		switch helperCase {
		case "invalid-restart-of-old-instance-in-run-once":
			args = []string{"--run-once"}
		case "invalid-restart-of-old-instance-restarts-kept":
			policyErr = errRunTestDocker
		default:
		}

		mockClient.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, selfID).Return(old, nil)
		mockClient.EXPECT().ListLabeledContainers(mock.Anything, watchtowerLabelKey).Return([]*container.Container{old}, nil).Once()
	case "orphan-recovery-by-old-instance", "orphan-recovery-with-rejected-notification-url":
		args = []string{"--log-level", "info", "--notification-url", "logger://"}
		if helperCase == "orphan-recovery-with-rejected-notification-url" {
			args = []string{"--log-level", "info", "--notification-url", "discord://secrettoken@notanumber"}
		}

		old := selfContainer("/watchtower-old-cccccccccccc", "")
		orphanID := container.ContainerID(strings.Repeat("0f", 32))
		orphan := container.NewContainer(nil,
			&dockerContainer.InspectResponse{
				ID:     string(orphanID),
				Name:   "/watchtower",
				State:  &dockerContainer.State{Status: dockerContainer.StateCreated},
				Config: &dockerContainer.Config{Labels: map[string]string{watchtowerLabelKey: "true"}},
			},
			nil,
		)
		detection = selfDetection{mountinfo: mountinfoWithSelf()}

		mockClient.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, selfID).Return(old, nil)
		mockClient.EXPECT().ListLabeledContainers(mock.Anything, watchtowerLabelKey).Return([]*container.Container{old, orphan}, nil)
		mockClient.EXPECT().StartContainerByID(mock.Anything, orphanID).Return(nil).Once()
	case "rejected-notification-url":
		args = []string{"--notification-url", "discord://secrettoken@notanumber"}
		detection = selfDetection{mountinfo: mountinfoWithSelf()}

		mockClient.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, selfID).Return(selfContainer("/watchtower", ""), nil)
	case "unreadable-scope-label", "unreadable-scope-label-restarts-kept":
		detection = selfDetection{mountinfo: mountinfoWithSelf()}

		if helperCase == "unreadable-scope-label-restarts-kept" {
			policyErr = errRunTestDocker
		}

		mockClient.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, selfID).Return(nil, errRunTestDocker)
	default:
		panic("unknown preRun exit helper case " + helperCase)
	}

	proc := newPreRunProcess(t, mockClient, detection)

	proc.pid = func() int { return pid }

	// The client factory prints the Docker host it connects to and whether its
	// wait for the daemon has a deadline. It fails with clientErr when a case
	// sets it, and with the context's error when the context has ended, as the
	// wait for the daemon does.
	proc.newClient = func(ctx context.Context, _ *zerolog.Logger, opts containerClient.ClientOptions) (DockerClient, error) {
		_, bounded := ctx.Deadline()
		fmt.Fprintf(os.Stderr, "newClient host=%s bounded=%t\n", opts.Docker.Host, bounded)

		if clientErr != nil {
			return nil, clientErr
		}

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		return mockClient, nil
	}

	for _, key := range []string{
		ephemeral.OrchestratorOldIDEnv,
		ephemeral.OrchestratorNewImageEnv,
		ephemeral.OrchestratorOriginalNameEnv,
		ephemeral.OrchestratorContainerChainEnv,
		ephemeral.OrchestratorCleanupEnv,
		ephemeral.OrchestratorNetworkContainerEnv,
	} {
		t.Setenv(key, "")
	}

	proc.log = logging.New(os.Stderr, logging.InfoLevel)

	if execute {
		executed, err := newExecuteCommand(t, proc, args...).ExecuteContextC(processCtx)
		proc.exit(processCtx, executed, err)

		return
	}

	// The test command clears the flag environment, so env is set after it.
	cmd := newTestRootCommand(t, args...)

	for key, value := range env {
		t.Setenv(key, value)
	}

	cmd.SetContext(processCtx)

	proc.exit(processCtx, cmd, proc.preRun(cmd, nil))
}
