package notifications

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"

	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
)

// TestProcessSendErrors verifies that each failed send is logged under its
// failure category, followed by a summary with the counts. An error without a
// matching URL is logged as an index mismatch.
func TestProcessSendErrors(t *testing.T) {
	t.Parallel()

	t.Run("should handle index guard when errs length exceeds URLs length", func(t *testing.T) {
		t.Parallel()

		log, logBuffer := newBufferLogger()

		mockRouter := &mockRouter{
			sendErrors: []error{errors.New("test error"), errors.New("extra error")},
		}

		shoutrrr := createTestNotifier(
			log,
			[]string{"logger://"},
			allButTrace,
			true,
			format.StaticData{},
			false,
			time.Duration(0),
		)
		shoutrrr.Router = mockRouter
		shoutrrr.RegisterHook(log)
		shoutrrr.StartNotification(false)
		log.Info().Msg("test message")
		shoutrrr.SendNotification(nil)

		shoutrrr.Close()
		assert.Contains(t, logBuffer.String(), `index_mismatch`)
	})

	t.Run("should categorize authentication errors", func(t *testing.T) {
		t.Parallel()

		log, logBuffer := newBufferLogger()

		mockRouter := &mockRouter{
			sendErrors: []error{errors.New("unauthorized access")},
		}

		shoutrrr := createTestNotifier(
			log,
			[]string{"logger://"},
			allButTrace,
			true,
			format.StaticData{},
			false,
			time.Duration(0),
		)
		shoutrrr.Router = mockRouter
		shoutrrr.RegisterHook(log)
		shoutrrr.StartNotification(false)
		log.Info().Msg("test message")
		shoutrrr.SendNotification(nil)

		shoutrrr.Close()
		assert.Regexp(t, `failure_type.*authentication`, logBuffer.String())
	})

	t.Run("should categorize network errors", func(t *testing.T) {
		t.Parallel()

		log, logBuffer := newBufferLogger()

		mockRouter := &mockRouter{
			sendErrors: []error{errors.New("connection timeout")},
		}

		shoutrrr := createTestNotifier(
			log,
			[]string{"logger://"},
			allButTrace,
			true,
			format.StaticData{},
			false,
			time.Duration(0),
		)
		shoutrrr.Router = mockRouter
		shoutrrr.RegisterHook(log)
		shoutrrr.StartNotification(false)
		log.Info().Msg("test message")
		shoutrrr.SendNotification(nil)

		shoutrrr.Close()
		assert.Regexp(t, `failure_type.*network`, logBuffer.String())
	})

	t.Run("should categorize rate limit errors", func(t *testing.T) {
		t.Parallel()

		log, logBuffer := newBufferLogger()

		mockRouter := &mockRouter{
			sendErrors: []error{errors.New("too many requests")},
		}

		shoutrrr := createTestNotifier(
			log,
			[]string{"logger://"},
			allButTrace,
			true,
			format.StaticData{},
			false,
			time.Duration(0),
		)
		shoutrrr.Router = mockRouter
		shoutrrr.RegisterHook(log)
		shoutrrr.StartNotification(false)
		log.Info().Msg("test message")
		shoutrrr.SendNotification(nil)

		shoutrrr.Close()
		assert.Regexp(t, `failure_type.*rate_limit`, logBuffer.String())
	})

	t.Run("should categorize unknown errors", func(t *testing.T) {
		t.Parallel()

		log, logBuffer := newBufferLogger()

		mockRouter := &mockRouter{
			sendErrors: []error{errors.New("some unknown error")},
		}

		shoutrrr := createTestNotifier(
			log,
			[]string{"logger://"},
			allButTrace,
			true,
			format.StaticData{},
			false,
			time.Duration(0),
		)
		shoutrrr.Router = mockRouter
		shoutrrr.RegisterHook(log)
		shoutrrr.StartNotification(false)
		log.Info().Msg("test message")
		shoutrrr.SendNotification(nil)

		shoutrrr.Close()
		assert.Regexp(t, `failure_type.*unknown`, logBuffer.String())
	})

	t.Run("should log summary with failure counts", func(t *testing.T) {
		t.Parallel()

		log, logBuffer := newBufferLogger()

		mockRouter := &mockRouter{
			sendErrors: []error{errors.New("auth error"), errors.New("network error")},
		}

		shoutrrr := createTestNotifier(
			log,
			[]string{"logger://", "logger://"},
			allButTrace,
			true,
			format.StaticData{},
			false,
			time.Duration(0),
		)
		shoutrrr.Router = mockRouter
		shoutrrr.RegisterHook(log)
		shoutrrr.StartNotification(false)
		log.Info().Msg("test message")
		shoutrrr.SendNotification(nil)

		shoutrrr.Close()
		assert.Regexp(t, `failed_count.*2`, logBuffer.String())
	})
}

