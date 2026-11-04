package notify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"

	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/reportfixture"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
)

var allButTrace = zerolog.DebugLevel

const shoutrrrFatalHelperEnv = "WATCHTOWER_SHOUTRRR_FATAL_HELPER"

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

// newBufferLogger returns a JSON logger at trace level and the buffer it writes
// to, for tests that check what the notifier logs.
//
// Returns:
//   - *zerolog.Logger: Logger writing to the buffer.
//   - *logcapture.Buffer: Buffer holding the log output.
func newBufferLogger() (*zerolog.Logger, *logcapture.Buffer) {
	buf := &logcapture.Buffer{}
	// JSON encoding keeps field assertions simple (failure_type, failed_count, etc.).
	l := zerolog.New(buf).Level(zerolog.TraceLevel).With().Timestamp().Logger()

	return &l, buf
}

// createTestNotifier wraps createNotifier with the default template. Pass a
// custom template to createNotifier directly when a test needs one.
func createTestNotifier(
	log *zerolog.Logger,
	urls []string,
	level zerolog.Level,
	legacy bool,
	data format.StaticData,
	stdout bool,
	delay time.Duration,
) *shoutrrrTypeNotifier {
	return createNotifier(log, urls, level, "", legacy, data, stdout, delay)
}

// mockRouter implements the router interface for testing error scenarios.
type mockRouter struct {
	sendErrors []error
}

func (m *mockRouter) Send(_ string, _ *shoutrrrTypes.Params) []error {
	return m.sendErrors
}

// blockingRouter simulates a notification router with blocking behavior for testing.
// It waits for an unlock signal before sending and signals completion via a channel.
type blockingRouter struct {
	unlock chan struct{}
	sent   chan struct{}
}

func (b blockingRouter) Send(_ string, _ *shoutrrrTypes.Params) []error {
	<-b.unlock

	b.sent <- struct{}{}

	return nil
}

// recordingRouter records each message it is asked to send, in order. It is
// safe for concurrent use.
type recordingRouter struct {
	mu       sync.Mutex
	messages []string
}

func (r *recordingRouter) Send(message string, _ *shoutrrrTypes.Params) []error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.messages = append(r.messages, message)

	return nil
}

// sent returns the messages sent so far, in the order they were sent.
//
// Returns:
//   - []string: A copy of the sent messages.
func (r *recordingRouter) sent() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.messages)
}

// queuedMessages returns count numbered messages, as tests queue them.
//
// Parameters:
//   - count: Number of messages.
//
// Returns:
//   - []string: Messages "message 0" through "message count-1".
func queuedMessages(count int) []string {
	messages := make([]string, count)
	for i := range messages {
		messages[i] = fmt.Sprintf("message %d", i)
	}

	return messages
}

// sendNotificationsWithBlockingRouter returns a notifier whose worker is
// sending one message through a blocking router, with the router.
//
// Parameters:
//   - t: Test that owns the notifier.
//
// Returns:
//   - *shoutrrrTypeNotifier: Notifier with a running worker.
//   - *blockingRouter: Router the worker is blocked in until unlocked.
func sendNotificationsWithBlockingRouter(t *testing.T) (*shoutrrrTypeNotifier, *blockingRouter) {
	t.Helper()

	router := &blockingRouter{
		unlock: make(chan struct{}, 1),
		sent:   make(chan struct{}, 1),
	}

	shoutrrr := createTestNotifier(newTestLogger(t), []string{}, allButTrace, true, format.StaticData{}, false, 0)
	shoutrrr.Router = router
	shoutrrr.RegisterHook(newTestLogger(t))

	shoutrrr.StartNotification(false)
	// Enqueue directly: Run(nil, ...) is fail-closed (cannot parse notify field).
	// These tests exercise the send worker, not hook field extraction.
	shoutrrr.entriesMutex.Lock()
	shoutrrr.entries = append(shoutrrr.entries, &format.Entry{Message: "foo bar"})
	shoutrrr.entriesMutex.Unlock()
	shoutrrr.SendNotification(nil)

	return shoutrrr, router
}

