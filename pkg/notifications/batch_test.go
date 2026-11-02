package notifications

import (
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TestSendNotification_Batching verifies that SendNotification sends a
// batch only when it holds at least one message.
func TestSendNotification_Batching(t *testing.T) {
	t.Parallel()

	t.Run("no messages are queued", func(t *testing.T) {
		t.Parallel()

		t.Run("should not send any notification", func(t *testing.T) {
			t.Parallel()

			log, logBuffer := newBufferLogger()

			shoutrrr := createTestNotifier(
				log,
				[]string{"logger://"},
				allButTrace,
				true,
				StaticData{},
				false,
				time.Duration(0),
			)
			shoutrrr.StartNotification(false)
			shoutrrr.SendNotification(nil)
			shoutrrr.Close()
			assert.NotContains(t, logBuffer.String(), `Shoutrrr:`)
		})
	})
	t.Run("at least one message is queued", func(t *testing.T) {
		t.Parallel()

		t.Run("should send a notification", func(t *testing.T) {
			t.Parallel()

			log, logBuffer := newBufferLogger()

			shoutrrr := createTestNotifier(
				log,
				[]string{"logger://"},
				allButTrace,
				true,
				StaticData{},
				false,
				time.Duration(0),
			)

			shoutrrr.RegisterHook(log)
			defer shoutrrr.Close()

			shoutrrr.StartNotification(false)
			log.Info().Msg("This log message is sponsored by ContainrrrVPN")
			shoutrrr.SendNotification(nil)
			require.Eventually(t, func() bool {
				return strings.Contains(logBuffer.String(), `Shoutrrr: This log message is sponsored by ContainrrrVPN`)
			}, 5*time.Second, 10*time.Millisecond)
		})
	})
}

// TestShouldSendNotification verifies which reports are sent at each
// notification level. At the error level, only a report with failed containers
// is sent.
func TestShouldSendNotification(t *testing.T) {
	t.Parallel()

	t.Run("notification level is error", func(t *testing.T) {
		t.Parallel()

		t.Run("should return true when report has failed containers", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{"logger://"},
				zerolog.ErrorLevel,
				false,
				StaticData{},
				false,
				time.Duration(0),
			)

			mockReport := newProgressReport(state.FailedState)
			result := shoutrrr.ShouldSendNotification(mockReport)
			assert.True(t, result)
		})

		t.Run("should return false when report has no failed containers", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{"logger://"},
				zerolog.ErrorLevel,
				false,
				StaticData{},
				false,
				time.Duration(0),
			)

			mockReport := newProgressReport(state.FreshState)
			result := shoutrrr.ShouldSendNotification(mockReport)
			assert.False(t, result)
		})
	})

	t.Run("notification level is warn", func(t *testing.T) {
		t.Parallel()

		t.Run("should return true regardless of report content", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{"logger://"},
				zerolog.WarnLevel,
				false,
				StaticData{},
				false,
				time.Duration(0),
			)

			mockReport := newProgressReport(state.FreshState)
			result := shoutrrr.ShouldSendNotification(mockReport)
			assert.True(t, result)
		})
	})

	t.Run("notification level is info", func(t *testing.T) {
		t.Parallel()

		t.Run("should return true regardless of report content", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{"logger://"},
				zerolog.InfoLevel,
				false,
				StaticData{},
				false,
				time.Duration(0),
			)

			mockReport := newProgressReport(state.FreshState)
			result := shoutrrr.ShouldSendNotification(mockReport)
			assert.True(t, result)
		})
	})

	t.Run("notification level is debug", func(t *testing.T) {
		t.Parallel()

		t.Run("should return true regardless of report content", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{"logger://"},
				zerolog.DebugLevel,
				false,
				StaticData{},
				false,
				time.Duration(0),
			)

			mockReport := newProgressReport(state.FreshState)
			result := shoutrrr.ShouldSendNotification(mockReport)
			assert.True(t, result)
		})
	})

	t.Run("notification level is trace", func(t *testing.T) {
		t.Parallel()

		t.Run("should return true regardless of report content", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{"logger://"},
				zerolog.TraceLevel,
				false,
				StaticData{},
				false,
				time.Duration(0),
			)

			mockReport := newProgressReport(state.FreshState)
			result := shoutrrr.ShouldSendNotification(mockReport)
			assert.True(t, result)
		})
	})

	t.Run("report is nil", func(t *testing.T) {
		t.Parallel()

		t.Run("should return true", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{"logger://"},
				zerolog.ErrorLevel,
				false,
				StaticData{},
				false,
				time.Duration(0),
			)

			result := shoutrrr.ShouldSendNotification(nil)
			assert.True(t, result)
		})
	})
}

// TestSendNotification_SingleFocusKeepsBatchingActive verifies that when every
// queued entry matches the single-container focus, n.entries remains a non-nil
// empty slice so subsequent Run calls continue batching.
func TestSendNotification_SingleFocusKeepsBatchingActive(t *testing.T) {
	n := createTestNotifier(
		newTestLogger(t),
		[]string{"logger://"},
		zerolog.InfoLevel,
		true,
		StaticData{},
		false,
		0,
	)

	full := newProgressReport(state.UpdatedState)
	require.NotEmpty(t, full.Updated())

	updated := full.Updated()[0]
	report := &state.SingleContainerReport{
		UpdatedReports: []types.ContainerReport{updated},
	}

	n.StartNotification(true)
	n.entriesMutex.Lock()
	n.entries = []*notificationEntry{
		{
			Message: "Found new image",
			Data:    map[string]any{"container": updated.Name()},
		},
	}
	n.entriesMutex.Unlock()

	n.SendNotification(report)

	n.entriesMutex.RLock()
	entries := n.entries
	n.entriesMutex.RUnlock()

	require.NotNil(t, entries, "batching slice must remain non-nil after single-focus drain")
	require.Empty(t, entries)

	n.Close()
}
