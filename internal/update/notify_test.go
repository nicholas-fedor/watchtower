package update

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	mockContainerClient "github.com/nicholas-fedor/watchtower/internal/container/client/mocks"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/observability/events"
	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/reportfixture"
	"github.com/nicholas-fedor/watchtower/internal/update/cleanup"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

// defaultTestUpdateParams returns a complete [updateParams.Params] snapshot for tests.
//
// Parameters:
//   - filter: Container filter for the update session.
//
// Returns:
//   - updateParams.Params: The default policy for tests that set none of their own.
func defaultTestUpdateParams(filter types.Filter) updateParams.Params {
	return updateParams.Params{
		Filter:       filter,
		Timeout:      time.Minute,
		LifecycleUID: 1000,
		LifecycleGID: 1001,
	}
}

// TestRunUpdatesWithNotifications_ScanCompletedCounts verifies that the
// scan_completed event carries the same counts as the metric the session
// returns. The session skips three containers in a dependency cycle and finds
// one fresh container. Of three stale containers, two fail to stop and one is
// recreated. Every count then has its own value, which catches a count taken
// from the wrong field.
func TestRunUpdatesWithNotifications_ScanCompletedCounts(t *testing.T) {
	t.Parallel()

	// cycleMember returns a stale container that depends on the next member
	// of a three-container cycle. The session skips every member.
	cycleMember := func(name, dependsOn string) types.Container {
		return mockUpdate.CreateMockContainerWithConfig(
			name,
			"/"+name,
			"image:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Image:  "image:latest",
				Labels: map[string]string{labels.DependsOn: dependsOn},
			},
		)
	}

	client := mockUpdate.CreateMockClient(
		&mockUpdate.TestData{
			Containers: []types.Container{
				cycleMember("cycle-a", "cycle-b"),
				cycleMember("cycle-b", "cycle-c"),
				cycleMember("cycle-c", "cycle-a"),
				mockUpdate.CreateMockContainer("stale-a", "stale-a", "image:latest", time.Now()),
				mockUpdate.CreateMockContainer("stale-b", "stale-b", "image:latest", time.Now()),
				mockUpdate.CreateMockContainer("stale-c", "stale-c", "image:latest", time.Now()),
				mockUpdate.CreateMockContainer("fresh", "fresh", "image:latest", time.Now()),
			},
			Staleness: map[string]bool{
				"stale-a": true,
				"stale-b": true,
				"stale-c": true,
				"fresh":   false,
			},
			StopContainerError:     errors.New("stop failed"),
			StopContainerFailCount: 2,
		},
		false,
		false,
	)

	broadcaster := events.NewBroadcaster()
	published, _ := broadcaster.SubscribeWithDone()

	metric := RunUpdatesWithNotifications(t.Context(), RunUpdatesWithNotificationsParams{
		Logger:           newTestLogger(t),
		Client:           client,
		EventBroadcaster: broadcaster,
		Update:           defaultTestUpdateParams(filters.NoFilter),
	})
	require.NotNil(t, metric)
	assert.Equal(t, 1, metric.Updated)

	// Events are published synchronously into the subscriber's buffer.
	var received []events.Event

	for len(published) > 0 {
		received = append(received, <-published)
	}

	require.Len(t, received, 2)
	assert.Equal(t, "scan_completed", received[1].Type)
	assert.Equal(t, events.ScanCompletedData{Scanned: 4, Updated: 1, Failed: 2, Skipped: 3}, received[1].Data)
	assert.Equal(t, events.ScanCompletedData{
		Scanned: metric.Scanned,
		Updated: metric.Updated,
		Failed:  metric.Failed,
		Skipped: metric.Skipped,
	}, received[1].Data)
}

// TestRunUpdatesWithNotifications_PanicEndsSession verifies that a panic during
// an update session ends it as an update error does. The panic is logged at
// error without notify=no, so the notification hook takes it. The session's
// notification batch is sent at once without a report, so report templates
// fall back to listing its entries. The scan_failed event follows
// scan_started. The metric marks a failed scan. The stack is logged at debug
// with notify=no.
func TestRunUpdatesWithNotifications_PanicEndsSession(t *testing.T) {
	t.Parallel()

	log, output := logcapture.New(zerolog.DebugLevel)

	client := mockContainerClient.NewMockClient(t)
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).
		RunAndReturn(func(context.Context, ...types.Filter) ([]types.Container, error) {
			panic("boom")
		}).Once()

	// Without a report, report templates list the batch's entries. The mock
	// fails the test on any SendNotification call.
	notifier := mockUpdate.NewMockNotifier(t)
	// The session starts its batch before the panic's Flush. Its deferred end
	// runs after it.
	mock.InOrder(
		expectSessionBatch(t, notifier, "Flush"),
		notifier.EXPECT().Flush().Once(),
	)

	broadcaster := events.NewBroadcaster()
	published, _ := broadcaster.SubscribeWithDone()

	var metric *metrics.Metric

	require.NotPanics(t, func() {
		metric = RunUpdatesWithNotifications(t.Context(), RunUpdatesWithNotificationsParams{
			Logger:           log,
			Client:           client,
			Notifier:         notifier,
			EventBroadcaster: broadcaster,
			Update:           defaultTestUpdateParams(filters.NoFilter),
		})
	})

	assert.Equal(t, &metrics.Metric{ScanFailed: true}, metric)

	// Events are published synchronously into the subscriber's buffer.
	var eventTypes []string

	for len(published) > 0 {
		eventTypes = append(eventTypes, (<-published).Type)
	}

	assert.Equal(t, []string{"scan_started", "scan_failed"}, eventTypes)

	var panicLine, stackLine string

	for line := range strings.SplitSeq(output.String(), "\n") {
		switch {
		case strings.Contains(line, `msg="Update panicked"`):
			panicLine = line
		case strings.Contains(line, `msg="Stack of the update panic"`):
			stackLine = line
		default:
		}
	}

	assert.Contains(t, panicLine, "level=error")
	assert.Contains(t, panicLine, "error=boom")
	assert.NotContains(t, panicLine, "notify=no")
	assert.Contains(t, stackLine, "level=debug")
	assert.Contains(t, stackLine, "notify=no")
}

