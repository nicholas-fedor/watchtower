package notify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
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
	unlock chan bool
	sent   chan bool
	ctx    context.Context //nolint:containedctx
}

func (b blockingRouter) Send(_ string, _ *shoutrrrTypes.Params) []error {
	select {
	case <-b.unlock:
		b.sent <- true
	case <-b.ctx.Done():
		// canceled, don't send
	}

	return nil
}

// sendNotificationsWithBlockingRouter creates a notifier with a blocking router for testing.
// It queues a message and returns the notifier and router to verify notification delays.
//
// TODO: Remove legacy template usage when legacy notification types are removed.
//
//nolint:godox
func sendNotificationsWithBlockingRouter(t *testing.T) (*shoutrrrTypeNotifier, *blockingRouter, error) {
	t.Helper()

	legacy := true
	ctx, cancel := context.WithCancel(context.Background())

	router := &blockingRouter{
		unlock: make(chan bool, 1),
		sent:   make(chan bool, 1),
		ctx:    ctx,
	}

	tpl, err := format.New(newTestLogger(t), "", legacy)
	if err != nil {
		cancel()

		return nil, nil, err
	}

	shoutrrr := &shoutrrrTypeNotifier{
		template: tpl,
		messages: make(chan string, 1),
		done:     make(chan struct{}),
		Router:   router,
		params:   &shoutrrrTypes.Params{},
		ctx:      ctx,
		cancel:   cancel,
		delay:    time.Duration(0),
	}

	entry := &format.Entry{
		Message: "foo bar",
	}

	go sendNotifications(shoutrrr)

	shoutrrr.StartNotification(false)
	// Enqueue directly: Run(nil, ...) is fail-closed (cannot parse notify field).
	// These tests exercise the send worker, not hook field extraction.
	shoutrrr.entriesMutex.Lock()
	shoutrrr.entries = append(shoutrrr.entries, entry)
	shoutrrr.entriesMutex.Unlock()
	shoutrrr.SendNotification(nil)

	return shoutrrr, router, nil
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

// TestShutdownGracePeriodConstant verifies that the shutdownGracePeriod constant is set to 50ms.
func TestShutdownGracePeriodConstant(t *testing.T) {
	expectedGracePeriod := 50 * time.Millisecond
	if shutdownGracePeriod != expectedGracePeriod {
		t.Fatalf("expected shutdownGracePeriod to be %v, got %v", expectedGracePeriod, shutdownGracePeriod)
	}
}

// TestCloseDoesNotHangWithBlockingRouter verifies that Close() completes without hanging
// when the router is blocked. This tests that the context cancellation properly unblocks
// the send call.
func TestCloseDoesNotHangWithBlockingRouter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Create a notifier with a blocking router
		shoutrrr, blockingRouter, err := sendNotificationsWithBlockingRouter(t)
		if err != nil {
			t.Fatal(err)
		}

		// Wait for goroutine to be blocked
		synctest.Wait()

		// Verify router is blocked (not yet sent)
		select {
		case <-blockingRouter.sent:
			t.Fatal("message should not have been sent yet")
		default:
			// Good - router is blocked
		}

		// Close should complete without hanging - use a goroutine since it may block briefly
		closeDone := make(chan error, 1)

		go func() {
			shoutrrr.Close()

			closeDone <- nil
		}()

		// Wait for Close to complete (should be fast with synctest)
		select {
		case <-closeDone:
			// Good - Close completed
			t.Log("Close() completed successfully")
		case <-time.After(2 * time.Second):
			t.Fatal("Close() hung - context cancellation did not unblock the goroutine")
		}

		// Verify context was canceled
		select {
		case <-shoutrrr.ctx.Done():
			t.Log("Context was canceled")
		default:
			t.Fatal("expected context to be canceled")
		}

		// Clean up: unlock the router so the goroutine can exit gracefully
		blockingRouter.unlock <- true

		// Wait for the done channel to be signaled
		synctest.Wait()

		select {
		case <-shoutrrr.done:
			t.Log("Done channel was signaled")
		default:
			t.Fatal("expected done channel to be signaled")
		}
	})
}

