package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"

	"github.com/nicholas-fedor/watchtower/internal/logging"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// preRunExitHelperEnv selects a preRun exit case when the test binary is
// re-executed as a subprocess. Empty means the parent process.
const preRunExitHelperEnv = "WATCHTOWER_CMD_PRE_RUN_EXIT_HELPER"

// selfID is the container ID the preRun tests detect for Watchtower itself.
var selfID = types.ContainerID(strings.Repeat("c", 64))

// selfDetection is what the self container ID detection can read: the
// mountinfo and cgroup files, and the HOSTNAME variable. Empty means absent.
type selfDetection struct {
	mountinfo string
	cgroup    string
	hostname  string
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

// setPreRunState prepares the process state preRun reads and writes, and
// restores it when the test ends. newClient returns client, self container ID
// detection reads only detection, and the Docker variables preRun sets are
// restored.
func setPreRunState(t *testing.T, client container.Client, detection selfDetection) {
	t.Helper()

	setRunState(t, runTestState{})

	savedNewClient := newClient
	savedMountinfo := container.ReadMountinfoFunc
	savedCgroup := container.ReadCgroupFunc

	t.Cleanup(func() {
		newClient = savedNewClient
		container.ReadMountinfoFunc = savedMountinfo
		container.ReadCgroupFunc = savedCgroup
	})

	newClient = func(*zerolog.Logger, container.ClientOptions) container.Client { return client }
	container.ReadMountinfoFunc = readFixture(detection.mountinfo)
	container.ReadCgroupFunc = readFixture(detection.cgroup)

	for _, key := range []string{"HOSTNAME", "DOCKER_HOST", "DOCKER_TLS_VERIFY", "DOCKER_API_VERSION", "DOCKER_CERT_PATH"} {
		t.Setenv(key, "")
		require.NoError(t, os.Unsetenv(key))
	}

	if detection.hostname != "" {
		t.Setenv("HOSTNAME", detection.hostname)
	}

	// preRun replaces the notifier with one hooked to its logger.
	t.Cleanup(func() { notifier.Close() })
}

// readFixture returns a file reader that returns content, or a not-found
// error when content is empty.
func readFixture(content string) func(string) ([]byte, error) {
	return func(string) ([]byte, error) {
		if content == "" {
			return nil, os.ErrNotExist
		}

		return []byte(content), nil
	}
}

// selfContainer returns the Watchtower container preRun detects, with the
// given name and container hostname.
func selfContainer(name, hostname string) types.Container {
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
			HostConfig: &dockerContainer.HostConfig{},
		},
		&dockerImage.InspectResponse{ID: "sha256:" + string(selfID)},
	)
}

// TestPreRun_SelfDetection records how preRun finds the Watchtower container
// it runs in, and what it keeps when detection or the lookup fails. A process
// whose detection fails looks the same as one outside a container.
func TestPreRun_SelfDetection(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		detection selfDetection
		setup     func(client *mockContainer.MockClient)
		wantID    types.ContainerID
		wantName  string
		wantCalls []string
	}{
		{
			name:      "outside a container",
			wantCalls: []string{},
		},
		{
			name:      "found from mountinfo",
			detection: selfDetection{mountinfo: mountinfoWithSelf()},
			setup: func(client *mockContainer.MockClient) {
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
			setup: func(client *mockContainer.MockClient) {
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
			setup: func(client *mockContainer.MockClient) {
				self := selfContainer("/watchtower", "wt-host")
				client.EXPECT().ListContainers(mock.Anything).Return([]types.Container{self}, nil)
				client.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, selfID).Return(self, nil)
			},
			wantID:    selfID,
			wantName:  "watchtower",
			wantCalls: []string{"ListContainers all", "GetCurrentWatchtowerContainer cccccccccccc"},
		},
		{
			name:      "lookup fails and the ID is kept",
			detection: selfDetection{mountinfo: mountinfoWithSelf()},
			setup: func(client *mockContainer.MockClient) {
				client.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, selfID).Return(nil, errRunTestDocker)
			},
			wantID:    selfID,
			wantCalls: []string{"GetCurrentWatchtowerContainer cccccccccccc"},
		},
		{
			name:      "lookup times out and the ID is cleared",
			detection: selfDetection{mountinfo: mountinfoWithSelf()},
			setup: func(client *mockContainer.MockClient) {
				client.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, selfID).
					Return(nil, fmt.Errorf("inspect: %w", context.DeadlineExceeded))
			},
			wantCalls: []string{"GetCurrentWatchtowerContainer cccccccccccc"},
		},
		{
			name:      "old instance continues in run-once mode",
			args:      []string{"--run-once"},
			detection: selfDetection{mountinfo: mountinfoWithSelf()},
			setup: func(client *mockContainer.MockClient) {
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
			mockClient := mockContainer.NewMockClient(t)
			if tt.setup != nil {
				tt.setup(mockClient)
			}

			setPreRunState(t, mockClient, tt.detection)

			fixtureNotifier := notifier

			newRunProcess().preRun(newTestRootCommand(t, tt.args...), nil)

			assert.Equal(t, tt.wantID, currentWatchtowerContainerID)
			assert.Equal(t, tt.wantName, containerName(currentWatchtowerContainer))
			assert.Equal(t, tt.wantCalls, runCalls(mockClient))
			assert.Same(t, mockClient, client, "preRun uses the client from newClient")
			assert.NotSame(t, fixtureNotifier, notifier, "preRun creates its own notifier")
		})
	}
}