// TestRegisterHook verifies that RegisterHook starts the notifier
// receiving log entries. Registering it a second time changes nothing.
func TestRegisterHook(t *testing.T) {
	t.Parallel()

	t.Run("it has not been added before", func(t *testing.T) {
		t.Parallel()

		t.Run("should start receiving via RegisterHook", func(t *testing.T) {
			t.Parallel()

			level := zerolog.TraceLevel
			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{},
				level,
				true,
				format.StaticData{},
				false,
				time.Second,
			)
			assert.False(t, shoutrrr.receiving.Load())

			log := newTestLogger(t)

			shoutrrr.RegisterHook(log)
			defer shoutrrr.Close()

			assert.True(t, shoutrrr.receiving.Load())
		})
	})
	t.Run("it is being added a second time", func(t *testing.T) {
		t.Parallel()

		t.Run("should be idempotent", func(t *testing.T) {
			t.Parallel()

			level := zerolog.TraceLevel
			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{},
				level,
				true,
				format.StaticData{},
				false,
				time.Second,
			)
			log := newTestLogger(t)

			shoutrrr.RegisterHook(log)
			defer shoutrrr.Close()

			shoutrrr.RegisterHook(log)
			assert.True(t, shoutrrr.receiving.Load())
		})
	})
}

// TestCreateNotifier_EmptyTitle verifies that a notifier built without a
// title sets no title param.
func TestCreateNotifier_EmptyTitle(t *testing.T) {
	t.Parallel()

	t.Run("should not have set the title param", func(t *testing.T) {
		t.Parallel()

		shoutrrr := createTestNotifier(newTestLogger(t), []string{"logger://"}, allButTrace, true, format.StaticData{
			Host:  "test.host",
			Title: "",
		}, false, time.Second)
		_, found := shoutrrr.params.Title()
		assert.False(t, found)
	})
}

// TestClose verifies that Close is safe to call repeatedly or concurrently. It
// returns at once without a running worker. Later operations do not panic.
func TestClose(t *testing.T) {
	t.Parallel()

	t.Run("Close() is called multiple times", func(t *testing.T) {
		t.Parallel()

		t.Run("should be idempotent and not panic", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{"logger://"},
				allButTrace,
				true,
				format.StaticData{},
				false,
				time.Duration(0),
			)
			shoutrrr.RegisterHook(newTestLogger(t))

			// First close should work normally
			shoutrrr.Close()

			// Subsequent closes should be no-ops
			shoutrrr.Close()
			shoutrrr.Close()

			// Should not panic
		})
	})

	t.Run("Close() is called without starting the goroutine", func(t *testing.T) {
		t.Parallel()

		t.Run("should not panic or block", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{"logger://"},
				allButTrace,
				true,
				format.StaticData{},
				false,
				time.Duration(0),
			)
			// Note: Not calling RegisterHook(), so no goroutine is started

			// Close should work without blocking
			shoutrrr.Close()

			// Should not panic
		})
	})

	t.Run("operations are attempted after Close()", func(t *testing.T) {
		t.Parallel()

		t.Run("should handle gracefully without panicking", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{"logger://"},
				allButTrace,
				true,
				format.StaticData{},
				false,
				time.Duration(0),
			)
			shoutrrr.RegisterHook(newTestLogger(t))

			// Close the notifier
			shoutrrr.Close()

			// These operations should not panic after close
			shoutrrr.StartNotification(false)
			shoutrrr.SendNotification(nil)
			shoutrrr.SendNotification(nil)

			// Fire should still work (but may not send if channel is closed)
			entry := &format.Entry{Message: "test"}
			shoutrrr.Run(nil, zerolog.InfoLevel, entry.Message)

			err := error(nil)
			require.NoError(t, err)

			// Should not panic
		})
	})

	t.Run("Close() is called concurrently", func(t *testing.T) {
		t.Parallel()

		t.Run("should handle concurrent calls safely", func(t *testing.T) {
			t.Parallel()

			shoutrrr := createTestNotifier(
				newTestLogger(t),
				[]string{"logger://"},
				allButTrace,
				true,
				format.StaticData{},
				false,
				time.Duration(0),
			)
			shoutrrr.RegisterHook(newTestLogger(t))

			// Start multiple goroutines calling Close concurrently
			done := make(chan bool, 10)

			for range 10 {
				go func() {
					shoutrrr.Close()

					done <- true
				}()
			}

			// Wait for all to complete
			for range 10 {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("Close did not return")
				}
			}

			// Should not panic and all should complete
		})
	})
}

