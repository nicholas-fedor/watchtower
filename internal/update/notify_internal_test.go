package update

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
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

const (
	currentWatchtowerID = "current-watchtower-id"
	otherWatchtowerID   = "other-watchtower-id"
)

// Test_handleUpdateResult verifies the metric and notification an update
// result produces, with and without an error or a partial report.
func Test_handleUpdateResult(t *testing.T) {
	t.Parallel()

	t.Run("should return a failed zero metric when error is not nil and there is no report", func(t *testing.T) {
		t.Parallel()

		err := errors.New("test error")
		result := handleUpdateResult(newTestLogger(t), nil, err, nil)
		assert.Equal(t, &metrics.Metric{ScanFailed: true}, result)
	})

	t.Run("should return the partial report's counts, marked failed, when error is not nil", func(t *testing.T) {
		t.Parallel()

		mockReport := mockTypes.NewMockReport(t)
		mockReport.EXPECT().Scanned().Return(make([]types.ContainerReport, 3))
		mockReport.EXPECT().Updated().Return(make([]types.ContainerReport, 1))
		mockReport.EXPECT().Failed().Return(make([]types.ContainerReport, 2))
		mockReport.EXPECT().Restarted().Return(nil)
		mockReport.EXPECT().Skipped().Return(nil)

		err := errors.New("rolling restart canceled")
		result := handleUpdateResult(newTestLogger(t), mockReport, err, nil)
		assert.Equal(t, &metrics.Metric{Scanned: 3, Updated: 1, Failed: 2, ScanFailed: true}, result)
	})

	t.Run("should return a failed zero metric when result is nil", func(t *testing.T) {
		t.Parallel()

		var err error

		result := handleUpdateResult(newTestLogger(t), nil, err, nil)
		assert.Equal(t, &metrics.Metric{ScanFailed: true}, result)
	})

	t.Run("should return nil when result is not nil and error is nil", func(t *testing.T) {
		t.Parallel()

		mockReport := mockTypes.NewMockReport(t)

		var err error

		result := handleUpdateResult(newTestLogger(t), mockReport, err, nil)
		assert.Nil(t, result)
	})

	t.Run("should send an empty report when error occurs without a report", func(t *testing.T) {
		t.Parallel()

		// Create a mock notifier that tracks if SendNotification was called
		mockNotifier := mockTypes.NewMockNotifier(t)
		mockNotifier.EXPECT().SendNotification(emptyReport{}).Times(1)

		// Call handleUpdateResult with an error and the mock notifier
		err := errors.New("dependency resolution error")
		result := handleUpdateResult(newTestLogger(t), nil, err, mockNotifier)

		// Verify we got the expected metric
		assert.Equal(t, &metrics.Metric{ScanFailed: true}, result)
	})

	t.Run("should send the partial report when error occurs after some work", func(t *testing.T) {
		t.Parallel()

		mockReport := mockTypes.NewMockReport(t)
		mockReport.EXPECT().Scanned().Return(nil)
		mockReport.EXPECT().Updated().Return(nil)
		mockReport.EXPECT().Failed().Return(nil)
		mockReport.EXPECT().Restarted().Return(nil)
		mockReport.EXPECT().Skipped().Return(nil)

		mockNotifier := mockTypes.NewMockNotifier(t)
		mockNotifier.EXPECT().SendNotification(mockReport).Times(1)

		err := errors.New("rolling restart canceled")
		handleUpdateResult(newTestLogger(t), mockReport, err, mockNotifier)
	})

	t.Run("should not send notification when error occurs and notifier is nil", func(t *testing.T) {
		t.Parallel()

		// Call handleUpdateResult with an error and nil notifier
		err := errors.New("dependency resolution error")
		result := handleUpdateResult(newTestLogger(t), nil, err, nil)

		// Verify we got the expected metric
		assert.Equal(t, &metrics.Metric{ScanFailed: true}, result)
	})

	t.Run("should not send notification when there is no error", func(t *testing.T) {
		t.Parallel()

		// Create a mock notifier with no expectations (will fail if any method is called)
		mockNotifier := mockTypes.NewMockNotifier(t)

		// Call handleUpdateResult without an error
		mockReport := mockTypes.NewMockReport(t)

		var err error

		result := handleUpdateResult(newTestLogger(t), mockReport, err, mockNotifier)

		// Verify we got the expected result
		assert.Nil(t, result)
	})
}

