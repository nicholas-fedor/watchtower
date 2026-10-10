package actions

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/container/oci"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// skipTestContainer returns a running container with the given labels.
func skipTestContainer(id, name string, labels map[string]string) types.Container {
	return mockActions.CreateMockContainerWithConfig(
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

			client := mockContainer.NewMockClient(t)
			// The post-check hooks list the containers again after the update.
			client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{app}, nil).Times(2)
			client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
				Return(true, types.ImageID("sha256:new"), "", nil)
			client.EXPECT().GetImageAnnotations(mock.Anything, mock.Anything).Return(oci.Annotations{}).Maybe()
			client.EXPECT().
				ExecuteCommand(mock.Anything, app, "/pre-update.sh", mock.Anything, mock.Anything, mock.Anything).
				Return(true, nil).Once()

			report, _, err := Update(testLogger(), t.Context(), client,
				types.UpdateParams{LifecycleHooks: true, RollingRestart: rolling})
			require.NoError(t, err)

			assert.Equal(t, map[string]string{"app": errSkipUpdate.Error()}, reportErrors(report.Skipped()))
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

			client := mockContainer.NewMockClient(t)
			client.EXPECT().ListContainers(mock.Anything, mock.Anything).
				Return([]types.Container{first, second}, nil).Once()
			client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
				Return(true, types.ImageID("sha256:new"), "", nil)
			client.EXPECT().GetImageAnnotations(mock.Anything, mock.Anything).Return(oci.Annotations{}).Maybe()

			// The update is canceled as soon as the first container is stopped.
			// Update stops containers one at a time on the calling goroutine,
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

			report, _, _ := Update(testLogger(), ctx, client, types.UpdateParams{RollingRestart: rolling})
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

	client := mockContainer.NewMockClient(t)
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).
		Return([]types.Container{first, second}, nil).Once()
	client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
		Return(true, types.ImageID("sha256:new"), "", nil)
	client.EXPECT().GetImageAnnotations(mock.Anything, mock.Anything).Return(oci.Annotations{}).Maybe()

	// Update stops containers one at a time on the calling goroutine, so
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

	report, _, _ := Update(testLogger(), ctx, client, types.UpdateParams{})
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

	client := mockContainer.NewMockClient(t)
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{app}, nil).Once()
	client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
		Return(true, types.ImageID("sha256:new"), "", nil)
	client.EXPECT().GetImageAnnotations(mock.Anything, mock.Anything).Return(oci.Annotations{}).Maybe()
	client.EXPECT().StopAndRemoveContainer(mock.Anything, app, mock.Anything).
		RunAndReturn(func(context.Context, types.Container, time.Duration) error {
			cancel()

			return errors.New("daemon unavailable")
		}).Once()

	report, _, _ := Update(testLogger(), ctx, client, types.UpdateParams{})
	require.NotNil(t, report)

	assert.Empty(t, report.Skipped())
	assert.Equal(t, map[string]string{"app": "failed to stop container: daemon unavailable"},
		reportErrors(report.Failed()))
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
	client := mockContainer.NewMockClient(t)
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

	report, _, err := Update(testLogger(), t.Context(), client, types.UpdateParams{SkipSelfUpdate: true})
	require.NoError(t, err)

	assert.Equal(t, []string{app.Name()}, reportNames(report.Updated()))
	assert.Empty(t, report.Failed())
}
