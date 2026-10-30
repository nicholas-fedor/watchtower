package update

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container"
	mockContainerClient "github.com/nicholas-fedor/watchtower/internal/container/client/mocks"
	containerImage "github.com/nicholas-fedor/watchtower/internal/container/image"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/container/oci"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// skipTestContainer returns a running container with the given labels.
func skipTestContainer(id, name string, labels map[string]string) types.Container {
	return mockUpdate.CreateMockContainerWithConfig(
		id,
		"/"+name,
		"org/"+name+":latest",
		true,
		false,
		time.Now(),
		&dockerContainer.Config{Image: "org/" + name + ":latest", Labels: labels},
	)
}

// reportErrors returns the error of each report, by container name.
func reportErrors(reports []types.ContainerReport) map[string]string {
	errs := make(map[string]string, len(reports))
	for _, r := range reports {
		errs[r.Name()] = r.Error()
	}

	return errs
}

// TestUpdate_PreUpdateExitCode75IsSkipped verifies that a container whose
// pre-update hook exits with code 75 is reported as skipped, with the hook's
// reason, and is neither stopped nor recreated, in both restart modes.
func TestUpdate_PreUpdateExitCode75IsSkipped(t *testing.T) {
	t.Parallel()

	for _, rolling := range []bool{false, true} {
		t.Run(fmt.Sprintf("rolling=%t", rolling), func(t *testing.T) {
			t.Parallel()

			app := skipTestContainer("a000000000000000000000000000000000000000000000000000000000000000", "app",
				map[string]string{"com.centurylinklabs.watchtower.lifecycle.pre-update": "/pre-update.sh"})

			client := mockContainerClient.NewMockClient(t)
			// The post-check hooks list the containers again after the update.
			client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{app}, nil).Times(2)
			client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
				Return(true, types.ImageID("sha256:new"), "", nil)
			client.EXPECT().GetImageAnnotations(mock.Anything, mock.Anything).Return(oci.Annotations{}).Maybe()
			client.EXPECT().
				ExecuteCommand(mock.Anything, app, "/pre-update.sh", mock.Anything, mock.Anything, mock.Anything).
				Return(true, nil).Once()

			report, _, err := update(newTestLogger(t), t.Context(), client,
				types.UpdateParams{LifecycleHooks: true, RollingRestart: rolling}, ratelimit.New())
			require.NoError(t, err)

			assert.Equal(t, map[string]string{
				"app": "skipping container due to pre-update command exit code 75 (EX_TEMPFAIL)",
			}, reportErrors(report.Skipped()))
			assert.Empty(t, report.Failed())
			assert.Empty(t, report.Updated())
		})
	}
}