// TestProcessSendErrors_RedactsURLs verifies that a failed send logs the
// service URL as its scheme only, in every failure category. Some services
// carry their credential in the host, such as an IFTTT key or a Pushbullet
// token, so keeping the host would write it to the log.
func TestProcessSendErrors_RedactsURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		err     error
		wantURL string
	}{
		{
			name:    "authentication failure",
			url:     "ifttt://iftttsecretkey/?events=deploy",
			err:     errors.New("unauthorized"),
			wantURL: "ifttt://[redacted]",
		},
		{
			name:    "network failure",
			url:     "pushbullet://pushbulletsecrettoken/device",
			err:     errors.New("connection refused"),
			wantURL: "pushbullet://[redacted]",
		},
		{
			name:    "rate limit",
			url:     "notifiarr://notifiarrsecretkey?channel=1",
			err:     errors.New("too many requests"),
			wantURL: "notifiarr://[redacted]",
		},
		{
			name:    "unknown failure",
			url:     "wecom://wecomsecretkey",
			err:     errors.New("unexpected response"),
			wantURL: "wecom://[redacted]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log, logBuffer := newBufferLogger()
			notifier := createTestNotifier(log, []string{"logger://"}, allButTrace, true, format.StaticData{}, false, 0)
			notifier.Urls = []string{tt.url}

			processSendErrors(notifier, []error{tt.err})

			output := logBuffer.String()
			assert.Contains(t, output, `"url":"`+tt.wantURL+`"`)
			assert.NotContains(t, output, "secret")
		})
	}
}

// TestSlowNotificationNotSent verifies that a message is not reported as sent
// while the router is still sending it.
func TestSlowNotificationNotSent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shoutrrr, blockingRouter, err := sendNotificationsWithBlockingRouter(t)
		if err != nil {
			t.Fatal(err)
		}

		// Wait for all goroutines to be blocked
		synctest.Wait()

		// The notification should not be sent because the router is blocked
		select {
		case <-blockingRouter.sent:
			t.Fatal("expected notification not to be sent")
		default:
			// Good, channel is empty
		}

		// Cancel cannot interrupt an in-flight Router.Send. Unlock so Send returns
		// and the worker can observe cancellation and exit.
		shoutrrr.cancel()

		blockingRouter.unlock <- true

		<-shoutrrr.done
	})
}

// TestSlowNotificationSent verifies that a message is sent once the router
// finishes sending it.
func TestSlowNotificationSent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shoutrrr, blockingRouter, err := sendNotificationsWithBlockingRouter(t)
		if err != nil {
			t.Fatal(err)
		}

		// Unlock the router
		blockingRouter.unlock <- true

		// Wait for the notification to be sent
		synctest.Wait()

		// The notification should be sent
		select {
		case sent := <-blockingRouter.sent:
			if !sent {
				t.Fatal("expected notification to be sent")
			}
		default:
			t.Fatal("expected notification to be sent")
		}

		// Cancel to clean up
		shoutrrr.cancel()
		// Wait for goroutine to exit
		<-shoutrrr.done
	})
}

// TestGracefulTerminationNotificationGoroutine verifies that canceling the
// notifier's context ends an idle worker.
func TestGracefulTerminationNotificationGoroutine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shoutrrr := createTestNotifier(
			newTestLogger(t),
			[]string{"logger://"},
			allButTrace,
			true,
			format.StaticData{},
			true, // stdout
			time.Duration(0),
		)

		// Start the notification goroutine manually
		go sendNotifications(shoutrrr)

		// Cancel the context directly while goroutine is waiting in select
		shoutrrr.cancel()

		// Wait for the goroutine to finish (done channel should be signaled)
		synctest.Wait()

		// Verify done channel received
		select {
		case <-shoutrrr.done:
			// Good
		default:
			t.Fatal("expected done channel to be signaled")
		}
	})
}

