package update

import (
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
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/reportfixture"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
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

		// The updated and failed containers are each also scanned.
		report := reportfixture.Report(state.UpdatedState, state.FailedState, state.FailedState)

		err := errors.New("rolling restart canceled")
		result := handleUpdateResult(newTestLogger(t), report, err, nil)
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

		var err error

		result := handleUpdateResult(newTestLogger(t), reportfixture.Report(), err, nil)
		assert.Nil(t, result)
	})

	t.Run("should keep the error line out of notifications only when the error is already reported", func(t *testing.T) {
		t.Parallel()

		// Each case ends a session with an error and no report. In both cases the
		// session's end sends the batch, which holds the entry that notifies the
		// cause. The send fails the test when a report is sent here.
		tests := []struct {
			name       string
			err        error
			wantNotify any
		}{
			{name: "plain error", err: errors.New("list failed"), wantNotify: nil},
			{name: "reported error", err: reportedError{err: errors.New("list failed")}, wantNotify: "no"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				log, buf := logcapture.NewJSON(zerolog.DebugLevel)

				handleUpdateResult(log, nil, tt.err, func(*state.Report) {
					t.Error("no report is sent without a report")
				})

				entries := logcapture.Entries[logcapture.Entry](t, buf.String())
				require.Len(t, entries, 1)
				assert.Equal(t, "Update execution failed", entries[0]["message"])
				assert.Equal(t, "list failed", entries[0]["error"])
				assert.Equal(t, tt.wantNotify, entries[0]["notify"])
			})
		}
	})

	t.Run("should send the partial report when error occurs after some work", func(t *testing.T) {
		t.Parallel()

		report := reportfixture.Report()

		var batch sessionBatch

		err := errors.New("rolling restart canceled")
		handleUpdateResult(newTestLogger(t), report, err, batch.record)

		assert.Equal(t, []*state.Report{report}, batch.sent)
	})

	t.Run("should return a failed zero metric when error occurs and notifier is nil", func(t *testing.T) {
		t.Parallel()

		err := errors.New("dependency resolution error")
		result := handleUpdateResult(newTestLogger(t), nil, err, nil)

		assert.Equal(t, &metrics.Metric{ScanFailed: true}, result)
	})

	t.Run("should not send notification when there is no error", func(t *testing.T) {
		t.Parallel()

		// Call handleUpdateResult without an error
		var err error

		result := handleUpdateResult(newTestLogger(t), reportfixture.Report(), err, func(*state.Report) {
			t.Error("no report is sent without an error")
		})

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
		config := updateParams.Params{
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
		config := updateParams.Params{
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
		config := updateParams.Params{
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
		config := updateParams.Params{
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
		config := updateParams.Params{
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