// TestRunUpdates verifies an update run: the metric it records, the reports
// and notifications it sends, its image cleanup and logs, and its behavior
// with several scopes and when errors occur.
func TestRunUpdates(t *testing.T) {
	t.Parallel()

	t.Run("handleUpdateResult", func(t *testing.T) {
		t.Parallel()

		t.Run("given an error without a report", func(t *testing.T) {
			t.Parallel()

			t.Run("should return a zero metric", func(t *testing.T) {
				t.Parallel()

				err := errors.New("test error")

				metric := handleUpdateResult(newTestLogger(t), nil, err, nil)
				require.NotNil(t, metric)
				assert.Equal(t, 0, metric.Scanned)
				assert.Equal(t, 0, metric.Updated)
				assert.Equal(t, 0, metric.Failed)
			})
		})

		t.Run("given a nil result", func(t *testing.T) {
			t.Parallel()

			t.Run("should return a zero metric", func(t *testing.T) {
				t.Parallel()

				metric := handleUpdateResult(newTestLogger(t), nil, nil, nil)
				require.NotNil(t, metric)
				assert.Equal(t, 0, metric.Scanned)
				assert.Equal(t, 0, metric.Updated)
				assert.Equal(t, 0, metric.Failed)
			})
		})

		t.Run("given a valid result with no error", func(t *testing.T) {
			t.Parallel()

			t.Run("should return nil", func(t *testing.T) {
				t.Parallel()

				result := &mockTypes.MockReport{}
				metric := handleUpdateResult(newTestLogger(t), result, nil, nil)
				assert.Nil(t, metric)
			})
		})
	})

	t.Run("RunUpdatesWithNotifications", func(t *testing.T) {
		t.Parallel()

		t.Run("notifier is nil", func(t *testing.T) {
			t.Parallel()

			t.Run("should not start notification batching", func(t *testing.T) {
				t.Parallel()

				client := mockUpdate.CreateMockClient(
					&mockUpdate.TestData{
						Containers: []types.Container{
							mockUpdate.CreateMockContainer(
								"test-container",
								"test-container",
								"image:latest",
								time.Now(),
							),
						},
						Staleness: map[string]bool{
							"test-container": false, // Container is not stale
						},
					},
					false,
					false,
				)

				params := RunUpdatesWithNotificationsParams{
					Logger:                       newTestLogger(t),
					Client:                       client,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Update:                       defaultTestUpdateParams(filters.NoFilter),
				}
				metric := RunUpdatesWithNotifications(t.Context(), params)

				require.NotNil(t, metric)
			})
		})

		t.Run("notifier is provided", func(t *testing.T) {
			t.Parallel()

			t.Run("should handle notification split by container", func(t *testing.T) {
				t.Parallel()

				client := mockUpdate.CreateMockClient(
					&mockUpdate.TestData{
						Containers: []types.Container{
							mockUpdate.CreateMockContainerWithConfig(
								"test-container",
								"test-container",
								"image:latest",
								true,
								false,
								time.Now().Add(-time.Hour),
								&dockerContainer.Config{},
							),
						},
						Staleness: map[string]bool{"test-container": true},
					},
					false,
					false,
				)

				notifier := mockUpdate.NewMockNotifier(t)
				// The session ends its batch after its split notification.
				notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(true)
				mock.InOrder(
					expectSessionBatch(t, notifier, "SendNotification"),
					notifier.EXPECT().SendNotification(mock.Anything).Once(),
				)

				params := RunUpdatesWithNotificationsParams{
					Logger:                       newTestLogger(t),
					Client:                       client,
					Notifier:                     notifier,
					NotificationSplitByContainer: true,
					NotificationReport:           false,
					Update:                       defaultTestUpdateParams(filters.NoFilter),
				}
				metric := RunUpdatesWithNotifications(t.Context(), params)

				require.NotNil(t, metric)
				notifier.AssertExpectations(t)
			})
		})

		t.Run("no containers are provided", func(t *testing.T) {
			t.Parallel()

			t.Run("should return zero metric when container list is empty", func(t *testing.T) {
				t.Parallel()

				client := mockUpdate.CreateMockClient(
					&mockUpdate.TestData{
						Containers: []types.Container{},
						Staleness:  map[string]bool{},
					},
					false,
					false,
				)

				params := RunUpdatesWithNotificationsParams{
					Logger:                       newTestLogger(t),
					Client:                       client,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Update:                       defaultTestUpdateParams(filters.NoFilter),
				}

				// Empty container list results in zero metrics
				metric := RunUpdatesWithNotifications(t.Context(), params)

				require.NotNil(t, metric)
				assert.Equal(t, 0, metric.Scanned)
				assert.Equal(t, 0, metric.Updated)
				assert.Equal(t, 0, metric.Failed)
			})
		})

		t.Run("CurrentContainerID is set", func(t *testing.T) {
			t.Parallel()

			t.Run("should correctly pass CurrentContainerID to UpdateConfig and skip other Watchtower containers", func(t *testing.T) {
				t.Parallel()

				// Create two Watchtower containers with the watchtower label, but only container1 should be updated due to CurrentContainerID
				watchtowerConfig := &dockerContainer.Config{
					Image:  "watchtower:latest",
					Labels: map[string]string{"com.centurylinklabs.watchtower": "true"},
				}

				container1 := mockUpdate.CreateMockContainerWithConfig(
					"container1-id",
					"watchtower-1",
					"watchtower:latest",
					true,
					false,
					time.Now().Add(-time.Hour),
					watchtowerConfig,
				)

				container2 := mockUpdate.CreateMockContainerWithConfig(
					"container2-id",
					"watchtower-2",
					"watchtower:latest",
					true,
					false,
					time.Now().Add(-time.Hour),
					watchtowerConfig,
				)

				client := mockUpdate.CreateMockClient(
					&mockUpdate.TestData{
						Containers: []types.Container{container1, container2},
						Staleness: map[string]bool{
							"watchtower-1": true,
							"watchtower-2": true,
						},
					},
					false,
					false,
				)

				update := defaultTestUpdateParams(filters.NoFilter)
				update.CurrentContainerID = types.ContainerID("container1-id")

				params := RunUpdatesWithNotificationsParams{
					Logger:                       newTestLogger(t),
					Client:                       client,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Update:                       update,
				}

				metric := RunUpdatesWithNotifications(t.Context(), params)

				require.NotNil(t, metric)
				// Only container1 should be updated, container2 should be skipped due to CurrentContainerID
				require.Len(t, client.TestData.CreateOrder, 1)
				assert.Contains(t, client.TestData.CreateOrder, "watchtower-1")
				assert.NotContains(t, client.TestData.CreateOrder, "watchtower-2")
			})
		})
	})

	t.Run("executeUpdate", func(t *testing.T) {
		t.Parallel()

		t.Run("should execute update and return results", func(t *testing.T) {
			t.Parallel()

			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: []types.Container{
						mockUpdate.CreateMockContainer(
							"test-container",
							"test-container",
							"image:latest",
							time.Now(),
						),
					},
					Staleness: map[string]bool{"test-container": true},
				},
				false,
				false,
			)

			config := updateParams.Params{
				Filter:           filters.NoFilter,
				Cleanup:          false,
				NoRestart:        false,
				MonitorOnly:      false,
				LifecycleHooks:   false,
				RollingRestart:   false,
				LabelPrecedence:  false,
				NoPull:           false,
				Timeout:          time.Minute,
				LifecycleUID:     1000,
				LifecycleGID:     1001,
				PullFailureDelay: time.Duration(0),
				RunOnce:          true,
			}

			// Test that function can be called without panicking
			result, cleanupImages, err := executeUpdate(newTestLogger(t),
				t.Context(),
				client,
				config,
				ratelimit.New(),
				nil,
			)

			// We expect some result or error, but mainly that it doesn't panic
			require.NotNil(t, result)
			require.NotNil(t, cleanupImages)
			// For this mock configuration, we expect no error
			require.NoError(t, err)
		})
	})

	t.Run("logUpdateReport", func(t *testing.T) {
		t.Parallel()

		t.Run("should log update report details", func(t *testing.T) {
			t.Parallel()

			mockReport := mockTypes.NewMockReport(t)

			mockContainer1 := mockTypes.NewMockContainerReport(t)
			mockContainer2 := mockTypes.NewMockContainerReport(t)
			mockContainer3 := mockTypes.NewMockContainerReport(t)

			mockContainer3.EXPECT().Name().Return("container3")

			mockReport.EXPECT().
				Scanned().
				Return([]types.ContainerReport{mockContainer1, mockContainer2})
			mockReport.EXPECT().Updated().Return([]types.ContainerReport{mockContainer3})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})

			// Function just logs, so we verify it doesn't panic and calls expected methods
			logUpdateReport(newTestLogger(t), mockReport)
		})

		t.Run("should handle reports with no updates", func(t *testing.T) {
			t.Parallel()

			mockReport := mockTypes.NewMockReport(t)

			mockContainer1 := mockTypes.NewMockContainerReport(t)
			mockContainer2 := mockTypes.NewMockContainerReport(t)

			mockReport.EXPECT().
				Scanned().
				Return([]types.ContainerReport{mockContainer1, mockContainer2})
			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})

			// Should handle empty updated containers gracefully
			logUpdateReport(newTestLogger(t), mockReport)
		})
	})

	t.Run("sendNotifications", func(t *testing.T) {
		t.Parallel()

		t.Run("should not send notifications when notifier is nil", func(t *testing.T) {
			t.Parallel()

			mockReport := mockTypes.NewMockReport(t)
			sendNotifications(newTestLogger(t), nil, false, false, mockReport)
			// No expectations since notifier is nil
		})

		t.Run("should send grouped notification when split is false", func(t *testing.T) {
			t.Parallel()

			mockReport := mockTypes.NewMockReport(t)
			notifier := mockUpdate.NewMockNotifier(t)
			notifier.EXPECT().ShouldSendNotification(mockReport).Return(true)
			notifier.EXPECT().SendNotification(mockReport).Return()
			sendNotifications(newTestLogger(t), notifier, false, false, mockReport)
			notifier.AssertExpectations(t)
		})

		// A report below the notifications level, such as one without failures
		// at the error level, is not sent. The batch still ends. Its queued
		// lines are sent without the report instead of waiting for the next
		// session.
		t.Run("should flush the batch when the report is below the notifications level", func(t *testing.T) {
			t.Parallel()

			mockReport := mockTypes.NewMockReport(t)
			notifier := mockUpdate.NewMockNotifier(t)
			notifier.EXPECT().ShouldSendNotification(mockReport).Return(false)
			notifier.EXPECT().Flush().Once()
			sendNotifications(newTestLogger(t), notifier, false, false, mockReport)
		})

		t.Run("should call sendSplitNotifications when split is true", func(t *testing.T) {
			t.Parallel()

			mockReport := mockTypes.NewMockReport(t)
			notifier := mockUpdate.NewMockNotifier(t)

			// Set up mock expectations for split notification behavior
			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			sendNotifications(newTestLogger(t), notifier, true, false, mockReport)
			notifier.AssertExpectations(t)
		})
	})

	t.Run("sendSplitNotifications", func(t *testing.T) {
		t.Parallel()

		t.Run("should send split notifications for updated containers when notificationReport is true", func(t *testing.T) {
			t.Parallel()

			mockReport := mockTypes.NewMockReport(t)
			mockContainer := mockTypes.NewMockContainerReport(t)

			mockContainer.EXPECT().ID().Return(types.ContainerID("test-id"))
			mockContainer.EXPECT().Name().Return("test-container")
			mockContainer.EXPECT().Error().Return("")

			mockReport.EXPECT().Updated().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			// Mock the SendNotification call
			notifier := mockUpdate.NewMockNotifier(t)
			notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(true)
			notifier.EXPECT().SendNotification(mock.Anything).Return()

			sendSplitNotifications(newTestLogger(t), notifier, true, mockReport)

			notifier.AssertExpectations(t)
		})

		t.Run("should handle empty results gracefully", func(t *testing.T) {
			t.Parallel()

			mockReport := mockTypes.NewMockReport(t)
			notifier := mockUpdate.NewMockNotifier(t)

			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			// With empty results, no notifications should be sent
			sendSplitNotifications(newTestLogger(t), notifier, true, mockReport)
			notifier.AssertExpectations(t)
		})

		t.Run("should send filtered entry notifications when notificationReport is false", func(t *testing.T) {
			t.Parallel()

			mockReport := mockTypes.NewMockReport(t)
			mockContainer := mockTypes.NewMockContainerReport(t)

			mockContainer.EXPECT().ID().Return(types.ContainerID("test-id"))
			mockContainer.EXPECT().Name().Return("test-container")
			mockContainer.EXPECT().ImageName().Return("test-image:latest")
			mockContainer.EXPECT().Error().Return("")

			mockReport.EXPECT().Updated().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			notifier := mockUpdate.NewMockNotifier(t)
			notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(true)
			notifier.EXPECT().SendNotification(mock.Anything).Return()

			sendSplitNotifications(newTestLogger(t), notifier, false, mockReport)
			notifier.AssertExpectations(t)
		})

		t.Run("should handle restarted containers when notificationReport is true", func(t *testing.T) {
			t.Parallel()

			mockReport := mockTypes.NewMockReport(t)
			mockContainer := mockTypes.NewMockContainerReport(t)

			mockContainer.EXPECT().ID().Return(types.ContainerID("restart-id"))
			mockContainer.EXPECT().Name().Return("restart-container")
			mockContainer.EXPECT().Error().Return("")

			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			notifier := mockUpdate.NewMockNotifier(t)
			notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(true)
			notifier.EXPECT().SendNotification(mock.Anything).Return()

			sendSplitNotifications(newTestLogger(t), notifier, true, mockReport)
			notifier.AssertExpectations(t)
		})

		t.Run("should skip containers with empty names", func(t *testing.T) {
			t.Parallel()

			mockReport := mockTypes.NewMockReport(t)
			mockContainer := mockTypes.NewMockContainerReport(t)

			mockContainer.EXPECT().ID().Return(types.ContainerID("test-id"))
			mockContainer.EXPECT().Name().Return("") // Empty name

			mockReport.EXPECT().Updated().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			notifier := mockUpdate.NewMockNotifier(t)
			// No SendNotification should be called due to empty name

			sendSplitNotifications(newTestLogger(t), notifier, true, mockReport)
			notifier.AssertExpectations(t)
		})

		t.Run("should handle restarted containers when notificationReport is false", func(t *testing.T) {
			t.Parallel()

			mockReport := mockTypes.NewMockReport(t)
			mockContainer := mockTypes.NewMockContainerReport(t)

			mockContainer.EXPECT().ID().Return(types.ContainerID("restart-id"))
			mockContainer.EXPECT().Name().Return("restart-container")
			mockContainer.EXPECT().ImageName().Return("restart-image:latest")
			mockContainer.EXPECT().Error().Return("")

			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			notifier := mockUpdate.NewMockNotifier(t)
			notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(true)
			notifier.EXPECT().SendNotification(mock.Anything).Return()

			sendSplitNotifications(newTestLogger(t), notifier, false, mockReport)
			notifier.AssertExpectations(t)
		})

		t.Run("should handle restarted containers with empty NewContainerID", func(t *testing.T) {
			t.Parallel()

			mockReport := mockTypes.NewMockReport(t)
			mockContainer := mockTypes.NewMockContainerReport(t)

			mockContainer.EXPECT().ID().Return(types.ContainerID("restart-id"))
			mockContainer.EXPECT().Name().Return("restart-container")
			mockContainer.EXPECT().ImageName().Return("restart-image:latest")
			mockContainer.EXPECT().Error().Return("")

			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainer})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			notifier := mockUpdate.NewMockNotifier(t)
			notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(true)
			notifier.EXPECT().SendNotification(mock.Anything).Return()

			sendSplitNotifications(newTestLogger(t), notifier, false, mockReport)
			notifier.AssertExpectations(t)
		})

		t.Run("should handle empty container lists gracefully", func(t *testing.T) {
			t.Parallel()

			mockReport := mockTypes.NewMockReport(t)

			// Return empty slices - all lists are empty
			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
			mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
			mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

			notifier := mockUpdate.NewMockNotifier(t)
			// No SendNotification should be called since all lists are empty

			sendSplitNotifications(newTestLogger(t), notifier, true, mockReport)
			notifier.AssertExpectations(t)
		})
	})

	t.Run("notification level filtering", func(t *testing.T) {
		t.Parallel()

		t.Run("when notification level is error", func(t *testing.T) {
			t.Parallel()

			t.Run("should not send notifications when no errors occur", func(t *testing.T) {
				t.Parallel()

				mockReport := mockTypes.NewMockReport(t)

				// Set up report with no errors - only successful updates
				mockContainer := mockTypes.NewMockContainerReport(t)
				mockContainer.EXPECT().ID().Return(types.ContainerID("test-id"))
				mockContainer.EXPECT().Name().Return("test-container")
				mockContainer.EXPECT().Error().Return("") // No error

				mockReport.EXPECT().Updated().Return([]types.ContainerReport{mockContainer})
				mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
				mockReport.EXPECT().Stale().Return([]types.ContainerReport{})
				mockReport.EXPECT().Scanned().Return([]types.ContainerReport{mockContainer})
				mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
				mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})
				mockReport.EXPECT().Fresh().Return([]types.ContainerReport{})

				notifier := mockUpdate.NewMockNotifier(t)
				notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(false)
				// With notification level set to error and no errors, no notification should be sent

				sendSplitNotifications(newTestLogger(t), notifier, true, mockReport)
				notifier.AssertExpectations(t)
			})
		})
	})

	t.Run("performImageCleanup", func(t *testing.T) {
		t.Parallel()

		t.Run("should return empty slice when cleanup is disabled", func(t *testing.T) {
			t.Parallel()

			client := mockUpdate.CreateMockClient(&mockUpdate.TestData{}, false, false)
			cleanedImages := performImageCleanup(newTestLogger(t), t.Context(), client, false, []cleanup.RemovedImageInfo{}, time.Minute)
			assert.Empty(t, cleanedImages)
		})

		t.Run("should perform cleanup when cleanup is enabled", func(t *testing.T) {
			t.Parallel()

			client := mockUpdate.CreateMockClient(&mockUpdate.TestData{}, false, false)
			cleanedImages := performImageCleanup(newTestLogger(t), t.Context(), client, true, []cleanup.RemovedImageInfo{
				{
					ContainerName: "test-container",
					ImageName:     "test-image:v1.0",
					ImageID:       types.ImageID("sha256:123"),
				},
			}, time.Minute)
			// The function should return the cleaned images when cleanup is enabled
			require.Len(t, cleanedImages, 1)
			assert.Equal(t, "test-container", cleanedImages[0].ContainerName)
		})

		t.Run("should return a valid slice when cleanup input is empty", func(t *testing.T) {
			t.Parallel()

			client := mockUpdate.CreateMockClient(&mockUpdate.TestData{}, false, false)
			cleanedImages := performImageCleanup(newTestLogger(t), t.Context(), client, true, []cleanup.RemovedImageInfo{}, time.Minute)
			// Should return a valid slice even with empty input
			require.NotNil(t, cleanedImages)
		})

		t.Run("should still remove images when the parent context is already canceled", func(t *testing.T) {
			t.Parallel()

			client := mockUpdate.CreateMockClient(&mockUpdate.TestData{}, false, false)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			cleanedImages := performImageCleanup(newTestLogger(t), ctx, client, true, []cleanup.RemovedImageInfo{
				{
					ContainerName: "test-container",
					ImageName:     "test-image:v1.0",
					ImageID:       types.ImageID("sha256:123"),
				},
			}, time.Minute)
			require.Len(t, cleanedImages, 1)
			assert.Equal(t, int32(1), client.TestData.TriedToRemoveImageCount.Load())
		})
	})

	t.Run("generateAndLogMetric", func(t *testing.T) {
		t.Parallel()

		t.Run("should log session completion with notify=no so it is not sent as a notification", func(t *testing.T) {
			t.Parallel()

			mockReport := mockTypes.NewMockReport(t)

			mockReport.EXPECT().Scanned().Return([]types.ContainerReport{})
			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{})

			log, buf := logcapture.NewJSON(zerolog.DebugLevel)
			_ = generateAndLogMetric(log, mockReport)

			var found bool

			for _, entry := range logcapture.Entries[logcapture.Entry](t, buf.String()) {
				if entry["message"] != "Update session completed" {
					continue
				}

				found = true

				assert.Equal(t, "no", entry["notify"])
			}

			assert.True(t, found)
		})

		t.Run("should log skipped count on the session completion line", func(t *testing.T) {
			t.Parallel()

			mockReport := mockTypes.NewMockReport(t)
			skippedA := mockTypes.NewMockContainerReport(t)
			skippedB := mockTypes.NewMockContainerReport(t)

			mockReport.EXPECT().Scanned().Return(make([]types.ContainerReport, 23))
			mockReport.EXPECT().Updated().Return([]types.ContainerReport{})
			mockReport.EXPECT().Failed().Return([]types.ContainerReport{})
			mockReport.EXPECT().Restarted().Return([]types.ContainerReport{})
			mockReport.EXPECT().Skipped().Return([]types.ContainerReport{skippedA, skippedB})

			log, buf := logcapture.NewJSON(zerolog.DebugLevel)
			metric := generateAndLogMetric(log, mockReport)

			assert.Equal(t, 2, metric.Skipped)
			assert.Equal(t, 23, metric.Scanned)

			var found bool

			for _, entry := range logcapture.Entries[logcapture.Entry](t, buf.String()) {
				if entry["message"] != "Update session completed" {
					continue
				}

				found = true

				assert.EqualValues(t, 2, entry["skipped"])
				assert.EqualValues(t, 23, entry["scanned"])
				assert.EqualValues(t, 0, entry["updated"])
				assert.EqualValues(t, 0, entry["failed"])
			}

			assert.True(t, found)
		})
	})

	t.Run("Multiple Concurrent Scopes", func(t *testing.T) {
		t.Parallel()

		t.Run("should support multiple Watchtower instances with different scopes running simultaneously", func(t *testing.T) {
			t.Parallel()

			// Create containers with different scopes
			scopeAContainer := mockUpdate.CreateMockContainerWithConfig(
				"scope-a-container",
				"scope-a-app",
				"app:latest",
				true,
				false,
				time.Now().Add(-time.Hour),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "scope-a",
					},
				},
			)

			scopeBContainer := mockUpdate.CreateMockContainerWithConfig(
				"scope-b-container",
				"scope-b-app",
				"app:latest",
				true,
				false,
				time.Now().Add(-time.Hour),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "scope-b",
					},
				},
			)

			// Create separate container lists for each client
			containersA := []types.Container{scopeAContainer}
			containersB := []types.Container{scopeBContainer}

			// Create separate test data for each client
			testDataA := &mockUpdate.TestData{
				Containers: containersA,
				Staleness: map[string]bool{
					"scope-a-app": true,
				},
			}

			testDataB := &mockUpdate.TestData{
				Containers: containersB,
				Staleness: map[string]bool{
					"scope-b-app": true,
				},
			}

			// Create mock clients for each scope
			clientA := mockUpdate.CreateMockClient(testDataA, false, false)
			clientB := mockUpdate.CreateMockClient(testDataB, false, false)

			// Results channel to collect metrics from concurrent operations
			results := make(chan *metrics.Metric, 2)

			var wg sync.WaitGroup

			// Launch concurrent updates for different scopes
			wg.Add(2)

			go func() {
				defer wg.Done()

				params := RunUpdatesWithNotificationsParams{
					Logger:                       newTestLogger(t),
					Client:                       clientA,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Update: defaultTestUpdateParams(
						filters.FilterByScope(newTestLogger(t), "scope-a", filters.NoFilter),
					),
				}

				metric := RunUpdatesWithNotifications(t.Context(), params)
				results <- metric
			}()

			go func() {
				defer wg.Done()

				params := RunUpdatesWithNotificationsParams{
					Logger:                       newTestLogger(t),
					Client:                       clientB,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Update: defaultTestUpdateParams(
						filters.FilterByScope(newTestLogger(t), "scope-b", filters.NoFilter),
					),
				}

				metric := RunUpdatesWithNotifications(t.Context(), params)
				results <- metric
			}()

			// Wait for all goroutines to complete
			wg.Wait()
			close(results)

			// Collect results
			var metrics []*metrics.Metric
			for metric := range results {
				metrics = append(metrics, metric)
			}

			// Verify we got results from both concurrent operations
			require.Len(t, metrics, 2)

			for _, metric := range metrics {
				require.NotNil(t, metric)
				assert.Equal(t, 1, metric.Scanned)
				// Each scope should scan 1 container
				assert.Equal(t, 1, metric.Updated)
				// Each scope should update 1 container
			}
		})

		t.Run("should ensure scope isolation and prevent interference between concurrent scopes", func(t *testing.T) {
			t.Parallel()

			// Create containers for different scopes
			scope1Container := mockUpdate.CreateMockContainerWithConfig(
				"scope1-watchtower",
				"scope1-watchtower",
				"watchtower:latest",
				true,
				false,
				time.Now().Add(-time.Hour),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "scope1",
					},
				},
			)

			scope2Container := mockUpdate.CreateMockContainerWithConfig(
				"scope2-watchtower",
				"scope2-watchtower",
				"watchtower:latest",
				true,
				false,
				time.Now().Add(-time.Hour),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "scope2",
					},
				},
			)

			allContainers := []types.Container{scope1Container, scope2Container}

			// Create separate test data for each client to track operations independently
			testData1 := &mockUpdate.TestData{
				Containers: allContainers,
				Staleness: map[string]bool{
					"scope1-watchtower": true,
					"scope2-watchtower": false, // scope2 container not stale for scope1 filter
				},
			}

			testData2 := &mockUpdate.TestData{
				Containers: allContainers,
				Staleness: map[string]bool{
					"scope1-watchtower": false, // scope1 container not stale for scope2 filter
					"scope2-watchtower": true,
				},
			}

			client1 := mockUpdate.CreateMockClient(testData1, false, false)
			client2 := mockUpdate.CreateMockClient(testData2, false, false)

			var wg sync.WaitGroup

			wg.Add(2)

			go func() {
				defer wg.Done()

				params := RunUpdatesWithNotificationsParams{
					Logger:                       newTestLogger(t),
					Client:                       client1,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Update: defaultTestUpdateParams(
						filters.FilterByScope(newTestLogger(t), "scope1", filters.WatchtowerContainersFilter),
					),
				}
				RunUpdatesWithNotifications(t.Context(), params)
			}()

			go func() {
				defer wg.Done()

				params := RunUpdatesWithNotificationsParams{
					Logger:                       newTestLogger(t),
					Client:                       client2,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Update: defaultTestUpdateParams(
						filters.FilterByScope(newTestLogger(t), "scope2", filters.WatchtowerContainersFilter),
					),
				}
				RunUpdatesWithNotifications(t.Context(), params)
			}()

			wg.Wait()

			// Verify scope isolation through separate TestData tracking
			// client1 should only create scope1-watchtower
			assert.Contains(t, client1.TestData.CreateOrder, "scope1-watchtower")
			assert.NotContains(t, client1.TestData.CreateOrder, "scope2-watchtower")

			// client2 should only create scope2-watchtower
			assert.Contains(t, client2.TestData.CreateOrder, "scope2-watchtower")
			assert.NotContains(t, client2.TestData.CreateOrder, "scope1-watchtower")
		})

		t.Run("should maintain resource cleanup isolation respecting scope boundaries", func(t *testing.T) {
			t.Parallel()

			// Create Watchtower containers for different scopes
			scopeAWatchtower := mockUpdate.CreateMockContainerWithConfig(
				"watchtower-a",
				"watchtower-a",
				"watchtower:latest",
				true,
				false,
				time.Now().Add(-time.Hour),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "scope-a",
					},
				},
			)

			scopeBWatchtower := mockUpdate.CreateMockContainerWithConfig(
				"watchtower-b",
				"watchtower-b",
				"watchtower:latest",
				true,
				false,
				time.Now().Add(-time.Hour),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "scope-b",
					},
				},
			)

			allContainers := []types.Container{scopeAWatchtower, scopeBWatchtower}

			// Create separate test data for each client to track cleanup independently
			testDataA := &mockUpdate.TestData{
				Containers: allContainers,
				Staleness: map[string]bool{
					"watchtower-a": true,
					"watchtower-b": false, // scope-b container not processed by scope-a filter
				},
			}

			testDataB := &mockUpdate.TestData{
				Containers: allContainers,
				Staleness: map[string]bool{
					"watchtower-a": false, // scope-a container not processed by scope-b filter
					"watchtower-b": true,
				},
			}

			clientA := mockUpdate.CreateMockClient(testDataA, false, false)
			clientB := mockUpdate.CreateMockClient(testDataB, false, false)

			var wg sync.WaitGroup

			wg.Add(2)

			go func() {
				defer wg.Done()

				updateA := defaultTestUpdateParams(
					filters.FilterByScope(newTestLogger(t), "scope-a", filters.WatchtowerContainersFilter),
				)
				updateA.Cleanup = true

				params := RunUpdatesWithNotificationsParams{
					Logger:                       newTestLogger(t),
					Client:                       clientA,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Update:                       updateA,
				}
				RunUpdatesWithNotifications(t.Context(), params)
			}()

			go func() {
				defer wg.Done()

				updateB := defaultTestUpdateParams(
					filters.FilterByScope(newTestLogger(t), "scope-b", filters.WatchtowerContainersFilter),
				)
				updateB.Cleanup = true

				params := RunUpdatesWithNotificationsParams{
					Logger:                       newTestLogger(t),
					Client:                       clientB,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Update:                       updateB,
				}
				RunUpdatesWithNotifications(t.Context(), params)
			}()

			wg.Wait()

			// Verify cleanup isolation - each client updates only the Watchtower container in its scope
			assert.Contains(t, clientA.TestData.CreateOrder, "watchtower-a")
			assert.NotContains(t, clientA.TestData.CreateOrder, "watchtower-b")
			assert.Contains(t, clientB.TestData.CreateOrder, "watchtower-b")
			assert.NotContains(t, clientB.TestData.CreateOrder, "watchtower-a")

			// Neither client removes an image, because the new instance of a renamed
			// Watchtower container cleans up its old image.
			assert.Equal(t, int32(0), clientA.TestData.TriedToRemoveImageCount.Load())
			assert.Equal(t, int32(0), clientB.TestData.TriedToRemoveImageCount.Load())
		})

		t.Run("should handle concurrent operations with different scopes without interference", func(t *testing.T) {
			t.Parallel()

			// Create mixed containers - regular apps and watchtower instances
			appContainer := mockUpdate.CreateMockContainerWithConfig(
				"app-1",
				"my-app",
				"myapp:latest",
				true,
				false,
				time.Now().Add(-time.Hour),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "prod", // This app belongs to prod scope
					},
				},
			)

			watchtowerProd := mockUpdate.CreateMockContainerWithConfig(
				"wt-prod",
				"watchtower-prod",
				"watchtower:latest",
				true,
				false,
				time.Now().Add(-time.Hour),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "prod",
					},
				},
			)

			watchtowerDev := mockUpdate.CreateMockContainerWithConfig(
				"wt-dev",
				"watchtower-dev",
				"watchtower:latest",
				true,
				false,
				time.Now().Add(-time.Hour),
				&dockerContainer.Config{
					Labels: map[string]string{
						"com.centurylinklabs.watchtower":       "true",
						"com.centurylinklabs.watchtower.scope": "dev",
					},
				},
			)

			allContainers := []types.Container{appContainer, watchtowerProd, watchtowerDev}

			testDataProd := &mockUpdate.TestData{
				Containers: allContainers,
				Staleness: map[string]bool{
					"my-app":          true,
					"watchtower-prod": true,
					"watchtower-dev":  true,
				},
			}

			testDataDev := &mockUpdate.TestData{
				Containers: allContainers,
				Staleness: map[string]bool{
					"my-app":          true,
					"watchtower-prod": true,
					"watchtower-dev":  true,
				},
			}

			clientProd := mockUpdate.CreateMockClient(testDataProd, false, false)
			clientDev := mockUpdate.CreateMockClient(testDataDev, false, false)

			results := make(chan map[string]int, 2)

			var wg sync.WaitGroup

			wg.Add(2)

			go func() {
				defer wg.Done()

				params := RunUpdatesWithNotificationsParams{
					Logger:                       newTestLogger(t),
					Client:                       clientProd,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Update: defaultTestUpdateParams(
						filters.FilterByScope(newTestLogger(t), "prod", filters.NoFilter),
					),
				}

				metric := RunUpdatesWithNotifications(t.Context(), params)
				results <- map[string]int{"prod-scanned": metric.Scanned, "prod-updated": metric.Updated}
			}()

			go func() {
				defer wg.Done()

				params := RunUpdatesWithNotificationsParams{
					Logger:                       newTestLogger(t),
					Client:                       clientDev,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Update: defaultTestUpdateParams(
						filters.FilterByScope(newTestLogger(t), "dev", filters.NoFilter),
					),
				}

				metric := RunUpdatesWithNotifications(t.Context(), params)
				results <- map[string]int{"dev-scanned": metric.Scanned, "dev-updated": metric.Updated}
			}()

			wg.Wait()
			close(results)

			// Collect results
			var resultMaps []map[string]int
			for resultMap := range results {
				resultMaps = append(resultMaps, resultMap)
			}

			require.Len(t, resultMaps, 2)

			// Verify concurrent operations completed independently
			totalScanned := 0
			totalUpdated := 0

			for _, resultMap := range resultMaps {
				for key, value := range resultMap {
					if key == "prod-scanned" || key == "dev-scanned" {
						totalScanned += value
					}

					if key == "prod-updated" || key == "dev-updated" {
						totalUpdated += value
					}
				}
			}

			// prod scans my-app and watchtower-prod, and dev scans watchtower-dev
			assert.Equal(t, 3, totalScanned)
			// prod updates my-app and watchtower-prod, and dev updates watchtower-dev
			assert.Equal(t, 3, totalUpdated)
		})
	})

	t.Run("Scoped Environment Error Reporting and State Consistency", func(t *testing.T) {
		t.Parallel()

		t.Run("scoped operations encounter errors", func(t *testing.T) {
			t.Parallel()

			t.Run("should maintain state consistency when scoped update operations fail partially", func(t *testing.T) {
				t.Parallel()

				// Create containers in same scope with one failing update
				scopeAContainer1 := mockUpdate.CreateMockContainerWithConfig(
					"scope-a-1",
					"app-a-1",
					"app:latest",
					true,
					false,
					time.Now().Add(-time.Hour),
					&dockerContainer.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.scope": "scope-a",
						},
					},
				)

				scopeAContainer2 := mockUpdate.CreateMockContainerWithConfig(
					"scope-a-2",
					"app-a-2",
					"app:latest",
					true,
					false,
					time.Now().Add(-time.Hour),
					&dockerContainer.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.scope": "scope-a",
						},
					},
				)

				allContainers := []types.Container{scopeAContainer1, scopeAContainer2}

				// Set up mock to fail updates
				testData := &mockUpdate.TestData{
					Containers: allContainers,
					Staleness:  map[string]bool{"app-a-1": true, "app-a-2": true},
					StopContainerError: errors.New(
						"container stop failed",
					), // Fail during update process
					StopContainerFailCount: 2, // Fail both containers
				}

				client := mockUpdate.CreateMockClient(testData, false, false)

				params := RunUpdatesWithNotificationsParams{
					Logger:                       newTestLogger(t),
					Client:                       client,
					Notifier:                     nil, // No notifications for this test
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Update: defaultTestUpdateParams(
						filters.FilterByScope(newTestLogger(t), "scope-a", filters.NoFilter),
					),
				}

				metric := RunUpdatesWithNotifications(t.Context(), params)

				// Verify state consistency: partial failures are properly tracked
				require.NotNil(t, metric)
				assert.Equal(t, 2, metric.Scanned)
				// Both containers in scope scanned
				assert.Equal(t, 2, metric.Failed)  // Both fail due to mock error
				assert.Equal(t, 0, metric.Updated) // No successful updates
			})

			t.Run("should properly isolate scoped operations when cleanup fails", func(t *testing.T) {
				t.Parallel()

				// Test that cleanup failures in one scope don't affect other scopes
				scopeAContainer := mockUpdate.CreateMockContainerWithConfig(
					"cleanup-a",
					"cleanup-app-a",
					"app:v1",
					true,
					false,
					time.Now().Add(-time.Hour),
					&dockerContainer.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower":       "true",
							"com.centurylinklabs.watchtower.scope": "scope-a",
						},
					},
				)

				scopeBContainer := mockUpdate.CreateMockContainerWithConfig(
					"cleanup-b",
					"cleanup-app-b",
					"app:v2",
					true,
					false,
					time.Now().Add(-time.Hour),
					&dockerContainer.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower":       "true",
							"com.centurylinklabs.watchtower.scope": "scope-b",
						},
					},
				)

				allContainers := []types.Container{scopeAContainer, scopeBContainer}

				// Different test data for different scopes
				testDataA := &mockUpdate.TestData{
					Containers: allContainers,
					Staleness: map[string]bool{
						"cleanup-app-a": true,
						"cleanup-app-b": false,
					},
					RemoveImageError: errors.New("cleanup failed in scope-a"),
					FailedImageIDs: []types.ImageID{
						types.ImageID("app:v1"),
					}, // Fail cleanup for scope-a
				}

				testDataB := &mockUpdate.TestData{
					Containers: allContainers,
					Staleness:  map[string]bool{"cleanup-app-a": false, "cleanup-app-b": true},
					// scope-b cleanup succeeds
				}

				clientA := mockUpdate.CreateMockClient(testDataA, false, false)
				clientB := mockUpdate.CreateMockClient(testDataB, false, false)

				// Run scope-a operation (cleanup should fail)
				updateA := defaultTestUpdateParams(
					filters.FilterByScope(newTestLogger(t), "scope-a", filters.NoFilter),
				)
				updateA.Cleanup = true

				paramsA := RunUpdatesWithNotificationsParams{
					Logger:                       newTestLogger(t),
					Client:                       clientA,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Update:                       updateA,
				}

				// Run scope-b operation (cleanup should succeed)
				updateB := defaultTestUpdateParams(
					filters.FilterByScope(newTestLogger(t), "scope-b", filters.NoFilter),
				)
				updateB.Cleanup = true

				paramsB := RunUpdatesWithNotificationsParams{
					Logger:                       newTestLogger(t),
					Client:                       clientB,
					Notifier:                     nil,
					NotificationSplitByContainer: false,
					NotificationReport:           false,
					Update:                       updateB,
				}

				var wg sync.WaitGroup

				results := make(chan *metrics.Metric, 2)

				wg.Add(2)

				go func() {
					defer wg.Done()

					metric := RunUpdatesWithNotifications(t.Context(), paramsA)
					results <- metric
				}()

				go func() {
					defer wg.Done()

					metric := RunUpdatesWithNotifications(t.Context(), paramsB)
					results <- metric
				}()

				wg.Wait()
				close(results)

				// Collect results
				var metrics []*metrics.Metric
				for metric := range results {
					metrics = append(metrics, metric)
				}

				// Verify scope isolation in error handling
				require.Len(t, metrics, 2)

				for _, metric := range metrics {
					require.NotNil(t, metric)
					// Each scope processes exactly one container
					assert.Equal(t, 1, metric.Scanned)
					assert.Equal(t, 1, metric.Updated) // Updates succeed
					assert.Equal(t, 0, metric.Failed)  // No update failures
				}
			})

			t.Run("should ensure proper error reporting isolation between concurrent scoped operations", func(t *testing.T) {
				t.Parallel()

				// Test concurrent scoped operations with different error patterns
				containerX := mockUpdate.CreateMockContainerWithConfig(
					"concurrent-x",
					"app-x",
					"app:latest",
					true,
					false,
					time.Now().Add(-time.Hour),
					&dockerContainer.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.scope": "scope-x",
						},
					},
				)

				containerY := mockUpdate.CreateMockContainerWithConfig(
					"concurrent-y",
					"app-y",
					"app:latest",
					true,
					false,
					time.Now().Add(-time.Hour),
					&dockerContainer.Config{
						Labels: map[string]string{
							"com.centurylinklabs.watchtower.scope": "scope-y",
						},
					},
				)

				allContainers := []types.Container{containerX, containerY}

				// scope-x has update errors, scope-y succeeds
				testDataX := &mockUpdate.TestData{
					Containers:             allContainers,
					Staleness:              map[string]bool{"app-x": true, "app-y": false},
					StopContainerError:     errors.New("scope-x update failure"),
					StopContainerFailCount: 1, // Fail once
				}

				testDataY := &mockUpdate.TestData{
					Containers: allContainers,
					Staleness:  map[string]bool{"app-x": false, "app-y": true},
					// scope-y succeeds
				}

				clientX := mockUpdate.CreateMockClient(testDataX, false, false)
				clientY := mockUpdate.CreateMockClient(testDataY, false, false)

				// Run concurrent operations
				var wg sync.WaitGroup

				results := make(chan *metrics.Metric, 2)

				wg.Add(2)

				go func() {
					defer wg.Done()

					params := RunUpdatesWithNotificationsParams{
						Logger:                       newTestLogger(t),
						Client:                       clientX,
						Notifier:                     nil,
						NotificationSplitByContainer: false,
						NotificationReport:           false,
						Update: defaultTestUpdateParams(
							filters.FilterByScope(newTestLogger(t), "scope-x", filters.NoFilter),
						),
					}

					metric := RunUpdatesWithNotifications(t.Context(), params)
					results <- metric
				}()

				go func() {
					defer wg.Done()

					params := RunUpdatesWithNotificationsParams{
						Logger:                       newTestLogger(t),
						Client:                       clientY,
						Notifier:                     nil,
						NotificationSplitByContainer: false,
						NotificationReport:           false,
						Update: defaultTestUpdateParams(
							filters.FilterByScope(newTestLogger(t), "scope-y", filters.NoFilter),
						),
					}

					metric := RunUpdatesWithNotifications(t.Context(), params)
					results <- metric
				}()

				wg.Wait()
				close(results)

				// Collect results
				var metrics []*metrics.Metric
				for metric := range results {
					metrics = append(metrics, metric)
				}

				// Verify proper isolation of error reporting
				require.Len(t, metrics, 2)

				totalScanned := 0
				totalFailed := 0
				totalUpdated := 0

				for _, metric := range metrics {
					require.NotNil(t, metric)
					assert.Equal(t, 1, metric.Scanned)
					// Each scope sees 1 container
					totalScanned += metric.Scanned
					totalFailed += metric.Failed
					totalUpdated += metric.Updated
				}

				assert.Equal(t, 2, totalScanned) // Both containers processed
				assert.Equal(t, 1, totalFailed)  // One scope failed
				assert.Equal(t, 1, totalUpdated) // One scope succeeded
			})
		})
	})
}