// TestUpdate_CanceledContainersAreSkipped verifies that the containers a
// canceled update never touched are reported as skipped, not failed, while a
// container already stopped is still recreated, in both restart modes.
func TestUpdate_CanceledContainersAreSkipped(t *testing.T) {
	t.Parallel()

	for _, rolling := range []bool{false, true} {
		t.Run(fmt.Sprintf("rolling=%t", rolling), func(t *testing.T) {
			t.Parallel()

			first := skipTestContainer("a000000000000000000000000000000000000000000000000000000000000000", "app-a", nil)
			second := skipTestContainer("b000000000000000000000000000000000000000000000000000000000000000", "app-b", nil)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			client := mockContainerClient.NewMockClient(t)
			client.EXPECT().ListContainers(mock.Anything, mock.Anything).
				Return([]types.Container{first, second}, nil).Once()
			client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
				Return(true, types.ImageID("sha256:new"), "", nil)
			client.EXPECT().GetImageAnnotations(mock.Anything, mock.Anything).Return(oci.Annotations{}).Maybe()

			// The update is canceled as soon as the first container is stopped.
			// update stops containers one at a time on the calling goroutine,
			// so stopped needs no lock.
			var stopped string

			client.EXPECT().StopAndRemoveContainer(mock.Anything, mock.Anything, mock.Anything).
				RunAndReturn(func(_ context.Context, c types.Container, _ time.Duration) error {
					stopped = c.Name()

					cancel()

					return nil
				}).Once()
			client.EXPECT().CreateContainer(mock.Anything, mock.Anything).
				Return(types.ContainerID("e000000000000000000000000000000000000000000000000000000000000000"), nil).Once()
			client.EXPECT().StartContainerByID(mock.Anything, mock.Anything).Return(nil).Once()
			client.EXPECT().GetContainer(mock.Anything, mock.Anything).Return(first, nil).Maybe()
			client.EXPECT().WaitForContainerHealthy(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

			report, _, _ := update(newTestLogger(t), ctx, client, types.UpdateParams{RollingRestart: rolling}, ratelimit.New())
			require.NotNil(t, report)

			untouched := first.Name()
			if stopped == first.Name() {
				untouched = second.Name()
			}

			assert.Equal(t, []string{stopped}, reportNames(report.Updated()), "the stopped container is recreated")
			assert.Equal(t, []string{untouched}, reportNames(report.Skipped()), "the untouched container is skipped")
			assert.Empty(t, report.Failed())
		})
	}
}

// TestUpdate_CanceledScanSkipsUncheckedContainers verifies that a scan canceled
// before its restart phase reports every container it did not check as
// skipped, with the cancellation as the reason, so its partial report does not
// read as a complete scan. A container recorded before the cancellation, such
// as one on a pinned image, keeps its result.
func TestUpdate_CanceledScanSkipsUncheckedContainers(t *testing.T) {
	t.Parallel()

	appA := skipTestContainer("a000000000000000000000000000000000000000000000000000000000000000", "app-a", nil)
	appB := skipTestContainer("b000000000000000000000000000000000000000000000000000000000000000", "app-b", nil)

	t.Run("canceled before the staleness checks", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// Canceling while the containers are listed ends the scan before it
		// checks any of them.
		client := mockContainerClient.NewMockClient(t)
		client.EXPECT().ListContainers(mock.Anything, mock.Anything).
			RunAndReturn(func(context.Context, ...types.Filter) ([]types.Container, error) {
				cancel()

				return []types.Container{appA, appB}, nil
			}).Once()

		report, _, err := update(newTestLogger(t), ctx, client, types.UpdateParams{}, ratelimit.New())
		require.ErrorIs(t, err, context.Canceled)
		require.NotNil(t, report)

		assert.Equal(t, map[string]string{
			"app-a": "update skipped: context canceled",
			"app-b": "update skipped: context canceled",
		}, reportErrors(report.Skipped()))
		assert.Empty(t, report.Scanned())
	})

	t.Run("canceled during the staleness checks", func(t *testing.T) {
		t.Parallel()

		pinnedImage := "org/pinned@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
		pinned := mockUpdate.CreateMockContainerWithConfig(
			"c000000000000000000000000000000000000000000000000000000000000000",
			"/pinned",
			pinnedImage,
			true,
			false,
			time.Now(),
			&dockerContainer.Config{Image: pinnedImage},
		)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		client := mockContainerClient.NewMockClient(t)
		client.EXPECT().ListContainers(mock.Anything, mock.Anything).
			Return([]types.Container{appA, appB, pinned}, nil).Once()

		// The first check cancels the scan. A check that starts after it
		// stops at the cancellation without calling Docker.
		client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(func(ctx context.Context, _ types.Container, _ types.UpdateParams) (bool, types.ImageID, string, error) {
				cancel()

				return false, "", "", ctx.Err()
			})

		report, _, err := update(newTestLogger(t), ctx, client, types.UpdateParams{}, ratelimit.New())
		require.ErrorIs(t, err, context.Canceled)
		require.NotNil(t, report)

		assert.Equal(t, []string{"pinned"}, reportNames(report.Scanned()), "the pinned container keeps its result")
		require.Equal(t, []string{"app-a", "app-b"}, reportNames(report.Skipped()))

		for _, skipped := range report.Skipped() {
			assert.True(t, strings.HasPrefix(skipped.Error(), "update skipped: "), skipped.Error())
			assert.Contains(t, skipped.Error(), "context canceled")
		}
	})
}