// TestClose_WaitsForInFlightSend verifies that Close waits for the message the
// worker is sending. The message is not reported as sent while the router is
// still sending it. The send finishes before Close returns.
func TestClose_WaitsForInFlightSend(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shoutrrr, router := sendNotificationsWithBlockingRouter(t)

		// Wait for the worker to block in Send.
		synctest.Wait()

		select {
		case <-router.sent:
			t.Fatal("message reported as sent while the router is still sending")
		default:
		}

		closed := make(chan struct{})

		go func() {
			shoutrrr.Close()
			close(closed)
		}()

		synctest.Wait()

		select {
		case <-closed:
			t.Fatal("Close returned while a send was in progress")
		default:
		}

		router.unlock <- struct{}{}

		synctest.Wait()

		select {
		case <-closed:
		default:
			t.Fatal("Close did not return after the send finished")
		}

		select {
		case <-router.sent:
		default:
			t.Fatal("expected the message to be sent")
		}
	})
}

// TestClose_SkipsSendDelay verifies that Close sends every queued message, in
// queue order, without waiting the configured delay between them.
func TestClose_SkipsSendDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		queued := queuedMessages(5)

		router := &recordingRouter{}
		notifier := createTestNotifier(newTestLogger(t), []string{}, allButTrace, true, format.StaticData{}, false, time.Minute)
		notifier.Router = router
		notifier.RegisterHook(newTestLogger(t))

		for _, message := range queued {
			notifier.messages <- message
		}

		// Let the worker take a message and start waiting the delay.
		synctest.Wait()

		start := time.Now()

		notifier.Close()

		assert.Zero(t, time.Since(start))
		assert.Equal(t, queued, router.sent())
	})
}

// TestClose_SendAfterCloseIsDropped verifies that a message sent after Close
// is dropped instead of panicking or reaching the queue. The entry renders a
// message. The send then reaches the closed check rather than the
// empty-message skip.
func TestClose_SendAfterCloseIsDropped(t *testing.T) {
	t.Parallel()

	log, buf := newBufferLogger()
	notifier := createTestNotifier(log, []string{}, allButTrace, true, format.StaticData{}, false, 0)
	notifier.Close()

	require.NotPanics(t, func() {
		notifier.sendEntries([]*format.Entry{{Level: "info", Message: "late"}}, nil)
	})

	assert.Empty(t, notifier.messages)
	assert.Contains(t, logcapture.Messages(t, buf.String(), "debug"), "Notifier closed, skipping send")
}

// TestClose_ConcurrentSendDoesNotPanic verifies that sendEntries running while
// Close shuts the worker down never panics. The test cannot force a send
// between the closed check and the enqueue. It repeats the race many times
// instead.
func TestClose_ConcurrentSendDoesNotPanic(t *testing.T) {
	t.Parallel()

	const (
		rounds  = 50
		senders = 4
		sends   = 10
	)

	entries := []*format.Entry{{Level: "info", Message: "late"}}

	for range rounds {
		log := zerolog.Nop()
		notifier := createTestNotifier(&log, []string{}, allButTrace, true, format.StaticData{}, false, 0)
		notifier.Router = &recordingRouter{}
		notifier.RegisterHook(&log)

		var wg sync.WaitGroup

		for range senders {
			wg.Go(func() {
				assert.NotPanics(t, func() {
					for range sends {
						notifier.sendEntries(entries, nil)
					}
				})
			})
		}

		notifier.Close()
		wg.Wait()
	}
}