// Test_newScanMetric checks that each report category reaches its own metric
// count. The categories have distinct sizes to catch a count taken from the
// wrong category. Nil category lists count as zero.
func Test_newScanMetric(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                                         string
		scanned, updated, failed, restarted, skipped []types.ContainerReport
		want                                         *metrics.Metric
	}{
		{
			name:      "empty report",
			scanned:   []types.ContainerReport{},
			updated:   []types.ContainerReport{},
			failed:    []types.ContainerReport{},
			restarted: []types.ContainerReport{},
			skipped:   []types.ContainerReport{},
			want:      &metrics.Metric{},
		},
		{
			name:      "nil categories",
			scanned:   nil,
			updated:   nil,
			failed:    nil,
			restarted: nil,
			skipped:   nil,
			want:      &metrics.Metric{},
		},
		{
			name:      "each category counted",
			scanned:   make([]types.ContainerReport, 10),
			updated:   make([]types.ContainerReport, 3),
			failed:    make([]types.ContainerReport, 2),
			restarted: make([]types.ContainerReport, 4),
			skipped:   make([]types.ContainerReport, 1),
			want:      &metrics.Metric{Scanned: 10, Updated: 3, Failed: 2, Restarted: 4, Skipped: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			report := mockTypes.NewMockReport(t)
			report.EXPECT().Scanned().Return(tt.scanned)
			report.EXPECT().Updated().Return(tt.updated)
			report.EXPECT().Failed().Return(tt.failed)
			report.EXPECT().Restarted().Return(tt.restarted)
			report.EXPECT().Skipped().Return(tt.skipped)

			assert.Equal(t, tt.want, newScanMetric(report))
		})
	}
}

