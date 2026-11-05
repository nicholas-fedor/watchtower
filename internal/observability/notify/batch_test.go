package notify

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
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
			shoutrrr.StartNotification()
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

			shoutrrr.StartNotification()
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

// TestSendNotification_SingleFocusKeepsBatchingActive verifies that a split
// notification that takes every queued entry leaves batching on, since the
// session's other split notifications follow.
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

	n.StartNotification()
	n.entriesMutex.Lock()
	n.batch.entries = []*format.Entry{
		{
			Message: "Found new image",
			Data:    map[string]any{"container": updated.Name()},
		},
	}
	n.entriesMutex.Unlock()

	n.SendNotification(report)

	n.entriesMutex.RLock()
	open := n.batch
	n.entriesMutex.RUnlock()

	require.NotNil(t, open, "the batch stays open after a split notification")
	assert.Empty(t, open.entries)

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
			remaining := n.batch.entries
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
	open := n.batch
	n.entriesMutex.RUnlock()

	// No batch is open. Later messages are sent as they are logged.
	assert.Nil(t, open)

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

				n.StartNotification()
				n.Flush()

				n.entriesMutex.RLock()
				open := n.batch
				n.entriesMutex.RUnlock()

				assert.Nil(t, open)
				assert.Empty(t, sentMessages(n))
			})
		}
	}
}

// TestNotifier_StartNotification_End verifies that ending a session's batch
// drops the entries no notification took and ends batching. A line logged
// afterwards is sent at once instead of waiting in the queue.
func TestNotifier_StartNotification_End(t *testing.T) {
	t.Parallel()

	n, hooked := newIdleNotifier(t)

	end := n.StartNotification()

	hooked.Warn().Str("image_id", "01d111111111").Msg(logging.RemovingImage)

	end()

	n.entriesMutex.RLock()
	open := n.batch
	n.entriesMutex.RUnlock()

	assert.Nil(t, open)
	assert.Empty(t, sentMessages(n))

	hooked.Warn().Msg("Logged after the session")

	assert.Equal(t, []string{"Logged after the session"}, sentMessages(n))
}

// newIdleNotifier returns a notifier that is not batching and a logger hooked
// to it. A line logged through that logger is sent at once.
func newIdleNotifier(t *testing.T) (*Notifier, zerolog.Logger) {
	t.Helper()

	n := newBatchingNotifier(t, nil)
	n.batch = nil

	return n, zerolog.New(io.Discard).Hook(n)
}

// TestNotifier_StartNotification verifies that starting a batch first sends
// the lines an earlier owner queued, without a report. Those are either the
// lines logged before startup or the lines of a check whose batch is taken
// over.
func TestNotifier_StartNotification(t *testing.T) {
	t.Parallel()

	n := newBatchingNotifier(t, nil)
	hooked := zerolog.New(io.Discard).Hook(n)

	// The notifier batches from its creation until the startup message.
	hooked.Warn().Msg("Logged before startup")
	require.Empty(t, sentMessages(n))

	n.StartNotification()
	assert.Equal(t, []string{"Logged before startup"}, sentMessages(n))

	hooked.Warn().Msg("Logged during startup")
	assert.Empty(t, sentMessages(n))

	n.Flush()
	assert.Equal(t, []string{"Logged during startup"}, sentMessages(n))
}

