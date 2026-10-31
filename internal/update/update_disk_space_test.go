package update

import (
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

// TestImageDiskSpaceGate verifies that an update warns about image disk usage
// at the warning threshold, blocks at the maximum, and fails closed when the
// usage cannot be read.
func TestImageDiskSpaceGate(t *testing.T) {
	t.Parallel()

	t.Run("skips the check when both thresholds are unset", func(t *testing.T) {
		t.Parallel()

		client := mockUpdate.CreateMockClient(&mockUpdate.TestData{}, false, false)

		_, _, err := update(
			newTestLogger(t),
			t.Context(),
			client,
			defaultTestUpdateParams(filters.NoFilter),
			ratelimit.New(),
		)
		require.NoError(t, err)
		assert.Equal(t, int32(0), client.TestData.GetImageDiskUsageCount.Load())
		assert.Positive(t, client.TestData.ListContainersCount.Load())
	})

	t.Run("continues when usage is below the warning threshold", func(t *testing.T) {
		t.Parallel()

		client := mockUpdate.CreateMockClient(&mockUpdate.TestData{
			ImageDiskUsage: types.ImageDiskUsage{TotalSize: 1_000, TotalCount: 1},
		}, false, false)

		params := defaultTestUpdateParams(filters.NoFilter)
		params.DiskSpaceMax = 10_000
		params.DiskSpaceWarn = 8_000

		_, _, err := update(newTestLogger(t), t.Context(), client, params, ratelimit.New())
		require.NoError(t, err)
		assert.Equal(t, int32(1), client.TestData.GetImageDiskUsageCount.Load())
		assert.Positive(t, client.TestData.ListContainersCount.Load())
	})

	t.Run("warns and continues when usage reaches the warning threshold", func(t *testing.T) {
		t.Parallel()

		client := mockUpdate.CreateMockClient(&mockUpdate.TestData{
			ImageDiskUsage: types.ImageDiskUsage{TotalSize: 8_000, TotalCount: 2},
		}, false, false)

		params := defaultTestUpdateParams(filters.NoFilter)
		params.DiskSpaceMax = 10_000
		params.DiskSpaceWarn = 8_000

		log, buf := logcapture.NewJSON(zerolog.DebugLevel)

		_, _, err := update(log, t.Context(), client, params, ratelimit.New())
		require.NoError(t, err)
		assert.Contains(t, logcapture.Messages(t, buf.String(), "warn"), msgImageDiskSpaceWarn)
		assert.Positive(t, client.TestData.ListContainersCount.Load())
	})

	t.Run("allows warn-only mode without blocking", func(t *testing.T) {
		t.Parallel()

		client := mockUpdate.CreateMockClient(&mockUpdate.TestData{
			ImageDiskUsage: types.ImageDiskUsage{TotalSize: 50_000, TotalCount: 3},
		}, false, false)

		params := defaultTestUpdateParams(filters.NoFilter)
		params.DiskSpaceWarn = 1_000

		log, buf := logcapture.NewJSON(zerolog.DebugLevel)

		_, _, err := update(log, t.Context(), client, params, ratelimit.New())
		require.NoError(t, err)
		assert.Contains(t, logcapture.Messages(t, buf.String(), "warn"), msgImageDiskSpaceWarn)
		assert.Positive(t, client.TestData.ListContainersCount.Load())
	})

	t.Run("blocks the session when usage reaches the maximum", func(t *testing.T) {
		t.Parallel()

		client := mockUpdate.CreateMockClient(&mockUpdate.TestData{
			ImageDiskUsage: types.ImageDiskUsage{
				TotalSize:   10_000,
				Reclaimable: 2_000,
				TotalCount:  4,
			},
		}, false, false)

		params := defaultTestUpdateParams(filters.NoFilter)
		params.DiskSpaceMax = 10_000
		params.DiskSpaceWarn = 8_000

		_, _, err := update(newTestLogger(t), t.Context(), client, params, ratelimit.New())
		require.ErrorIs(t, err, errImageDiskSpaceExceeded)
		assert.Equal(t, int32(0), client.TestData.ListContainersCount.Load())
	})

	t.Run("fails closed when image disk usage cannot be queried", func(t *testing.T) {
		t.Parallel()

		client := mockUpdate.CreateMockClient(&mockUpdate.TestData{
			GetImageDiskUsageError: errors.New("df unavailable"),
		}, false, false)

		params := defaultTestUpdateParams(filters.NoFilter)
		params.DiskSpaceMax = 10_000

		_, _, err := update(newTestLogger(t), t.Context(), client, params, ratelimit.New())
		require.ErrorIs(t, err, errImageDiskUsageFailed)
		assert.Equal(t, int32(0), client.TestData.ListContainersCount.Load())
	})
}

// TestRunUpdatesWithNotifications_DiskSpaceAbortNotifiesOnce verifies that a
// session ended by the image disk-space check reaches notifications once. Of
// the error lines logged, only the check's own line, with its sizes or cause,
// lacks notify=no, so it is the one entry the notifier queues. The session's
// "Update execution failed" line for the same error is logged with notify=no,
// and the session's batch is still sent.
func TestRunUpdatesWithNotifications_DiskSpaceAbortNotifiesOnce(t *testing.T) {
	t.Parallel()

	// Each case is one way the check ends the session, with the message of the
	// check's own error line.
	tests := []struct {
		name    string
		data    *mockUpdate.TestData
		message string
	}{
		{
			name: "usage reaches the maximum",
			data: &mockUpdate.TestData{
				ImageDiskUsage: types.ImageDiskUsage{TotalSize: 10_000, Reclaimable: 2_000, TotalCount: 4},
			},
			message: msgImageDiskSpaceExceeded,
		},
		{
			name: "usage cannot be queried",
			data: &mockUpdate.TestData{
				GetImageDiskUsageError: errors.New("df unavailable"),
			},
			message: "Failed to query Docker image disk usage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log, buf := logcapture.NewJSON(zerolog.DebugLevel)

			params := defaultTestUpdateParams(filters.NoFilter)
			params.DiskSpaceMax = 10_000

			notifier := mockTypes.NewMockNotifier(t)
			notifier.EXPECT().StartNotification(false).Once()
			notifier.EXPECT().SendNotification(nil).Once()

			metric := RunUpdatesWithNotifications(t.Context(), RunUpdatesWithNotificationsParams{
				Logger:   log,
				Client:   mockUpdate.CreateMockClient(tt.data, false, false),
				Notifier: notifier,
				Update:   params,
			})
			assert.True(t, metric.ScanFailed)

			// Every error line is listed in order, so a repeated message counts
			// once per line.
			var eligible, suppressed []string

			for _, entry := range logcapture.Entries[logcapture.Entry](t, buf.String()) {
				if entry["level"] != "error" {
					continue
				}

				message, _ := entry["message"].(string)
				if entry["notify"] == "no" {
					suppressed = append(suppressed, message)
				} else {
					eligible = append(eligible, message)
				}
			}

			assert.Equal(t, []string{tt.message}, eligible)
			assert.Equal(t, []string{"Update execution failed"}, suppressed)
		})
	}
}