// TestClose_SendPastClosedCheckDoesNotPanic verifies that a sender does not
// panic when it read the notifier as open before Close ran and queues its
// message after Close returns. Clearing the closed flag after Close gives the
// sender the state it would have seen, without pausing it inside sendEntries.
// The message stays in the queue that no worker reads.
func TestClose_SendPastClosedCheckDoesNotPanic(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()
	notifier := createTestNotifier(&log, []string{}, allButTrace, true, format.StaticData{}, false, 0)
	notifier.Router = &recordingRouter{}
	notifier.RegisterHook(&log)
	notifier.Close()

	notifier.closed.Store(false)

	require.NotPanics(t, func() {
		notifier.sendEntries([]*format.Entry{{Level: "info", Message: "late"}}, nil)
	})

	assert.Len(t, notifier.messages, 1)
}

// TestLL_WithoutLogger verifies that a notifier without its loop-safe logger,
// or a nil notifier, logs to a discarding logger instead of panicking.
func TestLL_WithoutLogger(t *testing.T) {
	t.Parallel()

	var nilNotifier *shoutrrrTypeNotifier

	for _, notifier := range []*shoutrrrTypeNotifier{nilNotifier, {}} {
		log := notifier.ll()
		require.NotNil(t, log)
		assert.Equal(t, zerolog.Disabled, log.GetLevel())
	}
}

// TestCreateNotifier_FatalsOnBadURL verifies that createNotifier calls Fatal
// when given a docker-secret file path instead of a valid URL.
// The fatal path is tested via subprocess because zerolog.Fatal calls os.Exit.
func TestCreateNotifier_FatalsOnBadURL(t *testing.T) {
	if os.Getenv(shoutrrrFatalHelperEnv) != "" {
		// Child process: expect createNotifier to fatal on invalid URL.
		log := zerolog.Nop()
		createNotifier(
			&log,
			[]string{"docker-secret:/run/secrets/my_url"},
			zerolog.InfoLevel,
			"",
			true,
			format.StaticData{},
			false,
			0,
		)

		// If we reach here, the fatal did not exit.
		t.Fatal("expected createNotifier to fatal on bad URL")
	}

	// Parent process: run child and assert non-zero exit.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCreateNotifier_FatalsOnBadURL$")

	cmd.Env = append(os.Environ(), shoutrrrFatalHelperEnv+"=1")
	out, err := cmd.CombinedOutput()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("timed out waiting for createNotifier fatal path; output:\n%s", string(out))
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
		t.Fatalf("expected non-zero exit for bad URL; output:\n%s", string(out))
	}
}

// TestCreateNotifier_AcceptsGotifyURLFromFileExpansion verifies that createNotifier
// succeeds when given a valid Gotify URL matching expanded secret content.
func TestCreateNotifier_AcceptsGotifyURLFromFileExpansion(t *testing.T) {
	notifier := createTestNotifier(
		newTestLogger(t),
		[]string{"gotify://gotify.example.com/Atoken123456789"},
		zerolog.InfoLevel,
		true,
		format.StaticData{},
		false,
		0,
	)

	require.NotNil(t, notifier)
}

