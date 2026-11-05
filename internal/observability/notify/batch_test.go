package notify

import (
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
	"github.com/nicholas-fedor/watchtower/internal/testutil/reportfixture"
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
				t,
				log,
				[]string{"logger://"},
				allButTrace,
				true,
				format.StaticData{},
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
				t,
				log,
				[]string{"logger://"},
				allButTrace,
				true,
				format.StaticData{},
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
				t,
				newTestLogger(t),
				[]string{"logger://"},
				zerolog.ErrorLevel,
				false,
				format.StaticData{},
				time.Duration(0),
			)

			mockReport := reportfixture.Report(state.FailedState)
			result := shoutrrr.ShouldSendNotification(mockReport)
			assert.True(t, result)
		})

		t.Run("should return false when report has no failed containers", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				t,
				newTestLogger(t),
				[]string{"logger://"},
				zerolog.ErrorLevel,
				false,
				format.StaticData{},
				time.Duration(0),
			)

			mockReport := reportfixture.Report(state.FreshState)
			result := shoutrrr.ShouldSendNotification(mockReport)
			assert.False(t, result)
		})
	})

	t.Run("notification level is warn", func(t *testing.T) {
		t.Parallel()

		t.Run("should return true regardless of report content", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				t,
				newTestLogger(t),
				[]string{"logger://"},
				zerolog.WarnLevel,
				false,
				format.StaticData{},
				time.Duration(0),
			)

			mockReport := reportfixture.Report(state.FreshState)
			result := shoutrrr.ShouldSendNotification(mockReport)
			assert.True(t, result)
		})
	})

	t.Run("notification level is info", func(t *testing.T) {
		t.Parallel()

		t.Run("should return true regardless of report content", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				t,
				newTestLogger(t),
				[]string{"logger://"},
				zerolog.InfoLevel,
				false,
				format.StaticData{},
				time.Duration(0),
			)

			mockReport := reportfixture.Report(state.FreshState)
			result := shoutrrr.ShouldSendNotification(mockReport)
			assert.True(t, result)
		})
	})

	t.Run("notification level is debug", func(t *testing.T) {
		t.Parallel()

		t.Run("should return true regardless of report content", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				t,
				newTestLogger(t),
				[]string{"logger://"},
				zerolog.DebugLevel,
				false,
				format.StaticData{},
				time.Duration(0),
			)

			mockReport := reportfixture.Report(state.FreshState)
			result := shoutrrr.ShouldSendNotification(mockReport)
			assert.True(t, result)
		})
	})

	t.Run("notification level is trace", func(t *testing.T) {
		t.Parallel()

		t.Run("should return true regardless of report content", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				t,
				newTestLogger(t),
				[]string{"logger://"},
				zerolog.TraceLevel,
				false,
				format.StaticData{},
				time.Duration(0),
			)

			mockReport := reportfixture.Report(state.FreshState)
			result := shoutrrr.ShouldSendNotification(mockReport)
			assert.True(t, result)
		})
	})

	t.Run("report is nil", func(t *testing.T) {
		t.Parallel()

		t.Run("should return true", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				t,
				newTestLogger(t),
				[]string{"logger://"},
				zerolog.ErrorLevel,
				false,
				format.StaticData{},
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
		t,
		newTestLogger(t),
		[]string{"logger://"},
		zerolog.InfoLevel,
		true,
		format.StaticData{},
		0,
	)

	full := reportfixture.Report(state.UpdatedState)
	require.NotEmpty(t, full.Updated())

	updated := full.Updated()[0]
	report := &state.SingleContainerReport{
		Focus:          updated,
		UpdatedReports: []types.ContainerReport{updated},
	}

	n.StartNotification(true)
	n.entriesMutex.Lock()
	n.entries = []*format.Entry{
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

// TestSendNotification_CooldownFocusWithOneContextContainer verifies that a
// split notification for a cooldown-skipped container sends that container's
// queued entries. The report also lists the session's updated and restarted
// containers for context. One of those lists holds exactly one container, which
// must not be taken for the notification's subject.
func TestSendNotification_CooldownFocusWithOneContextContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		context state.State
		// contextOf returns the context container's category in the session.
		contextOf func(types.Report) []types.ContainerReport
	}{
		{
			name:      "one updated container in the session",
			context:   state.UpdatedState,
			contextOf: types.Report.Updated,
		},
		{
			name:      "one restarted container in the session",
			context:   state.RestartedState,
			contextOf: types.Report.Restarted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session := reportfixture.Report(tt.context, state.SkippedState)
			skipped := session.Skipped()[0]

			require.Len(t, tt.contextOf(session), 1)
			other := tt.contextOf(session)[0]

			// The cooldown path names the skipped container as the focus and
			// lists the session's updated and restarted containers as context.
			report := &state.SingleContainerReport{
				Focus:            skipped,
				SkippedReports:   []types.ContainerReport{skipped},
				UpdatedReports:   session.Updated(),
				RestartedReports: session.Restarted(),
			}

			otherEntry := &format.Entry{
				Message: "Found new image",
				Data:    map[string]any{"container": other.Name()},
			}
			n := newBatchingNotifier(t, []*format.Entry{
				otherEntry,
				{
					Message: "Skipping update during cooldown",
					Data:    map[string]any{"container": skipped.Name()},
				},
			})

			n.SendNotification(report)

			n.entriesMutex.RLock()
			remaining := n.entries
			n.entriesMutex.RUnlock()

			assert.Equal(t, []*format.Entry{otherEntry}, remaining,
				"only the skipped container's entry is sent with its notification")

			sent := sentMessages(n)
			require.Len(t, sent, 1)
			assert.Contains(t, sent[0], "Skipping update during cooldown")
			assert.NotContains(t, sent[0], "Found new image")
		})
	}
}