// TestUpdate_CheckTimeoutSkipsOnlyItsContainer verifies that a staleness check
// failing with a timeout of its own, such as a registry request reaching its
// client timeout while the scan is still running, skips only that container.
// The scan keeps checking the other containers and does not end in an error.
func TestUpdate_CheckTimeoutSkipsOnlyItsContainer(t *testing.T) {
	t.Parallel()

	slow := skipTestContainer("a000000000000000000000000000000000000000000000000000000000000000", "slow", nil)
	fresh := skipTestContainer("b000000000000000000000000000000000000000000000000000000000000000", "fresh", nil)

	client := mockContainerClient.NewMockClient(t)
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).
		Return([]types.Container{slow, fresh}, nil).Once()
	client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, c types.Container, _ types.UpdateParams) (bool, types.ImageID, string, error) {
			if c.Name() == slow.Name() {
				return false, "", "", fmt.Errorf("could not determine image age: %w", context.DeadlineExceeded)
			}

			return false, c.ImageID(), "", nil
		}).Times(2)

	report, _, err := update(newTestLogger(t), t.Context(), client, types.UpdateParams{}, ratelimit.New())
	require.NoError(t, err)

	assert.Equal(t, map[string]string{
		"slow": "could not determine image age: context deadline exceeded",
	}, reportErrors(report.Skipped()))
	assert.Equal(t, []string{"fresh"}, reportNames(report.Scanned()))
}

// TestUpdate_CanceledRecreateFailureIsFailed verifies that a container stopped
// before an update is canceled, whose recreation then fails, is reported as
// failed even though the failure wraps the cancellation, while the container
// the update never touched is reported as skipped.
func TestUpdate_CanceledRecreateFailureIsFailed(t *testing.T) {
	t.Parallel()

	first := skipTestContainer("a000000000000000000000000000000000000000000000000000000000000000", "app-a", nil)
	second := skipTestContainer("b000000000000000000000000000000000000000000000000000000000000000", "app-b", nil)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	client := mockContainerClient.NewMockClient(t)
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).
		Return([]types.Container{first, second}, nil).Once()
	client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
		Return(true, types.ImageID("sha256:new"), "", nil)
	client.EXPECT().GetImageAnnotations(mock.Anything, mock.Anything).Return(oci.Annotations{}).Maybe()

	// update stops containers one at a time on the calling goroutine, so
	// stopped needs no lock.
	var stopped string

	client.EXPECT().StopAndRemoveContainer(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, c types.Container, _ time.Duration) error {
			stopped = c.Name()

			cancel()

			return nil
		}).Once()
	client.EXPECT().CreateContainer(mock.Anything, mock.Anything).
		Return(types.ContainerID(""), fmt.Errorf("create container: %w", context.Canceled)).Once()

	report, _, _ := update(newTestLogger(t), ctx, client, types.UpdateParams{}, ratelimit.New())
	require.NotNil(t, report)

	untouched := first.Name()
	if stopped == first.Name() {
		untouched = second.Name()
	}

	failed := reportErrors(report.Failed())
	require.Contains(t, failed, stopped, "the container left stopped is a failure")
	assert.Len(t, failed, 1)
	assert.Equal(t, []string{untouched}, reportNames(report.Skipped()))
}