// TestCreateNotifier_InvalidTemplateFallsBackToDefault verifies that a
// notification template that fails to parse is replaced by the default
// template, so notifications render as with no template set instead of
// failing when the first notification is sent.
func TestCreateNotifier_InvalidTemplateFallsBackToDefault(t *testing.T) {
	data := format.Data{
		Host: "test.host", Title: "Watchtower updates on test.host",
		Entries: []*format.Entry{
			{Level: "info", Message: "Found new image", Data: map[string]any{"image": "mock/updt1:latest"}},
			{Level: "warning", Message: "Could not check container for updates"},
		},
		Report: reportfixture.Report(
			state.UpdatedState,
			state.FailedState,
			state.SkippedState,
			state.FreshState,
			state.RestartedState,
		),
	}

	for _, legacy := range []bool{true, false} {
		t.Run(fmt.Sprintf("legacy=%t", legacy), func(t *testing.T) {
			invalid := createNotifier(newTestLogger(t), []string{}, zerolog.TraceLevel, "{{ .Broken", legacy, format.StaticData{}, false, 0)
			t.Cleanup(invalid.Close)

			fallback := createNotifier(newTestLogger(t), []string{}, zerolog.TraceLevel, "", legacy, format.StaticData{}, false, 0)
			t.Cleanup(fallback.Close)

			want, err := fallback.template.Render(fallback.ll(), data)
			require.NoError(t, err)

			got, err := invalid.template.Render(invalid.ll(), data)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

// TestCreateNotifier_LogsShoutrrrRecordsAtTheirLevel verifies that the
// notifier gives Shoutrrr its loop-safe logger. A Shoutrrr warning is a warn
// entry that is not notified. The logger service's text is still logged
// without a level. Both messages start with the Shoutrrr prefix.
func TestCreateNotifier_LogsShoutrrrRecordsAtTheirLevel(t *testing.T) {
	t.Parallel()

	// entry holds the fields of a log entry that the notifier decides.
	type entry struct {
		Level   string `json:"level"`
		Message string `json:"message"`
		Notify  string `json:"notify"`
	}

	log, buf := logcapture.NewJSON(zerolog.InfoLevel)

	notifier := createTestNotifier(
		log,
		[]string{"logger://", newGenericWebhookURL(t)},
		zerolog.InfoLevel,
		false,
		format.StaticData{},
		false,
		0,
	)
	t.Cleanup(notifier.Close)

	for _, err := range notifier.Router.Send("Found new image", notifier.params) {
		require.NoError(t, err)
	}

	assert.ElementsMatch(t, []entry{
		{Level: "", Message: "Shoutrrr: Found new image", Notify: "no"},
		{Level: "warn", Message: "Shoutrrr: sending without TLS", Notify: "no"},
	}, logcapture.Entries[entry](t, buf.String()))
}

// TestCreateNotifier_PrintsLoggerTextToStdout verifies that with stdout set,
// the logger service's text goes to standard output without the Shoutrrr
// prefix. Only Shoutrrr's own records reach the Watchtower log.
//
// It replaces os.Stdout. It does not run in parallel for that reason.
func TestCreateNotifier_PrintsLoggerTextToStdout(t *testing.T) {
	reader, writer, err := os.Pipe()
	require.NoError(t, err)

	stdout := os.Stdout
	os.Stdout = writer

	t.Cleanup(func() { os.Stdout = stdout })

	log, buf := logcapture.NewJSON(zerolog.InfoLevel)

	notifier := createTestNotifier(
		log,
		[]string{"logger://", newGenericWebhookURL(t)},
		zerolog.InfoLevel,
		false,
		format.StaticData{},
		true,
		0,
	)
	t.Cleanup(notifier.Close)

	for _, err := range notifier.Router.Send("Found new image", notifier.params) {
		require.NoError(t, err)
	}

	os.Stdout = stdout

	require.NoError(t, writer.Close())

	printed, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	assert.Equal(t, "Found new image\n", string(printed))

	logs := buf.String()
	assert.Equal(t, []string{"Shoutrrr: sending without TLS"}, logcapture.Messages(t, logs, "warn"))
	assert.NotContains(t, logs, "Found new image")
}
