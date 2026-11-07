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
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

// TestNotifier_StartNotification_EndSendsQueued verifies that a batch's end
// sends it only when it holds at least one message.
func TestNotifier_StartNotification_EndSendsQueued(t *testing.T) {
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
			// With the worker running, a message that the end sent would reach
			// the log.
			shoutrrr.RegisterHook(log)
			shoutrrr.StartNotification()()
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

			end := shoutrrr.StartNotification()

			log.Info().Msg("This log message is sponsored by ContainrrrVPN")
			end()
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

// TestSessionSend_SingleFocusKeepsBatchingActive verifies that a split
// notification that takes every queued entry leaves batching on, since the
// session's other split notifications follow.
func TestSessionSend_SingleFocusKeepsBatchingActive(t *testing.T) {
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

	send, _ := startSessionWith(n, []*format.Entry{
		{
			Message: "Found new image",
			Data:    map[string]any{"container": updated.Name()},
		},
	})

	send(report)

	n.entriesMutex.RLock()
	open := n.batch
	n.entriesMutex.RUnlock()

	require.NotNil(t, open, "the batch stays open after a split notification")
	assert.Empty(t, open.entries)

	n.Close()
}

// TestEntryContainer verifies which container a queued entry names. The
// container field wins, then container_name. An entry with only an image names
// no container, since several containers can share an image.
func TestEntryContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		data map[string]any
		name string
		want string
	}{
		{name: "container field", data: map[string]any{"container": "web1"}, want: "web1"},
		{name: "container_name field", data: map[string]any{"container_name": "web1"}, want: "web1"},
		{
			name: "container field wins over container_name",
			data: map[string]any{"container": "web1", "container_name": "web2"},
			want: "web1",
		},
		{
			name: "empty container field falls back to container_name",
			data: map[string]any{"container": "", "container_name": "web1"},
			want: "web1",
		},
		{name: "image only", data: map[string]any{"image": "org/web:1.0"}, want: ""},
		{name: "no fields", data: nil, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, entryContainer(&format.Entry{Message: "Line", Data: tt.data}))
		})
	}
}

// TestSessionSend_TakesOnlyFocusContainerEntries verifies that a split
// notification takes the entries that name its container, by container or by
// container_name. A line that names only the container's image stays queued.
// The session's end sends it with the other lines that name no container.
func TestSessionSend_TakesOnlyFocusContainerEntries(t *testing.T) {
	t.Parallel()

	session := reportfixture.Report(state.UpdatedState)
	require.NotEmpty(t, session.Updated())

	updated := session.Updated()[0]

	imageOnly := &format.Entry{Message: "Image line", Data: map[string]any{"image": updated.ImageName()}}

	n := newBatchingNotifier(t)
	send, end := startSessionWith(n, []*format.Entry{
		{Message: "Container line", Data: map[string]any{"container": updated.Name()}},
		{Message: "Removal line", Data: map[string]any{"container_name": updated.Name()}},
		imageOnly,
	})

	send(state.NewSingleContainerReport(updated, session))

	remaining := queuedEntries(t, n)

	assert.Equal(t, []*format.Entry{imageOnly}, remaining)
	assert.Equal(t, []string{
		"Container line | container=" + updated.Name() + " \nRemoval line | container_name=" + updated.Name() + " ",
	}, sentMessages(n))

	end()

	assert.Equal(t, []string{"Image line | image=" + updated.ImageName() + " "}, sentMessages(n))
}

// TestSessionSend_UnnamedFocusTakesNothing verifies that a split notification
// whose container has no name takes no queued entry. The lines that name no
// container stay queued for the session's end.
func TestSessionSend_UnnamedFocusTakesNothing(t *testing.T) {
	t.Parallel()

	focus := mockTypes.NewMockContainerReport(t)
	focus.EXPECT().Name().Return("")

	sessionLine := &format.Entry{Message: "Session line"}

	n := newBatchingNotifier(t)
	send, _ := startSessionWith(n, []*format.Entry{sessionLine})

	send(&state.SingleContainerReport{Focus: focus})

	remaining := queuedEntries(t, n)

	assert.Equal(t, []*format.Entry{sessionLine}, remaining)
}

// TestSessionSend_CooldownFocusWithOneContextContainer verifies that a
// split notification for a cooldown-skipped container sends that container's
// queued entries. The report also lists the session's failed and stale
// containers for context. One of those lists holds exactly one container, which
// must not be taken for the notification's subject.
func TestSessionSend_CooldownFocusWithOneContextContainer(t *testing.T) {
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
			n := newBatchingNotifier(t)
			send, _ := startSessionWith(n, []*format.Entry{
				otherEntry,
				{
					Message: "Skipping update during cooldown",
					Data:    map[string]any{"container": skipped.Name()},
				},
			})

			send(report)

			remaining := queuedEntries(t, n)

			assert.Equal(t, []*format.Entry{otherEntry}, remaining,
				"only the skipped container's entry is sent with its notification")

			sent := sentMessages(n)
			require.Len(t, sent, 1)
			assert.Contains(t, sent[0], "Skipping update during cooldown")
			assert.NotContains(t, sent[0], "Found new image")
		})
	}
}