// TestSendSplitNotifications_CooldownSkipped verifies that a container skipped
// during its update cooldown gets its own split notification in both modes,
// and that a container skipped for another reason gets none. The cooldown
// report follows the split report rule: its updated and restarted lists stay
// empty, since those containers get their own notifications.
func TestSendSplitNotifications_CooldownSkipped(t *testing.T) {
	t.Parallel()

	for _, notificationReport := range []bool{true, false} {
		t.Run(fmt.Sprintf("notification report %t", notificationReport), func(t *testing.T) {
			t.Parallel()

			session := reportfixture.Report(
				state.UpdatedState,
				state.RestartedState,
				state.SkippedState,
				state.SkippedState,
			)
			cooldown := session.Skipped()[0]

			status, ok := cooldown.(*state.ContainerStatus)
			require.True(t, ok)
			status.SetCooldownInfo("1 hour", "24 hours", "23 hours", time.Now().Add(23*time.Hour))

			notifier := mockUpdate.NewMockNotifier(t)
			notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(true)

			var sent []*state.SingleContainerReport

			notifier.EXPECT().SendNotification(mock.Anything).Run(func(report types.Report) {
				single, ok := report.(*state.SingleContainerReport)
				require.True(t, ok)

				sent = append(sent, single)
			})

			sendSplitNotifications(newTestLogger(t), notifier, notificationReport, session)

			// One notification each for the updated, restarted and cooldown containers.
			require.Len(t, sent, 3)

			cooldownReport := sent[2]
			assert.Same(t, cooldown, cooldownReport.Focus)
			assert.Empty(t, cooldownReport.UpdatedReports)
			assert.Empty(t, cooldownReport.RestartedReports)
			assert.Equal(t, state.NewSingleContainerReport(cooldown, session), cooldownReport)
		})
	}
}

