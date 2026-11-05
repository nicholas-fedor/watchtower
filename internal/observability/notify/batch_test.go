package notify

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
	"github.com/nicholas-fedor/watchtower/internal/testutil/reportfixture"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TestNotifier_FlushBatching verifies that Flush sends a batch only when it
// holds at least one message.
func TestNotifier_FlushBatching(t *testing.T) {
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
			// With the worker running, a message that Flush sent would reach the log.
			shoutrrr.RegisterHook(log)
			shoutrrr.StartNotification(false)
			shoutrrr.Flush()
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
			shoutrrr.Flush()
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
// queued entries. The report also lists the session's failed and stale
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
			name:      "one failed container in the session",
			context:   state.FailedState,
			contextOf: types.Report.Failed,
		},
		{
			name:      "one stale container in the session",
			context:   state.StaleState,
			contextOf: types.Report.Stale,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session := reportfixture.Report(tt.context, state.SkippedState)
			skipped := session.Skipped()[0]

			require.Len(t, tt.contextOf(session), 1)
			other := tt.contextOf(session)[0]

			report := state.NewSingleContainerReport(skipped, session)

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

// TestNotifier_Flush verifies that Flush sends every queued entry in one
// notification without a report and empties the queue. Two containers that
// share an image each log "Found new image". The notification carries that
// message once.
func TestNotifier_Flush(t *testing.T) {
	t.Parallel()

	foundNewImage := map[string]any{"image": "mock/app:latest", "new_id": "sha256:new"}

	n := newBatchingNotifier(t, []*format.Entry{
		{Message: logging.FoundNewImage, Data: withContainer(foundNewImage, "app1")},
		{Message: logging.FoundNewImage, Data: withContainer(foundNewImage, "app2")},
		{Message: logging.StoppingContainer, Data: map[string]any{"container": "app1"}},
	})

	n.Flush()

	n.entriesMutex.RLock()
	remaining := n.entries
	n.entriesMutex.RUnlock()

	// A nil queue ends batching, so later messages are sent as they are logged.
	assert.Nil(t, remaining)

	sent := sentMessages(n)
	assert.Equal(t, []string{
		"Found new image: mock/app:latest (sha256:new)\nStopped stale container: app1 (unknown)",
	}, sent)
}

// TestNotifier_FlushEmptyQueue verifies that Flush sends nothing when nothing
// is queued, with every built-in template, and still ends batching. The JSON
// and porcelain templates render a document even without entries. Flush would
// otherwise send it as an empty notification.
func TestNotifier_FlushEmptyQueue(t *testing.T) {
	t.Parallel()

	for _, tpl := range []string{"", "json.v1", "porcelain.json", "porcelain.v1.summary-no-log"} {
		for _, legacy := range []bool{true, false} {
			t.Run(fmt.Sprintf("%q legacy %t", tpl, legacy), func(t *testing.T) {
				t.Parallel()

				n := createNotifier(newTestLogger(t), nil, nil, zerolog.ErrorLevel, tpl, legacy, format.StaticData{}, 0)
				t.Cleanup(n.Close)

				n.StartNotification(false)
				n.Flush()

				n.entriesMutex.RLock()
				remaining := n.entries
				n.entriesMutex.RUnlock()

				assert.Nil(t, remaining)
				assert.Empty(t, sentMessages(n))
			})
		}
	}
}

// TestNotifier_Discard verifies that Discard empties the queue without sending
// it and ends batching. A line logged afterwards is sent at once instead of
// waiting in the queue.
func TestNotifier_Discard(t *testing.T) {
	t.Parallel()

	n := newBatchingNotifier(t, []*format.Entry{
		{Message: logging.RemovingImage, Data: map[string]any{"image_id": "01d111111111"}},
	})

	n.Discard()

	n.entriesMutex.RLock()
	remaining := n.entries
	n.entriesMutex.RUnlock()

	assert.Nil(t, remaining)
	assert.Empty(t, sentMessages(n))

	hooked := zerolog.New(io.Discard).Hook(n)
	hooked.Warn().Msg("Logged after the session")

	assert.Equal(t, []string{"Logged after the session"}, sentMessages(n))
}

// TestSendNotification_FailedFocusCarriesFailure verifies that a failure line travels with its split notification.
//
// With the legacy template the notification names the container and image and
// gives the failure's cause.
func TestSendNotification_FailedFocusCarriesFailure(t *testing.T) {
	t.Parallel()

	session := reportfixture.Report(state.FailedState)
	failed := session.Failed()[0]

	n := newBatchingNotifier(t, []*format.Entry{{
		Message: logging.ContainerUpdateFailed,
		Data: map[string]any{
			"container": failed.Name(),
			"image":     failed.ImageName(),
			"error":     "pre-update command failed: exit code 1",
		},
	}})

	n.SendNotification(state.NewSingleContainerReport(failed, session))

	assert.Equal(t, []string{
		"Update failed for " + failed.Name() + " (" + failed.ImageName() + "): pre-update command failed: exit code 1",
	}, sentMessages(n))
}