// TestUpdate_StopFailureStaysFailedWhenCanceled verifies that a container
// whose stop fails while the update is canceled stays reported as failed with
// the stop error. The restart phase skips it because it was never stopped, and
// that skip must not replace the failure.
func TestUpdate_StopFailureStaysFailedWhenCanceled(t *testing.T) {
	t.Parallel()

	app := skipTestContainer("a000000000000000000000000000000000000000000000000000000000000000", "app", nil)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	client := mockContainerClient.NewMockClient(t)
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{app}, nil).Once()
	client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
		Return(true, types.ImageID("sha256:new"), "", nil)
	client.EXPECT().GetImageAnnotations(mock.Anything, mock.Anything).Return(oci.Annotations{}).Maybe()
	client.EXPECT().StopAndRemoveContainer(mock.Anything, app, mock.Anything).
		RunAndReturn(func(context.Context, types.Container, time.Duration) error {
			cancel()

			return errors.New("daemon unavailable")
		}).Once()

	report, _, _ := update(newTestLogger(t), ctx, client, types.UpdateParams{}, ratelimit.New())
	require.NotNil(t, report)

	assert.Empty(t, report.Skipped())
	assert.Equal(t, map[string]string{"app": "failed to stop container: daemon unavailable"},
		reportErrors(report.Failed()))
}

// TestUpdate_ContainerGoneBeforeStopIsSkipped verifies that a container which
// no longer exists when the update stops it, for example one removed or
// replaced after the scan, is reported as skipped and is not recreated from
// the scan's inspect data, in both restart modes.
func TestUpdate_ContainerGoneBeforeStopIsSkipped(t *testing.T) {
	t.Parallel()

	for _, rolling := range []bool{false, true} {
		t.Run(fmt.Sprintf("rolling=%t", rolling), func(t *testing.T) {
			t.Parallel()

			app := skipTestContainer("a000000000000000000000000000000000000000000000000000000000000000", "app", nil)

			// The mock fails the test on any call to recreate the container.
			client := mockContainerClient.NewMockClient(t)
			client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{app}, nil).Once()
			client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
				Return(true, types.ImageID("sha256:new"), "", nil)
			client.EXPECT().GetImageAnnotations(mock.Anything, mock.Anything).Return(oci.Annotations{}).Maybe()
			client.EXPECT().StopAndRemoveContainer(mock.Anything, app, mock.Anything).
				Return(fmt.Errorf("no such container: %w", cerrdefs.ErrNotFound)).Once()

			report, _, err := update(newTestLogger(t), t.Context(), client, types.UpdateParams{RollingRestart: rolling}, ratelimit.New())
			require.NoError(t, err)

			skips := reportErrors(report.Skipped())
			require.Contains(t, skips, "app")
			assert.Contains(t, skips["app"], "container no longer exists")
			assert.Empty(t, report.Failed())
			assert.Empty(t, report.Updated())
		})
	}
}

// TestUpdate_SkipSelfUpdateLeavesDependentWatchtowerRunning verifies that
// while self-updates are disabled, a Watchtower container is not recreated
// when a container it depends on is updated. Only the dependency is replaced.
func TestUpdate_SkipSelfUpdateLeavesDependentWatchtowerRunning(t *testing.T) {
	t.Parallel()

	app := skipTestContainer("a000000000000000000000000000000000000000000000000000000000000000", "app", nil)
	watchtower := skipTestContainer("b000000000000000000000000000000000000000000000000000000000000000", "watchtower",
		map[string]string{
			"com.centurylinklabs.watchtower":            "true",
			"com.centurylinklabs.watchtower.depends-on": "app",
		})

	// The mock fails the test on a second recreation.
	client := mockContainerClient.NewMockClient(t)
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).
		Return([]types.Container{app, watchtower}, nil).Once()
	client.EXPECT().IsContainerStale(mock.Anything, app, mock.Anything).
		Return(true, types.ImageID("sha256:new"), "", nil).Once()
	client.EXPECT().GetImageAnnotations(mock.Anything, mock.Anything).Return(oci.Annotations{}).Maybe()
	client.EXPECT().StopAndRemoveContainer(mock.Anything, app, mock.Anything).Return(nil).Once()
	client.EXPECT().CreateContainer(mock.Anything, app).
		Return(types.ContainerID("e000000000000000000000000000000000000000000000000000000000000000"), nil).Once()
	client.EXPECT().StartContainerByID(mock.Anything, mock.Anything).Return(nil).Once()
	client.EXPECT().GetContainer(mock.Anything, mock.Anything).Return(app, nil).Maybe()

	report, _, err := update(newTestLogger(t), t.Context(), client, types.UpdateParams{SkipSelfUpdate: true}, ratelimit.New())
	require.NoError(t, err)

	assert.Equal(t, []string{app.Name()}, reportNames(report.Updated()))
	assert.Empty(t, report.Failed())
}