// Test_executeUpdate verifies that an update session runs the update for
// stale containers, honors run-once mode, and sets Watchtower's restart policy.
func Test_executeUpdate(t *testing.T) {
	t.Parallel()

	t.Run("should execute update successfully", func(t *testing.T) {
		t.Parallel()

		client := mockUpdate.CreateMockClient(
			&mockUpdate.TestData{
				Containers: []types.Container{
					mockUpdate.CreateMockContainerWithConfig(
						"test-container",
						"/test-container",
						"test:latest",
						true,
						false,
						time.Now(),
						&dockerContainer.Config{},
					),
				},
				Staleness: map[string]bool{
					"test-container": false,
				},
			},
			false,
			false,
		)
		config := types.UpdateParams{
			Filter: filters.NoFilter,
		}
		report, cleanupInfos, err := executeUpdate(newTestLogger(t),
			t.Context(),
			client,
			config,
			ratelimit.New(),
			nil,
		)
		require.NoError(t, err)
		require.NotNil(t, report)
		require.NotNil(t, cleanupInfos)
	})

	t.Run("should not return error when no containers to update", func(t *testing.T) {
		t.Parallel()

		client := mockUpdate.CreateMockClient(
			&mockUpdate.TestData{},
			false,
			false,
		)
		config := types.UpdateParams{
			Filter: filters.NoFilter,
		}
		report, cleanupInfos, err := executeUpdate(newTestLogger(t),
			t.Context(),
			client,
			config,
			ratelimit.New(),
			nil,
		)
		require.NoError(t, err)
		require.NotNil(t, report)
		require.NotNil(t, cleanupInfos)
	})

	t.Run("should execute update logic for stale containers", func(t *testing.T) {
		t.Parallel()

		client := mockUpdate.CreateMockClient(
			&mockUpdate.TestData{
				Containers: []types.Container{
					mockUpdate.CreateMockContainerWithConfig(
						"test-container",
						"/test-container",
						"test:latest",
						true,
						false,
						time.Now(),
						&dockerContainer.Config{},
					),
				},
				Staleness: map[string]bool{
					"test-container": true,
				},
			},
			false,
			false,
		)
		config := types.UpdateParams{
			Filter: filters.NoFilter,
		}
		report, cleanupInfos, err := executeUpdate(newTestLogger(t),
			t.Context(),
			client,
			config,
			ratelimit.New(),
			nil,
		)
		require.NoError(t, err)
		require.NotNil(t, report)
		require.NotNil(t, cleanupInfos)
		assert.Equal(t, int32(1), client.TestData.StartContainerCount.Load())
	})

	t.Run("should propagate RunOnce mode and skip Watchtower self-update", func(t *testing.T) {
		t.Parallel()

		client := mockUpdate.CreateMockClient(
			&mockUpdate.TestData{
				Containers: []types.Container{
					mockUpdate.CreateMockContainerWithConfig(
						"watchtower",
						"/watchtower",
						"watchtower:latest",
						true,
						false,
						time.Now(),
						&dockerContainer.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower": "true",
							},
						},
					),
				},
				Staleness: map[string]bool{
					"watchtower": true,
				},
			},
			false,
			false,
		)
		config := types.UpdateParams{
			Filter:  filters.NoFilter,
			RunOnce: true,
		}
		report, cleanupInfos, err := executeUpdate(newTestLogger(t),
			t.Context(),
			client,
			config,
			ratelimit.New(),
			nil,
		)
		require.NoError(t, err)
		require.NotNil(t, report)
		require.NotNil(t, cleanupInfos)
		assert.Equal(t, int32(0), client.TestData.RenameContainerCount.Load())
		assert.Equal(t, int32(0), client.TestData.StartContainerCount.Load())
	})

	t.Run("should call SetRestartPolicy for Watchtower restart policy changes", func(t *testing.T) {
		t.Parallel()

		client := mockUpdate.CreateMockClient(
			&mockUpdate.TestData{
				Containers: []types.Container{
					mockUpdate.CreateMockContainerWithConfig(
						"watchtower",
						"/watchtower",
						"watchtower:latest",
						true,
						false,
						time.Now(),
						&dockerContainer.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower": "true",
							},
						},
					),
				},
				Staleness: map[string]bool{
					"watchtower": true,
				},
			},
			false,
			false,
		)
		config := types.UpdateParams{
			Filter: filters.NoFilter,
		}
		report, cleanupInfos, err := executeUpdate(newTestLogger(t),
			t.Context(),
			client,
			config,
			ratelimit.New(),
			nil,
		)
		require.NoError(t, err)
		require.NotNil(t, report)
		require.NotNil(t, cleanupInfos)
		assert.Equal(t, int32(1), client.TestData.SetRestartPolicyCount.Load())
	})
}

