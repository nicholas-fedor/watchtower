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

	"github.com/nicholas-fedor/watchtower/internal/container"
	mockContainerClient "github.com/nicholas-fedor/watchtower/internal/container/client/mocks"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/container/provenance"
	"github.com/nicholas-fedor/watchtower/internal/observability/events"
	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/reportfixture"
	"github.com/nicholas-fedor/watchtower/internal/update/cleanup"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
)

// defaultTestUpdateParams returns a complete [updateParams.Params] snapshot for tests.
//
// Parameters:
//   - filter: Container filter for the update session.
//
// Returns:
//   - updateParams.Params: The default policy for tests that set none of their own.
func defaultTestUpdateParams(filter filters.Filter) updateParams.Params {
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
	cycleMember := func(name, dependsOn string) *container.Container {
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

	client := mockUpdate.CreateFakeClient(
		&mockUpdate.TestData{
			Containers: []*container.Container{
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

// TestRunUpdatesWithNotifications_StoppingProcessMakesNoUpdate verifies that a
// session started after the process was told to stop does nothing and returns
// no metric, so it is not reported as a failed scan. The client and notifier
// mocks fail on any call.
func TestRunUpdatesWithNotifications_StoppingProcessMakesNoUpdate(t *testing.T) {
	t.Parallel()

	broadcaster := events.NewBroadcaster()
	published, _ := broadcaster.SubscribeWithDone()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	metric := RunUpdatesWithNotifications(ctx, RunUpdatesWithNotificationsParams{
		Logger:           newTestLogger(t),
		Client:           mockContainerClient.NewMockClient(t),
		Notifier:         mockUpdate.NewMockNotifier(t),
		EventBroadcaster: broadcaster,
		Update:           defaultTestUpdateParams(filters.NoFilter),
	})

	assert.Nil(t, metric, "a stopping process returned a scan metric")
	assert.Empty(t, published, "a stopping process published a scan event")
}

// TestRunUpdatesWithNotifications_PassedDeadlineStillRuns verifies that a
// session whose deadline has already passed still runs and reports its scan.
// Only a stopped process cancels its context. A deadline that passes first,
// such as an HTTP API request that waited for the update lock longer than its
// timeout, is a failure the caller should see.
func TestRunUpdatesWithNotifications_PassedDeadlineStillRuns(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()

	metric := RunUpdatesWithNotifications(ctx, RunUpdatesWithNotificationsParams{
		Logger: newTestLogger(t),
		Client: mockUpdate.CreateFakeClient(&mockUpdate.TestData{}, false, false),
		Update: defaultTestUpdateParams(filters.NoFilter),
	})

	require.NotNil(t, metric, "a session past its deadline returned no scan metric")
	assert.True(t, metric.ScanFailed, "a session past its deadline did not report a failed scan")
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
		RunAndReturn(func(context.Context, ...filters.Filter) ([]*container.Container, error) {
			panic("boom")
		}).Once()

	// The session's own end sends its batch, which holds the panic entry,
	// without a report. The mock fails the test on any other notifier call.
	notifier := mockUpdate.NewMockNotifier(t)
	expectSessionBatch(t, notifier)

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

				result := reportfixture.Report()
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

				client := mockUpdate.CreateFakeClient(
					&mockUpdate.TestData{
						Containers: []*container.Container{
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

				client := mockUpdate.CreateFakeClient(
					&mockUpdate.TestData{
						Containers: []*container.Container{
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
				batch := expectSessionBatch(t, notifier)

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
				assert.Len(t, batch.sent, 1)
			})
		})

		t.Run("no containers are provided", func(t *testing.T) {
			t.Parallel()

			t.Run("should return zero metric when container list is empty", func(t *testing.T) {
				t.Parallel()

				client := mockUpdate.CreateFakeClient(
					&mockUpdate.TestData{
						Containers: []*container.Container{},
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

				client := mockUpdate.CreateFakeClient(
					&mockUpdate.TestData{
						Containers: []*container.Container{container1, container2},
						Staleness: map[string]bool{
							"watchtower-1": true,
							"watchtower-2": true,
						},
					},
					false,
					false,
				)

				update := defaultTestUpdateParams(filters.NoFilter)
				update.CurrentContainerID = container.ContainerID("container1-id")

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

			client := mockUpdate.CreateFakeClient(
				&mockUpdate.TestData{
					Containers: []*container.Container{
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

			// The function only logs. The test checks that it handles the report.
			logUpdateReport(newTestLogger(t), reportfixture.Report(state.UpdatedState, state.FreshState))
		})

		t.Run("should handle reports with no updates", func(t *testing.T) {
			t.Parallel()

			logUpdateReport(newTestLogger(t), reportfixture.Report(state.FreshState, state.FreshState))
		})
	})

	t.Run("sendNotifications", func(t *testing.T) {
		t.Parallel()

		t.Run("should not send notifications when notifier is nil", func(t *testing.T) {
			t.Parallel()

			sendNotifications(newTestLogger(t), nil, nil, false, false, reportfixture.Report())
		})

		t.Run("should send grouped notification when split is false", func(t *testing.T) {
			t.Parallel()

			report := reportfixture.Report()
			notifier := mockUpdate.NewMockNotifier(t)
			notifier.EXPECT().ShouldSendNotification(report).Return(true)

			var batch sessionBatch

			sendNotifications(newTestLogger(t), notifier, batch.record, false, false, report)
			assert.Len(t, batch.sent, 1)
		})

		// A report below the notifications level, such as one without failures
		// at the error level, is not sent. The session's end then sends its
		// queued lines without the report. The mock fails the test on any other
		// notifier call.
		t.Run("should leave the batch to the session's end when the report is below the notifications level", func(t *testing.T) {
			t.Parallel()

			report := reportfixture.Report()
			notifier := mockUpdate.NewMockNotifier(t)
			notifier.EXPECT().ShouldSendNotification(report).Return(false)

			var batch sessionBatch

			sendNotifications(newTestLogger(t), notifier, batch.record, false, false, report)
			assert.Empty(t, batch.sent)
		})

		t.Run("should call sendSplitNotifications when split is true", func(t *testing.T) {
			t.Parallel()

			notifier := mockUpdate.NewMockNotifier(t)

			var batch sessionBatch

			sendNotifications(newTestLogger(t), notifier, batch.record, true, false, reportfixture.Report())
			assert.Empty(t, batch.sent)
		})
	})

	t.Run("sendSplitNotifications", func(t *testing.T) {
		t.Parallel()

		// Each updated or restarted container gets one notification in both
		// notification modes. An empty session sends none.
		tests := []struct {
			name               string
			states             []state.State
			wantSent           int
			notificationReport bool
		}{
			{name: "updated container in report mode", states: []state.State{state.UpdatedState}, wantSent: 1, notificationReport: true},
			{name: "updated container in entry mode", states: []state.State{state.UpdatedState}, wantSent: 1, notificationReport: false},
			{name: "restarted container in report mode", states: []state.State{state.RestartedState}, wantSent: 1, notificationReport: true},
			{name: "restarted container in entry mode", states: []state.State{state.RestartedState}, wantSent: 1, notificationReport: false},
			{name: "empty session", states: nil, wantSent: 0, notificationReport: true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				var batch sessionBatch

				sendSplitNotifications(newTestLogger(t), batch.record, tt.notificationReport, reportfixture.Report(tt.states...))
				assert.Len(t, batch.sent, tt.wantSent)
			})
		}

		t.Run("should skip containers with empty names", func(t *testing.T) {
			t.Parallel()

			unnamed := modelfixture.Container(containerfixture.WithName(""))
			session := reportfixture.UpdatedReport(unnamed, "new_image_id", provenance.ReportMeta{})
			require.Len(t, session.Updated(), 1)

			var batch sessionBatch

			sendSplitNotifications(newTestLogger(t), batch.record, true, session)
			assert.Empty(t, batch.sent)
		})
	})

	t.Run("notification level filtering", func(t *testing.T) {
		t.Parallel()

		t.Run("when notification level is error", func(t *testing.T) {
			t.Parallel()

			t.Run("should not send notifications when no errors occur", func(t *testing.T) {
				t.Parallel()

				notifier := mockUpdate.NewMockNotifier(t)
				notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(false)
				// With notification level set to error and no errors, no notification should be sent

				var batch sessionBatch

				sendNotifications(newTestLogger(t), notifier, batch.record, true, true, reportfixture.Report(state.UpdatedState))
				assert.Empty(t, batch.sent)
			})
		})
	})

	t.Run("performImageCleanup", func(t *testing.T) {
		t.Parallel()

		t.Run("should return empty slice when cleanup is disabled", func(t *testing.T) {
			t.Parallel()

			client := mockUpdate.CreateFakeClient(&mockUpdate.TestData{}, false, false)
			cleanedImages := performImageCleanup(newTestLogger(t), t.Context(), client, false, []cleanup.RemovedImageInfo{}, time.Minute)
			assert.Empty(t, cleanedImages)
		})

		t.Run("should perform cleanup when cleanup is enabled", func(t *testing.T) {
			t.Parallel()

			client := mockUpdate.CreateFakeClient(&mockUpdate.TestData{}, false, false)
			cleanedImages := performImageCleanup(newTestLogger(t), t.Context(), client, true, []cleanup.RemovedImageInfo{
				{
					ContainerName: "test-container",
					ImageName:     "test-image:v1.0",
					ImageID:       container.ImageID("sha256:123"),
				},
			}, time.Minute)
			// The function should return the cleaned images when cleanup is enabled
			require.Len(t, cleanedImages, 1)
			assert.Equal(t, "test-container", cleanedImages[0].ContainerName)
		})

		t.Run("should return a valid slice when cleanup input is empty", func(t *testing.T) {
			t.Parallel()

			client := mockUpdate.CreateFakeClient(&mockUpdate.TestData{}, false, false)
			cleanedImages := performImageCleanup(newTestLogger(t), t.Context(), client, true, []cleanup.RemovedImageInfo{}, time.Minute)
			// Should return a valid slice even with empty input
			require.NotNil(t, cleanedImages)
		})

		t.Run("should still remove images when the parent context is already canceled", func(t *testing.T) {
			t.Parallel()

			client := mockUpdate.CreateFakeClient(&mockUpdate.TestData{}, false, false)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			cleanedImages := performImageCleanup(newTestLogger(t), ctx, client, true, []cleanup.RemovedImageInfo{
				{
					ContainerName: "test-container",
					ImageName:     "test-image:v1.0",
					ImageID:       container.ImageID("sha256:123"),
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

			log, buf := logcapture.NewJSON(zerolog.DebugLevel)
			_ = generateAndLogMetric(log, reportfixture.Report())

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

			report := reportfixture.Report(
				state.FreshState, state.FreshState, state.FreshState, state.SkippedState, state.SkippedState,
			)

			log, buf := logcapture.NewJSON(zerolog.DebugLevel)
			metric := generateAndLogMetric(log, report)

			assert.Equal(t, 2, metric.Skipped)
			assert.Equal(t, 3, metric.Scanned)

			var found bool

			for _, entry := range logcapture.Entries[logcapture.Entry](t, buf.String()) {
				if entry["message"] != "Update session completed" {
					continue
				}

				found = true

				assert.EqualValues(t, 2, entry["skipped"])
				assert.EqualValues(t, 3, entry["scanned"])
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
			containersA := []*container.Container{scopeAContainer}
			containersB := []*container.Container{scopeBContainer}

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

			// Create a fake client for each scope
			clientA := mockUpdate.CreateFakeClient(testDataA, false, false)
			clientB := mockUpdate.CreateFakeClient(testDataB, false, false)

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

			allContainers := []*container.Container{scope1Container, scope2Container}

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

			client1 := mockUpdate.CreateFakeClient(testData1, false, false)
			client2 := mockUpdate.CreateFakeClient(testData2, false, false)

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

			allContainers := []*container.Container{scopeAWatchtower, scopeBWatchtower}

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

			clientA := mockUpdate.CreateFakeClient(testDataA, false, false)
			clientB := mockUpdate.CreateFakeClient(testDataB, false, false)

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

			allContainers := []*container.Container{appContainer, watchtowerProd, watchtowerDev}

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

			clientProd := mockUpdate.CreateFakeClient(testDataProd, false, false)
			clientDev := mockUpdate.CreateFakeClient(testDataDev, false, false)

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

				allContainers := []*container.Container{scopeAContainer1, scopeAContainer2}

				// Set up mock to fail updates
				testData := &mockUpdate.TestData{
					Containers: allContainers,
					Staleness:  map[string]bool{"app-a-1": true, "app-a-2": true},
					StopContainerError: errors.New(
						"container stop failed",
					), // Fail during update process
					StopContainerFailCount: 2, // Fail both containers
				}

				client := mockUpdate.CreateFakeClient(testData, false, false)

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

				allContainers := []*container.Container{scopeAContainer, scopeBContainer}

				// Different test data for different scopes
				testDataA := &mockUpdate.TestData{
					Containers: allContainers,
					Staleness: map[string]bool{
						"cleanup-app-a": true,
						"cleanup-app-b": false,
					},
					RemoveImageError: errors.New("cleanup failed in scope-a"),
					FailedImageIDs: []container.ImageID{
						container.ImageID("app:v1"),
					}, // Fail cleanup for scope-a
				}

				testDataB := &mockUpdate.TestData{
					Containers: allContainers,
					Staleness:  map[string]bool{"cleanup-app-a": false, "cleanup-app-b": true},
					// scope-b cleanup succeeds
				}

				clientA := mockUpdate.CreateFakeClient(testDataA, false, false)
				clientB := mockUpdate.CreateFakeClient(testDataB, false, false)

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

				allContainers := []*container.Container{containerX, containerY}

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

				clientX := mockUpdate.CreateFakeClient(testDataX, false, false)
				clientY := mockUpdate.CreateFakeClient(testDataY, false, false)

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
// wrong category. Every container except the skipped one is also scanned. A
// report without containers counts zero.
func Test_newScanMetric(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want   *metrics.Metric
		name   string
		states []state.State
	}{
		{name: "empty report", states: nil, want: &metrics.Metric{}},
		{
			name: "each category counted",
			states: []state.State{
				state.UpdatedState, state.UpdatedState, state.UpdatedState,
				state.FailedState, state.FailedState,
				state.RestartedState, state.RestartedState, state.RestartedState, state.RestartedState,
				state.SkippedState,
				state.FreshState,
			},
			want: &metrics.Metric{Scanned: 10, Updated: 3, Failed: 2, Restarted: 4, Skipped: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, newScanMetric(reportfixture.Report(tt.states...)))
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

			log := newTestLogger(t)
			progress := state.Progress{}

			updated, newImage := reportfixture.Container(0, state.UpdatedState)
			progress.AddScanned(log, updated, newImage, updateParams.Params{})
			progress.MarkForUpdate(log, updated.ID())

			restarted, _ := reportfixture.Container(0, state.RestartedState)
			progress.AddScanned(log, restarted, restarted.ImageID(), updateParams.Params{})
			progress.MarkRestarted(log, restarted.ID())

			// One skipped container waits out its update cooldown. The other
			// was skipped for another reason.
			inCooldown, _ := reportfixture.Container(0, state.SkippedState)
			progress.AddSkipped(log, inCooldown, errors.New("in cooldown"), updateParams.Params{})
			progress.SetCooldownInfo(log, inCooldown.ID(), "1 hour", "24 hours", "23 hours", time.Now().Add(23*time.Hour))

			otherSkipped, _ := reportfixture.Container(1, state.SkippedState)
			progress.AddSkipped(log, otherSkipped, errors.New("skipped"), updateParams.Params{})

			session := progress.Report(log)
			cooldown := progress[inCooldown.ID()]

			var batch sessionBatch

			sendSplitNotifications(log, batch.record, notificationReport, session)

			// One notification each for the updated, restarted and cooldown containers.
			require.Len(t, batch.sent, 3)

			cooldownReport := batch.sent[2]
			assert.Same(t, cooldown, cooldownReport.Focus())
			assert.Empty(t, cooldownReport.Updated())
			assert.Empty(t, cooldownReport.Restarted())
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
		focusOf func(*state.Report) []*state.ContainerStatus
		// want returns the lines the rendered notification must contain.
		want func(focus *state.ContainerStatus) []string
		name string
		// states are the session's containers. Each one gets a notification.
		states []state.State
		// index is the position of the container's notification among the sent ones.
		index int
	}{
		{
			name:    "monitor-only container with a newer image",
			states:  []state.State{state.StaleState},
			focusOf: (*state.Report).Stale,
			index:   0,
			want: func(focus *state.ContainerStatus) []string {
				return []string{"0 Updated", "- " + focus.Name() + " (" + focus.ImageName() + "): Stale"}
			},
		},
		{
			name:    "failed container after an updated one",
			states:  []state.State{state.UpdatedState, state.FailedState},
			focusOf: (*state.Report).Failed,
			index:   1,
			want: func(focus *state.ContainerStatus) []string {
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

				var batch sessionBatch

				sendSplitNotifications(log, batch.record, notificationReport, session)

				require.Len(t, batch.sent, len(tt.states))
				assert.Equal(t, state.NewSingleContainerReport(focus, session), batch.sent[tt.index])

				rendered, err := format.Default(false).Render(log, format.Data{Report: batch.sent[tt.index]})
				require.NoError(t, err)
				assert.NotContains(t, rendered, "updated to")

				for _, line := range tt.want(focus) {
					assert.Contains(t, rendered, line)
				}
			})
		}
	}
}