// messageLevels returns the level of each log record with the given message.
func messageLevels(t *testing.T, logs, message string) []string {
	t.Helper()

	var levels []string

	for line := range strings.Lines(logs) {
		var record map[string]any

		require.NoError(t, json.Unmarshal([]byte(line), &record))

		if record["message"] == message {
			level, ok := record["level"].(string)
			require.True(t, ok)

			levels = append(levels, level)
		}
	}

	return levels
}

// repeatLevel returns n copies of a log level name.
func repeatLevel(level string, n int) []string {
	levels := make([]string, n)
	for i := range levels {
		levels[i] = level
	}

	return levels
}

// randomContainerID returns a new 64-character container ID. Problems are
// recorded per process, so each run of a test needs containers not seen before.
func randomContainerID(t *testing.T) string {
	t.Helper()

	id := make([]byte, 32)
	_, err := rand.Read(id)
	require.NoError(t, err)

	return hex.EncodeToString(id)
}

// TestUpdate_DependencyProblemsWarnOnce verifies that a dependency problem is
// logged as a warning, which notifications include, only on the first scan
// that finds it, and at debug level on later scans.
func TestUpdate_DependencyProblemsWarnOnce(t *testing.T) {
	t.Parallel()

	const dependsOn = "com.centurylinklabs.watchtower.depends-on"

	tests := []struct {
		name       string
		containers []types.Container
		message    string
	}{
		{
			name: "self-dependency",
			containers: []types.Container{
				skipTestContainer(randomContainerID(t), "self",
					map[string]string{dependsOn: "self"}),
			},
			message: "Skipping container update (self-dependency)",
		},
		{
			name: "depends-on entry that names no container",
			containers: []types.Container{
				skipTestContainer(randomContainerID(t), "orphan",
					map[string]string{dependsOn: "missing"}),
			},
			message: "Ignoring depends-on entry that names no container",
		},
		{
			name: "circular dependency",
			containers: []types.Container{
				skipTestContainer(randomContainerID(t), "cycle-a",
					map[string]string{dependsOn: "cycle-b"}),
				skipTestContainer(randomContainerID(t), "cycle-b",
					map[string]string{dependsOn: "cycle-a"}),
			},
			message: "Skipping container update (circular dependency)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := mockContainerClient.NewMockClient(t)
			client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return(tt.containers, nil)
			client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
				Return(false, types.ImageID(""), "", nil).Maybe()

			scanLevels := make([][]string, 0, 2)

			for range 2 {
				var out bytes.Buffer

				log := zerolog.New(&out).Level(zerolog.DebugLevel)

				_, _, err := update(&log, t.Context(), client, types.UpdateParams{}, ratelimit.New())
				require.NoError(t, err)

				scanLevels = append(scanLevels, messageLevels(t, out.String(), tt.message))
			}

			require.NotEmpty(t, scanLevels[0])
			assert.Equal(t, repeatLevel("warn", len(scanLevels[0])), scanLevels[0], "the first scan warns")
			assert.Equal(t, repeatLevel("debug", len(scanLevels[0])), scanLevels[1], "the second scan logs at debug")
		})
	}
}