// TestGracefulTerminationDuringMessageProcessing verifies that the worker
// finishes the message it is sending and then ends when the context is
// canceled.
func TestGracefulTerminationDuringMessageProcessing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shoutrrr, blockingRouter, err := sendNotificationsWithBlockingRouter(t)
		if err != nil {
			t.Fatal(err)
		}

		// Unlock the router to allow the message processing to complete
		blockingRouter.unlock <- true

		// Wait for the notification to be sent
		synctest.Wait()

		// Verify that the message was sent
		select {
		case sent := <-blockingRouter.sent:
			if !sent {
				t.Fatal("expected message to be sent")
			}
		default:
			t.Fatal("expected message to be sent")
		}

		// Cancel context to test graceful termination
		shoutrrr.cancel()

		// Wait for goroutine to finish
		synctest.Wait()

		// Verify done channel signaled
		select {
		case <-shoutrrr.done:
			// Good
		default:
			t.Fatal("expected done channel to be signaled")
		}
	})
}

// TestContextCancellationIndependentOfStopChannel verifies that canceling the
// context ends the worker without closing the stop channel.
func TestContextCancellationIndependentOfStopChannel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shoutrrr := createTestNotifier(
			newTestLogger(t),
			[]string{"logger://"},
			allButTrace,
			true,
			format.StaticData{},
			true, // stdout
			time.Duration(0),
		)

		// Start the notification goroutine manually
		go sendNotifications(shoutrrr)

		// Test that context cancellation works without closing stop channel
		shoutrrr.cancel()

		// Wait for goroutine to finish via done channel
		synctest.Wait()

		// Verify done channel received
		select {
		case <-shoutrrr.done:
			// Good
		default:
			t.Fatal("expected done channel to be signaled")
		}

		// Verify stop channel is still open (not closed by context cancellation)
		select {
		case <-shoutrrr.stop:
			t.Fatal("stop channel should not be closed by context cancellation")
		default:
			// Good, stop channel is still open
		}
	})
}

// TestSendNotifications_SendsQueuedMessagesAfterStop verifies that the worker
// sends every message still queued when the stop signal arrives. The worker
// picks at random between a queued message and the stop signal, so with many
// messages queued it reaches the shutdown drain with near certainty, and each
// message must be sent on either path.
func TestSendNotifications_SendsQueuedMessagesAfterStop(t *testing.T) {
	t.Parallel()

	const queued = 64

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	router := &countingRouter{}
	notifier := &shoutrrrTypeNotifier{
		messages: make(chan string, queued),
		done:     make(chan struct{}, 1),
		stop:     make(chan struct{}),
		Router:   router,
		params:   &shoutrrrTypes.Params{},
		ctx:      ctx,
		cancel:   cancel,
		localLog: newTestLogger(t),
	}

	for i := range queued {
		notifier.messages <- fmt.Sprintf("message %d", i)
	}

	close(notifier.stop)

	sendNotifications(notifier)

	assert.Equal(t, int32(queued), router.sends.Load())
}

// TestSend_CanceledBeforeSend verifies that send does not call the router once
// the notifier's context is canceled.
func TestSend_CanceledBeforeSend(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	router := &countingRouter{}
	notifier := &shoutrrrTypeNotifier{
		Router: router,
		ctx:    ctx,
		cancel: cancel,
	}

	notifier.send("should not send")
	assert.Equal(t, int32(0), router.sends.Load())
}

// TestSendEntries_EmptyMessageLogsSkipAtDebug verifies that a template that
// renders an empty message skips the send with a single debug line, whatever
// the number of notification URLs. With report templates an empty message is
// the normal result of a scan without changes, so it must not log at info.
func TestSendEntries_EmptyMessageLogsSkipAtDebug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		urls []string
	}{
		{name: "one URL", urls: []string{"logger://"}},
		{name: "two URLs", urls: []string{"logger://", "logger://"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tpl, err := format.New(newTestLogger(t), "{{- /* renders nothing */ -}}", false)
			require.NoError(t, err)

			log, buf := newBufferLogger()
			notifier := &shoutrrrTypeNotifier{
				Urls:     tt.urls,
				template: tpl,
				localLog: log,
			}

			notifier.sendEntries(nil, nil)

			var skips int

			for _, entry := range logcapture.Entries[logcapture.Entry](t, buf.String()) {
				assert.NotEqual(t, "info", entry["level"], entry["message"])

				if entry["message"] == "Message empty, skipping send" {
					assert.Equal(t, "debug", entry["level"])

					skips++
				}
			}

			assert.Equal(t, 1, skips)
		})
	}
}