// TestNotifier_StartNotification_EndEmptyBatch verifies that a batch's end
// sends nothing when nothing is queued, with every built-in template, and
// still ends batching. The JSON and porcelain templates render a document even
// without entries. The end would otherwise send it as an empty notification.
func TestNotifier_StartNotification_EndEmptyBatch(t *testing.T) {
	t.Parallel()

	for _, tpl := range []string{"", "json.v1", "porcelain.json", "porcelain.v1.summary-no-log"} {
		for _, legacy := range []bool{true, false} {
			t.Run(fmt.Sprintf("%q legacy %t", tpl, legacy), func(t *testing.T) {
				t.Parallel()

				n := createNotifier(newTestLogger(t), nil, nil, zerolog.ErrorLevel, tpl, legacy, format.StaticData{}, 0)
				t.Cleanup(n.Close)

				n.StartNotification()()

				n.entriesMutex.RLock()
				open := n.batch
				n.entriesMutex.RUnlock()

				assert.Nil(t, open)
				assert.Empty(t, sentMessages(n))
			})
		}
	}
}

// TestNotifier_StartSessionNotification_End verifies that ending a session's batch
// sends the lines no notification took without a report. A line that a split
// notification already sent is not sent again. Ending the batch also ends
// batching. A line logged afterwards is sent at once.
func TestNotifier_StartSessionNotification_End(t *testing.T) {
	t.Parallel()

	session := reportfixture.Report(state.UpdatedState)
	require.NotEmpty(t, session.Updated())

	updated := session.Updated()[0]
	focused := "Focused line | container=" + updated.Name() + " "

	tests := []struct {
		name             string
		want             []string
		splitByContainer bool
	}{
		{
			name:             "split by container",
			splitByContainer: true,
			want:             []string{"Container warning | container=app1 ", focused, "Session error\nSession info"},
		},
		{
			name: "grouped",
			want: []string{"Container warning | container=app1 \nSession error\nSession info", focused},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			n, hooked := newIdleNotifier(t)
			n.splitByContainer = tt.splitByContainer

			send, end := n.StartSessionNotification()

			hooked.Warn().Str("container", "app1").Msg("Container warning")
			hooked.Info().Str("container", updated.Name()).Msg("Focused line")
			hooked.Error().Msg("Session error")
			hooked.Info().Msg("Session info")

			send(state.NewSingleContainerReport(updated, session))

			end()

			n.entriesMutex.RLock()
			open := n.batch
			n.entriesMutex.RUnlock()

			assert.Nil(t, open)

			// Containers are sent in map order.
			sent := sentMessages(n)
			slices.Sort(sent)

			assert.Equal(t, tt.want, sent)

			hooked.Warn().Msg("Logged after the session")

			assert.Equal(t, []string{"Logged after the session"}, sentMessages(n))
		})
	}
}

// newIdleNotifier returns a notifier that is not batching and a logger hooked
// to it. A line logged through that logger is sent at once.
func newIdleNotifier(t *testing.T) (*Notifier, zerolog.Logger) {
	t.Helper()

	n := newBatchingNotifier(t)
	n.batch = nil

	return n, zerolog.New(io.Discard).Hook(n)
}