// containerName names a container, or returns "" when it is nil.
func containerName(c types.Container) string {
	if c == nil {
		return ""
	}

	return strings.TrimPrefix(c.Name(), "/")
}

// TestPreRun_Configuration verifies that preRun loads the configuration from
// the flags and exports the Docker connection settings to the environment,
// where the Docker client reads them.
func TestPreRun_Configuration(t *testing.T) {
	setPreRunState(t, mockContainer.NewMockClient(t), selfDetection{})

	newRunProcess().preRun(newTestRootCommand(t,
		"--scope", "prod",
		"--host", "tcp://docker.example.com:2376",
		"--tlsverify",
		"--api-version", "1.47",
	), nil)

	assert.Equal(t, "prod", appCfg.Filter.Scope)
	// With TLS verification enabled, a tcp:// host is exported as https://.
	assert.Equal(t, "https://docker.example.com:2376", os.Getenv("DOCKER_HOST"))
	assert.Equal(t, "1", os.Getenv("DOCKER_TLS_VERIFY"))
	assert.Equal(t, "1.47", os.Getenv("DOCKER_API_VERSION"))
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
		wantOutput   string
	}{
		{helperCase: "invalid-log-format", wantExitCode: 1, wantOutput: "Failed to initialize logging"},
		{helperCase: "invalid-configuration", wantExitCode: 1, wantOutput: "Failed to load configuration"},
		{
			// RunOrchestrator always ends the process, so the restart policy
			// fallback after it in preRun does not run.
			helperCase:   "orchestrator-without-settings",
			wantExitCode: 1,
			wantOutput:   "Failed to read orchestrator environment variables",
		},
		{
			helperCase:   "invalid-restart-of-old-instance",
			wantExitCode: 0,
			wantOutput:   "SetRestartPolicy watchtower-old-cccccccccccc no",
		},
		{
			helperCase:   "invalid-restart-of-chain-parent",
			wantExitCode: 0,
			wantOutput:   "SetRestartPolicy watchtower no",
		},
	}

	for _, tt := range tests {
		t.Run(tt.helperCase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			// Codacy: static argv only (os.Args[0] is this test binary).
			// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
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
			assert.Contains(t, string(out), tt.wantOutput)
			assert.NotContains(t, string(out), "PASS", "the helper must end the process")
		})
	}
}

// preRunExitHelper runs the named exit case in the re-executed test binary.
// The Docker client prints each restart policy change so the parent can
// check it.
func preRunExitHelper(t *testing.T, helperCase string) {
	t.Helper()

	mockClient := mockContainer.NewMockClient(t)
	mockClient.EXPECT().SetRestartPolicy(mock.Anything, mock.Anything, mock.Anything).
		Run(func(_ context.Context, c types.Container, policy dockerContainer.RestartPolicy) {
			fmt.Fprintf(os.Stderr, "SetRestartPolicy %s %s\n", containerLabel(c), policy.Name)
		}).Maybe()

	var (
		args      []string
		detection selfDetection
	)

	switch helperCase {
	case "invalid-log-format":
		args = []string{"--log-format", "bogus"}
	case "invalid-configuration":
		args = []string{"--rolling-restart", "--monitor-only"}
	case "orchestrator-without-settings":
		args = []string{"--self-update-orchestrator"}
	case "invalid-restart-of-old-instance":
		old := selfContainer("/watchtower-old-cccccccccccc", "")
		detection = selfDetection{mountinfo: mountinfoWithSelf()}

		mockClient.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, selfID).Return(old, nil)
		mockClient.EXPECT().ListContainers(mock.Anything, withFilter).Return([]types.Container{old}, nil).Maybe()
	case "invalid-restart-of-chain-parent":
		// The container's chain lists its own ID, which marks it as the parent
		// of a newer instance.
		parent := selfContainer("/watchtower", "")
		parent.ContainerInfo().Config.Labels[container.ContainerChainLabel] = strings.Repeat("a", 64) + "," + string(selfID)
		detection = selfDetection{mountinfo: mountinfoWithSelf()}

		mockClient.EXPECT().GetCurrentWatchtowerContainer(mock.Anything, selfID).Return(parent, nil)
		mockClient.EXPECT().ListContainers(mock.Anything, withFilter).Return([]types.Container{parent}, nil).Maybe()
	default:
		panic("unknown preRun exit helper case " + helperCase)
	}

	setPreRunState(t, mockClient, detection)

	for _, key := range []string{"WT_ORCHESTRATOR_OLD_ID", "WT_ORCHESTRATOR_NEW_IMAGE", "WT_ORCHESTRATOR_ORIGINAL_NAME"} {
		t.Setenv(key, "")
	}

	proc := &process{log: logging.New(os.Stderr, logging.InfoLevel)}
	proc.preRun(newTestRootCommand(t, args...), nil)
}