// controlledRouter simulates a router that can be controlled via channels for testing.
// It waits for a continue signal before sending, allowing deterministic testing.
type controlledRouter struct {
	continueCh chan struct{}
	sent       chan bool
	ctx        context.Context //nolint:containedctx
}

func (c *controlledRouter) Send(_ string, _ *shoutrrrTypes.Params) []error {
	// Signal that we're waiting
	// Wait for continue signal or context cancellation
	select {
	case <-c.continueCh:
		c.sent <- true
	case <-c.ctx.Done():
		// canceled, don't send
	}

	return nil
}

// TestGracePeriodAllowsInFlightMessages verifies that in-flight messages have time to complete
// before context is canceled during the shutdown grace period.
func TestGracePeriodAllowsInFlightMessages(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Create a controlled router
		ctx, cancel := context.WithCancel(context.Background())

		controlledR := &controlledRouter{
			continueCh: make(chan struct{}, 1),
			sent:       make(chan bool, 1),
			ctx:        ctx,
		}

		tpl, err := format.New(newTestLogger(t), "", true)
		if err != nil {
			cancel()
			t.Fatal(err)
		}

		shoutrrr := &shoutrrrTypeNotifier{
			template:  tpl,
			messages:  make(chan string, 1),
			done:      make(chan struct{}),
			stop:      make(chan struct{}),
			Router:    controlledR,
			params:    &shoutrrrTypes.Params{},
			ctx:       ctx,
			cancel:    cancel,
			delay:     time.Duration(0),
			receiving: atomic.Bool{},
		}
		shoutrrr.receiving.Store(true)

		// Start the notification goroutine
		go sendNotifications(shoutrrr)

		// Queue a message
		shoutrrr.messages <- "test message"

		// Wait for goroutine to be blocked in router.Send
		synctest.Wait()

		// Now call Close - this should trigger the grace period wait
		// We let it proceed by signaling continueCh in a separate goroutine
		go func() {
			// Wait a bit to simulate the grace period passing
			synctest.Wait()
			// Then allow the message to be sent
			controlledR.continueCh <- struct{}{}
		}()

		// Close should complete
		shoutrrr.Close()

		// Wait for done channel
		synctest.Wait()

		// Note: Close() already waits for done channel internally (line 500 in shoutrrr.go)
		// so we don't need to wait for it here. Instead, we verify the message was sent.

		// Verify the message was actually sent by checking controlledR.sent
		timeout := 2 * time.Second
		select {
		case sent := <-controlledR.sent:
			if !sent {
				t.Fatal("expected message to be sent during grace period")
			}

			t.Log("Message was successfully sent during grace period")
		case <-time.After(timeout):
			t.Fatalf("expected sent channel to be signaled within %v timeout", timeout)
		}
	})
}

// TestCloseWithNoGoroutine verifies that Close() works correctly when the
// notification goroutine was never started.
func TestCloseWithNoGoroutine(t *testing.T) {
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

	// Close should complete immediately without blocking
	// Use goroutine+channel+select pattern to avoid flaky wall-clock timing.
	// Close() has a shutdownGracePeriod (50ms) wait, so we use 1s timeout
	// which is comfortably larger than 2x shutdownGracePeriod.
	closeDone := make(chan struct{})

	go func() {
		shoutrrr.Close()

		closeDone <- struct{}{}
	}()

	// Select with timeout - Close() should complete within reasonable time
	select {
	case <-closeDone:
		t.Log("Close() completed successfully with no goroutine")
	case <-time.After(1 * time.Second):
		t.Fatalf("Close() took too long without goroutine (timeout exceeded)")
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

type countingRouter struct {
	sends atomic.Int32
}

func (c *countingRouter) Send(_ string, _ *shoutrrrTypes.Params) []error {
	c.sends.Add(1)

	return nil
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