// TestNotifier_StartNotification verifies that starting a batch first sends
// the lines an earlier owner queued, without a report. Those are either the
// lines logged before startup or the lines of a check whose batch is taken
// over.
func TestNotifier_StartNotification(t *testing.T) {
	t.Parallel()

	n := newBatchingNotifier(t)
	hooked := zerolog.New(io.Discard).Hook(n)

	// The notifier batches from its creation until the startup message.
	hooked.Warn().Msg("Logged before startup")
	require.Empty(t, sentMessages(n))

	end := n.StartNotification()
	assert.Equal(t, []string{"Logged before startup"}, sentMessages(n))

	hooked.Warn().Msg("Logged during startup")
	assert.Empty(t, sentMessages(n))

	end()
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
				end := n.StartNotification()

				release := n.HoldNotifications()

				hooked.Warn().Msg("Check line")
				release()

				queued := sentMessages(n)

				hooked.Warn().Msg("Session line")
				end()

				return append(queued, sentMessages(n)...)
			},
			want: []string{"Check line\nSession line"},
		},
		{
			name: "session starting during a check takes its batch over",
			run: func(n *Notifier, hooked zerolog.Logger) []string {
				release := n.HoldNotifications()

				hooked.Warn().Msg("Check line before the session")

				end := n.StartNotification()

				hooked.Warn().Msg("Line during the session")
				release()

				queued := sentMessages(n)

				end()

				return append(queued, sentMessages(n)...)
			},
			want: []string{"Check line before the session", "Line during the session"},
		},
		{
			name: "check after a session's send keeps its batch when the session ends",
			run: func(n *Notifier, hooked zerolog.Logger) []string {
				send, end := n.StartSessionNotification()

				// The session sends its grouped notification.
				hooked.Warn().Msg("Session line")
				send(nil)

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

// TestNotifier_StartSessionNotification_SendReachesOnlyItsBatch verifies that
// a session's send uses only the batch the session opened. Once that batch has
// ended, a send sends nothing. It leaves the open batch, such as a check
// request's or a later session's, to its owner, which sends it later.
func TestNotifier_StartSessionNotification_SendReachesOnlyItsBatch(t *testing.T) {
	t.Parallel()

	session := reportfixture.Report(state.UpdatedState)
	require.NotEmpty(t, session.Updated())

	updated := session.Updated()[0]
	split := state.NewSingleContainerReport(updated, session)
	checkLine := "Check line | container=" + updated.Name() + " "

	// sendDuringCheck runs beforeEnd with the session's send and then ends the
	// session. It then sends report through that send while a check's batch is
	// open. Last, it releases the check. The check's line names the split report's
	// container, so a split send that reached the check's batch would take it.
	// It returns the messages sent by the session's end, by the late send and
	// by the check's release.
	sendDuringCheck := func(
		n *Notifier,
		hooked zerolog.Logger,
		report types.Report,
		beforeEnd func(send func(types.Report)),
	) [][]string {
		send, end := n.StartSessionNotification()

		hooked.Warn().Msg("Session line")
		beforeEnd(send)
		end()

		sent := [][]string{sentMessages(n)}

		release := n.HoldNotifications()

		hooked.Warn().Str("container", updated.Name()).Msg("Check line")
		send(report)

		sent = append(sent, sentMessages(n))

		release()

		return append(sent, sentMessages(n))
	}

	tests := []struct {
		// run logs the lines and drives the batches. It returns the messages
		// sent by each of its steps.
		run  func(n *Notifier, hooked zerolog.Logger) [][]string
		name string
		// want holds the messages each step sends.
		want [][]string
	}{
		{
			name: "grouped send after the session's end",
			run: func(n *Notifier, hooked zerolog.Logger) [][]string {
				return sendDuringCheck(n, hooked, nil, func(func(types.Report)) {})
			},
			want: [][]string{{"Session line"}, nil, {checkLine}},
		},
		{
			name: "split send after the session's end",
			run: func(n *Notifier, hooked zerolog.Logger) [][]string {
				return sendDuringCheck(n, hooked, split, func(func(types.Report)) {})
			},
			want: [][]string{{"Session line"}, nil, {checkLine}},
		},
		{
			name: "second grouped send",
			run: func(n *Notifier, hooked zerolog.Logger) [][]string {
				return sendDuringCheck(n, hooked, nil, func(send func(types.Report)) { send(nil) })
			},
			want: [][]string{{"Session line"}, nil, {checkLine}},
		},
		{
			name: "send after a later session took the batch over",
			run: func(n *Notifier, hooked zerolog.Logger) [][]string {
				send, _ := n.StartSessionNotification()

				hooked.Warn().Msg("First session line")

				_, end := n.StartSessionNotification()

				sent := [][]string{sentMessages(n)}

				hooked.Warn().Msg("Second session line")
				send(nil)

				sent = append(sent, sentMessages(n))

				end()

				return append(sent, sentMessages(n))
			},
			want: [][]string{{"First session line"}, nil, {"Second session line"}},
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

	end := n.StartNotification()

	hooked.Warn().Msg("Session line")
	end()

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

// TestSessionSend_FailedFocusCarriesFailure verifies that a failure line travels with its split notification.
//
// With the legacy template the notification names the container and image and
// gives the failure's cause.
func TestSessionSend_FailedFocusCarriesFailure(t *testing.T) {
	t.Parallel()

	session := reportfixture.Report(state.FailedState)
	failed := session.Failed()[0]

	n := newBatchingNotifier(t)
	send, _ := startSessionWith(n, []*format.Entry{{
		Message: logging.ContainerUpdateFailed,
		Data: map[string]any{
			"container": failed.Name(),
			"image":     failed.ImageName(),
			"error":     "pre-update command failed: exit code 1",
		},
	}})

	send(state.NewSingleContainerReport(failed, session))

	assert.Equal(t, []string{
		"Update failed for " + failed.Name() + " (" + failed.ImageName() + "): pre-update command failed: exit code 1",
	}, sentMessages(n))
}