// TestNotifier_HoldNotifications verifies how a check request's batch
// overlaps the batches of update sessions and other checks. Overlapping checks
// share one batch. Each check's release sends the lines queued so far. The
// last one closes the batch. A check during a session joins the session's
// batch. An update session that starts during a check
// takes the batch over and first sends the check's queued lines. Every line is
// sent exactly once.
func TestNotifier_HoldNotifications(t *testing.T) {
	t.Parallel()

	tests := []struct {
		// run logs the lines and drives the batches.
		run  func(n *Notifier, hooked zerolog.Logger) []string
		name string
		// want is every message sent, in order.
		want []string
	}{
		{
			name: "check sends its lines when it ends",
			run: func(n *Notifier, hooked zerolog.Logger) []string {
				release := n.HoldNotifications()

				hooked.Warn().Msg("Check line")

				queued := sentMessages(n)

				release()
				hooked.Warn().Msg("After the check")

				return append(queued, sentMessages(n)...)
			},
			want: []string{"Check line", "After the check"},
		},
		{
			name: "check sends one notification per container when split",
			run: func(n *Notifier, hooked zerolog.Logger) []string {
				n.splitByContainer = true
				release := n.HoldNotifications()

				hooked.Warn().Str("container", "app2").Msg("Check line")
				hooked.Warn().Str("container", "app1").Msg("Check line")

				release()

				// Containers are sent in map order.
				sent := sentMessages(n)
				slices.Sort(sent)

				return sent
			},
			want: []string{"Check line | container=app1 ", "Check line | container=app2 "},
		},
		{
			name: "check during a session joins its batch",
			run: func(n *Notifier, hooked zerolog.Logger) []string {
				n.StartNotification()

				release := n.HoldNotifications()

				hooked.Warn().Msg("Check line")
				release()

				queued := sentMessages(n)

				hooked.Warn().Msg("Session line")
				n.Flush()

				return append(queued, sentMessages(n)...)
			},
			want: []string{"Check line\nSession line"},
		},
		{
			name: "session starting during a check takes its batch over",
			run: func(n *Notifier, hooked zerolog.Logger) []string {
				release := n.HoldNotifications()

				hooked.Warn().Msg("Check line before the session")

				n.StartNotification()
				hooked.Warn().Msg("Line during the session")
				release()

				queued := sentMessages(n)

				n.Flush()

				return append(queued, sentMessages(n)...)
			},
			want: []string{"Check line before the session", "Line during the session"},
		},
		{
			name: "check after a session's send keeps its batch when the session ends",
			run: func(n *Notifier, hooked zerolog.Logger) []string {
				end := n.StartNotification()

				hooked.Warn().Msg("Session line")
				n.Flush()

				release := n.HoldNotifications()

				hooked.Warn().Msg("Check line")

				end()
				release()

				return sentMessages(n)
			},
			want: []string{"Session line", "Check line"},
		},
		{
			name: "second check joins the first",
			run: func(n *Notifier, hooked zerolog.Logger) []string {
				first := n.HoldNotifications()
				second := n.HoldNotifications()

				hooked.Warn().Msg("Check line")

				second()

				queued := sentMessages(n)

				first()

				return append(queued, sentMessages(n)...)
			},
			want: []string{"Check line"},
		},
		{
			name: "each overlapping check sends the lines queued when it ends",
			run: func(n *Notifier, hooked zerolog.Logger) []string {
				first := n.HoldNotifications()
				second := n.HoldNotifications()

				hooked.Warn().Msg("First check line")
				first()

				hooked.Warn().Msg("Second check line one")
				hooked.Warn().Msg("Second check line two")
				second()

				return sentMessages(n)
			},
			want: []string{"First check line", "Second check line one\nSecond check line two"},
		},
		{
			name: "session taking a check's batch over sends it per container when split",
			run: func(n *Notifier, hooked zerolog.Logger) []string {
				n.splitByContainer = true
				release := n.HoldNotifications()

				hooked.Warn().Str("container", "app2").Msg("Check line")
				hooked.Warn().Str("container", "app1").Msg("Check line")

				n.StartNotification()
				release()

				// Containers are sent in map order.
				sent := sentMessages(n)
				slices.Sort(sent)

				return sent
			},
			want: []string{"Check line | container=app1 ", "Check line | container=app2 "},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			n, hooked := newIdleNotifier(t)

			assert.Equal(t, tt.want, tt.run(n, hooked))
		})
	}
}

// TestNotifier_HoldNotifications_Concurrent verifies that every line logged by
// concurrent check requests and an update session is sent exactly once,
// whichever batch it joins.
func TestNotifier_HoldNotifications_Concurrent(t *testing.T) {
	t.Parallel()

	const checks = 16

	n, hooked := newIdleNotifier(t)

	var wg sync.WaitGroup

	for check := range checks {
		wg.Go(func() {
			release := n.HoldNotifications()

			hooked.Warn().Msgf("Check line %d", check)
			release()
		})
	}

	n.StartNotification()
	hooked.Warn().Msg("Session line")
	n.Flush()

	wg.Wait()

	lines := make([]string, 0, checks+1)
	for _, message := range sentMessages(n) {
		lines = append(lines, strings.Split(message, "\n")...)
	}

	want := make([]string, 0, checks+1)
	want = append(want, "Session line")

	for check := range checks {
		want = append(want, fmt.Sprintf("Check line %d", check))
	}

	assert.ElementsMatch(t, want, lines)
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
