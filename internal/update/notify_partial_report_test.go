package update

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/oci"
	"github.com/nicholas-fedor/watchtower/internal/observability/events"
	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
)

// TestRunUpdatesWithNotifications_UpdateEndKeepsPartialResults verifies that
// work finished before a session ends early is still reported and cleaned up.
// A rolling restart ends after the first of two containers has been replaced.
// The notification and the metric carry the partial report, and the replaced
// container's old image is removed. A stop ends the session without a failed
// scan, so subscribers get scan_completed. A passed deadline is an update
// error, so the metric marks a failed scan and subscribers get scan_failed.
func TestRunUpdatesWithNotifications_UpdateEndKeepsPartialResults(t *testing.T) {
	t.Parallel()

	const newID = container.ContainerID("e000000000000000000000000000000000000000000000000000000000000000")

	tests := []struct {
		name string
		// newContext returns the session's context and the wait for the
		// replacement to become healthy, which ends the session.
		newContext func(t *testing.T) (context.Context, func())
		// wantMetric is the metric the session returns. A failed scan ends
		// with scan_failed for subscribers, any other with scan_completed.
		wantMetric *metrics.Metric
	}{
		{
			name: "stopped",
			newContext: func(t *testing.T) (context.Context, func()) {
				t.Helper()

				ctx, cancel := context.WithCancel(t.Context())
				t.Cleanup(cancel)

				return ctx, cancel
			},
			wantMetric: &metrics.Metric{Scanned: 1, Updated: 1, Skipped: 1},
		},
		{
			name: "past its deadline",
			newContext: func(t *testing.T) (context.Context, func()) {
				t.Helper()

				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				t.Cleanup(cancel)

				return ctx, func() { time.Sleep(2 * time.Minute) }
			},
			wantMetric: &metrics.Metric{Scanned: 1, Updated: 1, Skipped: 1, ScanFailed: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				// appContainer returns a running container that uses the given image.
				appContainer := func(id, name, image string) *container.Container {
					return mockUpdate.CreateMockContainerWithConfig(
						id,
						name,
						image,
						true,
						false,
						time.Now(),
						&dockerContainer.Config{Image: image},
					)
				}

				first := appContainer("a000000000000000000000000000000000000000000000000000000000000000", "/app-a", "org/app-a:1")
				second := appContainer("b000000000000000000000000000000000000000000000000000000000000000", "/app-b", "org/app-b:1")

				ctx, endSession := tt.newContext(t)

				client := newMockClient(t)
				client.EXPECT().ListContainers(mock.Anything, mock.Anything).
					Return([]*container.Container{first, second}, nil).Once()
				client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
					Return(true, container.ImageID("sha256:new"), "", nil).Times(2)
				client.EXPECT().GetImageAnnotations(mock.Anything, mock.Anything).Return(oci.Annotations{})

				// Only one container is replaced. The session ends while waiting
				// for the replacement to become healthy, so the rolling restart
				// stops before the other container.
				client.EXPECT().StopAndRemoveContainer(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
				client.EXPECT().CreateContainer(mock.Anything, mock.Anything).Return(newID, nil).Once()
				client.EXPECT().StartContainerByID(mock.Anything, newID).Return(nil).Once()
				client.EXPECT().WaitForContainerHealthy(mock.Anything, newID, mock.Anything).
					RunAndReturn(func(context.Context, container.ContainerID, time.Duration) error {
						endSession()

						return nil
					}).Once()

				var removed []container.ImageID

				client.EXPECT().RemoveImageByID(mock.Anything, mock.Anything, mock.Anything).
					Run(func(_ context.Context, id container.ImageID, _ string) {
						removed = append(removed, id)
					}).
					Return(nil)

				notifier := mockUpdate.NewMockNotifier(t)
				batch := expectSessionBatch(t, notifier)
				notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(true).Maybe()

				broadcaster := events.NewBroadcaster()
				published, _ := broadcaster.SubscribeWithDone()

				metric := RunUpdatesWithNotifications(ctx, RunUpdatesWithNotificationsParams{
					Logger:           newTestLogger(t),
					Client:           client,
					Notifier:         notifier,
					EventBroadcaster: broadcaster,
					Update:           updateParams.Params{RollingRestart: true, Cleanup: true},
				})

				assert.Equal(t, tt.wantMetric, metric)

				require.Len(t, batch.sent, 1, "one notification is sent")
				require.Len(t, batch.sent[0].Updated(), 1, "the notification reports the replaced container")
				assert.Len(t, batch.sent[0].Skipped(), 1, "the notification reports the container left unprocessed as skipped")
				assert.Empty(t, batch.sent[0].Failed())

				updated := batch.sent[0].Updated()[0]
				assert.Equal(t, []container.ImageID{updated.CurrentImageID()}, removed,
					"the replaced container's old image is removed")

				// Events are published synchronously into the subscriber's buffer.
				var eventTypes []string

				for len(published) > 0 {
					eventTypes = append(eventTypes, (<-published).Type)
				}

				wantEnd := "scan_completed"
				if tt.wantMetric.ScanFailed {
					wantEnd = "scan_failed"
				}

				assert.Equal(t, []string{"scan_started", "image_cleanup", wantEnd}, eventTypes)
			})
		})
	}
}