// TestUpdate_FailedCheckWarnsOnce verifies that a failed staleness check warns
// once per container and cause, so a lasting failure reaches notifications on
// the first scan and is logged at debug on later scans. A check failure that
// is reported in a line of its own, such as a cooldown deferral, a registry
// rate limit, or an invalid Git label, adds no warning.
func TestUpdate_FailedCheckWarnsOnce(t *testing.T) {
	t.Parallel()

	const message = "Could not check container for updates"

	// Each case returns checkErrs[i] from the check in scan i and lists the
	// level the warning has in each scan, or none.
	tests := []struct {
		name      string
		checkErrs []error
		want      [][]string
	}{
		{
			name:      "a lasting failure",
			checkErrs: []error{errors.New("dial tcp: i/o timeout"), errors.New("dial tcp: i/o timeout")},
			want:      [][]string{{"warn"}, {"debug"}},
		},
		{
			name:      "a different cause",
			checkErrs: []error{errors.New("dial tcp: i/o timeout"), errors.New("unauthorized")},
			want:      [][]string{{"warn"}, {"warn"}},
		},
		{
			name:      "a cooldown deferral",
			checkErrs: []error{containerImage.ErrImageCooldown, &containerImage.CooldownError{Delay: "1h"}},
			want:      [][]string{nil, nil},
		},
		{
			name:      "a registry rate limit",
			checkErrs: []error{&ratelimit.Error{}, &ratelimit.Error{}},
			want:      [][]string{nil, nil},
		},
		{
			name: "an invalid Git label",
			checkErrs: []error{
				fmt.Errorf("git check: %w: weekly", git.ErrInvalidPolicy),
				fmt.Errorf("git check: %w: bad host", git.ErrInvalidHost),
			},
			want: [][]string{nil, nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// A fresh ID per run keeps the process-wide record of reported
			// problems from carrying over between test runs.
			app := skipTestContainer(randomContainerID(t), "app", nil)

			got := make([][]string, 0, len(tt.checkErrs))

			for _, checkErr := range tt.checkErrs {
				client := mockContainerClient.NewMockClient(t)
				client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{app}, nil).Once()
				client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
					Return(false, app.ImageID(), "", checkErr).Once()

				var out bytes.Buffer

				log := zerolog.New(&out).Level(zerolog.DebugLevel)

				_, _, err := update(&log, t.Context(), client, types.UpdateParams{}, ratelimit.New())
				require.NoError(t, err)

				got = append(got, messageLevels(t, out.String(), message))
			}

			assert.Equal(t, tt.want, got)
		})
	}
}

// TestUpdate_UnparsableImageWarnsOnce verifies that a container whose image
// reference cannot be parsed is skipped with a warning on the first scan and
// logged at debug on later scans, as for a failed staleness check.
func TestUpdate_UnparsableImageWarnsOnce(t *testing.T) {
	t.Parallel()

	// Neither the uppercase image name nor the fallback built from the
	// container name parses as an image reference.
	app := mockUpdate.CreateMockContainerWithConfig(
		randomContainerID(t),
		"/Bad Name",
		"Org/App:latest",
		true,
		false,
		time.Now(),
		&dockerContainer.Config{Image: "Org/App:latest"},
	)

	got := make([][]string, 0, 2)

	for range 2 {
		client := mockContainerClient.NewMockClient(t)
		client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{app}, nil).Once()

		var out bytes.Buffer

		log := zerolog.New(&out).Level(zerolog.DebugLevel)

		report, _, err := update(&log, t.Context(), client, types.UpdateParams{}, ratelimit.New())
		require.NoError(t, err)
		require.Len(t, report.Skipped(), 1)

		got = append(got, messageLevels(t, out.String(), "Could not check container for updates"))
	}

	assert.Equal(t, [][]string{{"warn"}, {"debug"}}, got)
}