// Test_emptyReport verifies that every list of the empty report is nil.
func Test_emptyReport(t *testing.T) {
	t.Parallel()

	t.Run("Scanned() should return nil", func(t *testing.T) {
		t.Parallel()

		report := emptyReport{}
		assert.Nil(t, report.Scanned())
	})

	t.Run("Updated() should return nil", func(t *testing.T) {
		t.Parallel()

		report := emptyReport{}
		assert.Nil(t, report.Updated())
	})

	t.Run("Failed() should return nil", func(t *testing.T) {
		t.Parallel()

		report := emptyReport{}
		assert.Nil(t, report.Failed())
	})

	t.Run("Skipped() should return nil", func(t *testing.T) {
		t.Parallel()

		report := emptyReport{}
		assert.Nil(t, report.Skipped())
	})

	t.Run("Stale() should return nil", func(t *testing.T) {
		t.Parallel()

		report := emptyReport{}
		assert.Nil(t, report.Stale())
	})

	t.Run("Fresh() should return nil", func(t *testing.T) {
		t.Parallel()

		report := emptyReport{}
		assert.Nil(t, report.Fresh())
	})

	t.Run("Restarted() should return nil", func(t *testing.T) {
		t.Parallel()

		report := emptyReport{}
		assert.Nil(t, report.Restarted())
	})

	t.Run("All() should return nil", func(t *testing.T) {
		t.Parallel()

		report := emptyReport{}
		assert.Nil(t, report.All())
	})
}

// parseJSONLogEntries parses zerolog JSON lines from a buffer into field maps.
func parseJSONLogEntries(buf *bytes.Buffer) []map[string]any {
	var entries []map[string]any

	for line := range bytes.SplitSeq(buf.Bytes(), []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var m map[string]any

		err := json.Unmarshal(line, &m)
		if err != nil {
			continue
		}

		entries = append(entries, m)
	}

	return entries
}

// newCaptureLogger returns a JSON zerolog logger writing to a buffer for assertions.
func newCaptureLogger() (*zerolog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	l := zerolog.New(buf).Level(zerolog.DebugLevel).With().Timestamp().Logger()

	return &l, buf
}

// stopContainersTestCase represents a test case for stopContainersInReversedOrder cancellation.
type stopContainersTestCase struct {
	name                string
	numContainers       int
	cancelAtIndex       int    // Index at which to cancel (from end, -1 means no cancellation)
	expectedStopped     int    // Number of containers that should be stopped
	expectedSkipped     int    // Number of containers that should be skipped
	expectedLogMessages int    // Expected number of log messages for skipped containers
	description         string // Human-readable description
}

// rollingRestartTestCase represents a test case for performRollingRestart cancellation.
type rollingRestartTestCase struct {
	name                string
	numContainers       int
	cancelAtIndex       int    // Index at which to cancel (-1 means no cancellation)
	expectedProcessed   int    // Number of containers that should be processed
	expectedSkipped     int    // Number of containers that should be skipped
	expectedLogMessages int    // Expected number of log messages for skipped containers
	description         string // Human-readable description
}

// Test_deduplicateByImageID verifies that cleanup entries keep only the first
// entry for each image ID, in order.
func Test_deduplicateByImageID(t *testing.T) {
	t.Parallel()

	t.Run("should return empty slice for empty input", func(t *testing.T) {
		t.Parallel()

		result := deduplicateByImageID([]types.RemovedImageInfo{})
		assert.Empty(t, result)
	})

	t.Run("should return all entries when all ImageIDs are unique", func(t *testing.T) {
		t.Parallel()

		images := []types.RemovedImageInfo{
			{ImageID: types.ImageID("sha256:aaa"), ContainerName: "container-a"},
			{ImageID: types.ImageID("sha256:bbb"), ContainerName: "container-b"},
		}

		result := deduplicateByImageID(images)
		require.Len(t, result, 2)
		assert.Equal(t, types.ImageID("sha256:aaa"), result[0].ImageID)
		assert.Equal(t, types.ImageID("sha256:bbb"), result[1].ImageID)
	})

	t.Run("should keep only first occurrence when ImageIDs are duplicated", func(t *testing.T) {
		t.Parallel()

		images := []types.RemovedImageInfo{
			{ImageID: types.ImageID("sha256:aaa"), ContainerName: "container-a"},
			{ImageID: types.ImageID("sha256:aaa"), ContainerName: "container-b"},
		}

		result := deduplicateByImageID(images)
		require.Len(t, result, 1)
		// Should keep the first occurrence with container-a
		assert.Equal(t, "container-a", result[0].ContainerName)
	})

	t.Run("should handle mixed unique and duplicate ImageIDs", func(t *testing.T) {
		t.Parallel()

		images := []types.RemovedImageInfo{
			{ImageID: types.ImageID("sha256:aaa"), ContainerName: "container-a"},
			{ImageID: types.ImageID("sha256:bbb"), ContainerName: "container-b"},
			{ImageID: types.ImageID("sha256:aaa"), ContainerName: "container-c"},
			{ImageID: types.ImageID("sha256:ccc"), ContainerName: "container-d"},
		}

		result := deduplicateByImageID(images)
		require.Len(t, result, 3)
		assert.Equal(t, types.ImageID("sha256:aaa"), result[0].ImageID)
		assert.Equal(t, "container-a", result[0].ContainerName)
		assert.Equal(t, types.ImageID("sha256:bbb"), result[1].ImageID)
		assert.Equal(t, types.ImageID("sha256:ccc"), result[2].ImageID)
	})
}