// TestSendSplitNotifications_OwnNotification verifies that stale and failed containers get their own notification.
//
// The stale container is monitor-only with a newer image. Each row runs in both
// modes. The notification's report is the one NewSingleContainerReport builds
// for that container. The default template lists the container under its own
// state and reports nothing as updated.
func TestSendSplitNotifications_OwnNotification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		// focusOf returns the session category that holds the container.
		focusOf func(types.Report) []types.ContainerReport
		// want returns the lines the rendered notification must contain.
		want func(focus types.ContainerReport) []string
		name string
		// states are the session's containers. Each one gets a notification.
		states []state.State
		// index is the position of the container's notification among the sent ones.
		index int
	}{
		{
			name:    "monitor-only container with a newer image",
			states:  []state.State{state.StaleState},
			focusOf: types.Report.Stale,
			index:   0,
			want: func(focus types.ContainerReport) []string {
				return []string{"0 Updated", "- " + focus.Name() + " (" + focus.ImageName() + "): Stale"}
			},
		},
		{
			name:    "failed container after an updated one",
			states:  []state.State{state.UpdatedState, state.FailedState},
			focusOf: types.Report.Failed,
			index:   1,
			want: func(focus types.ContainerReport) []string {
				return []string{
					"0 Updated",
					"1 Failed",
					"- " + focus.Name() + " (" + focus.ImageName() + "): Failed: " + focus.Error(),
				}
			},
		},
	}

	for _, tt := range tests {
		for _, notificationReport := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s notification report %t", tt.name, notificationReport), func(t *testing.T) {
				t.Parallel()

				log := newTestLogger(t)

				session := reportfixture.Report(tt.states...)
				focus := tt.focusOf(session)[0]

				notifier := mockUpdate.NewMockNotifier(t)
				notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(true).Times(len(tt.states))

				var sent []types.Report

				notifier.EXPECT().SendNotification(mock.Anything).
					Run(func(report types.Report) { sent = append(sent, report) }).Times(len(tt.states))

				sendSplitNotifications(log, notifier, notificationReport, session)

				require.Len(t, sent, len(tt.states))
				assert.Equal(t, state.NewSingleContainerReport(focus, session), sent[tt.index])

				rendered, err := format.Default(false).Render(log, format.Data{Report: sent[tt.index]})
				require.NoError(t, err)
				assert.NotContains(t, rendered, "updated to")

				for _, line := range tt.want(focus) {
					assert.Contains(t, rendered, line)
				}
			})
		}
	}
}