// TestUpdate_UnrecreatableConfigWarnsOnce verifies that a stale container whose
// configuration cannot be recreated is skipped with a warning on the first scan.
// The second scan logs it at debug, so the blocked update reaches notifications
// once. The container's image could not be inspected, so it has no image info.
func TestUpdate_UnrecreatableConfigWarnsOnce(t *testing.T) {
	t.Parallel()

	// Without image info, VerifyConfiguration rejects the container.
	app := container.NewContainer(nil, &dockerContainer.InspectResponse{
		ID:         randomContainerID(t),
		Name:       "/app",
		Config:     &dockerContainer.Config{Image: "org/app:latest"},
		HostConfig: &dockerContainer.HostConfig{},
	}, nil)

	got := make([][]string, 0, 2)

	for range 2 {
		client := mockContainerClient.NewMockClient(t)
		client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{app}, nil).Once()
		client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
			Return(true, types.ImageID("sha256:new"), "", nil).Once()

		var out bytes.Buffer

		log := zerolog.New(&out).Level(zerolog.DebugLevel)

		report, _, err := update(&log, t.Context(), client, types.UpdateParams{}, ratelimit.New())
		require.NoError(t, err)
		require.Len(t, report.Skipped(), 1)

		got = append(got, messageLevels(t, out.String(), "Could not update container. Its configuration cannot be recreated"))
	}

	assert.Equal(t, [][]string{{"warn"}, {"debug"}}, got)
}

// TestUpdate_InvalidGitLabelWarnsOnce verifies that a Git-watched container
// with an invalid git semver policy or git-host label warns on the first scan
// and logs at debug on the next. The git-host warning leaves out the label's
// value, since it can hold a credential.
func TestUpdate_InvalidGitLabelWarnsOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		labels  map[string]string
		message string
	}{
		{
			name:    "git semver policy",
			labels:  map[string]string{labels.GitSemverPolicy: "nightly"},
			message: "Skipped container with an invalid git semver policy",
		},
		{
			name:    "git-host",
			labels:  map[string]string{labels.GitHost: "ftp://user:supersecret@evil.example"},
			message: "Skipped container with an invalid git-host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			containerLabels := map[string]string{
				labels.GitRepo: "https://github.com/org/app.git",
				labels.GitRef:  "main",
			}
			maps.Copy(containerLabels, tt.labels)

			app := skipTestContainer(randomContainerID(t), "app", containerLabels)

			// The label is rejected before any request reaches the remote.
			gitClient := git.New(newTestLogger(t), git.Options{Timeout: time.Second})

			got := make([][]string, 0, 2)

			for range 2 {
				client := mockContainerClient.NewMockClient(t)
				client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{app}, nil).Once()

				var out bytes.Buffer

				log := zerolog.New(&out).Level(zerolog.DebugLevel)

				report, _, err := update(&log, t.Context(), client,
					types.UpdateParams{EnableGitMonitoring: true}, ratelimit.New(), gitClient)
				require.NoError(t, err)
				require.Len(t, report.Skipped(), 1)
				assert.NotContains(t, out.String(), "supersecret")
				// Notification templates name the container from this field.
				assert.Contains(t, out.String(), `"container":"app","image":"org/app:latest","message":"`+tt.message+`"`)

				got = append(got, messageLevels(t, out.String(), tt.message))
			}

			assert.Equal(t, [][]string{{"warn"}, {"debug"}}, got)
		})
	}
}

// TestReportedProblems_StartsOverWhenFull verifies that the record of reported
// problems stays bounded. Once it is full, a new problem clears it, so earlier
// problems are reported again.
func TestReportedProblems_StartsOverWhenFull(t *testing.T) {
	t.Parallel()

	problems := &reportedProblems{seen: make(map[string]struct{})}

	assert.Equal(t, zerolog.WarnLevel, problems.level("problem", "first"))
	assert.Equal(t, zerolog.DebugLevel, problems.level("problem", "first"))

	for i := 1; i < maxReportedProblems; i++ {
		problems.level("problem", strconv.Itoa(i))
	}

	require.Len(t, problems.seen, maxReportedProblems)
	assert.Equal(t, zerolog.WarnLevel, problems.level("problem", "overflow"))
	assert.Len(t, problems.seen, 1)
	assert.Equal(t, zerolog.WarnLevel, problems.level("problem", "first"), "the record started over")
}
