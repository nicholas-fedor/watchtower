package notify

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
			t,
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
			t,
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
			t,
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
			t,
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
			t,
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
			t,
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
			notifier := createTestNotifier(t, log, []string{"logger://"}, allButTrace, true, format.StaticData{}, false, 0)
			notifier.Urls = []string{tt.url}

			processSendErrors(notifier, []error{tt.err})

			output := logBuffer.String()
			assert.Contains(t, output, `"url":"`+tt.wantURL+`"`)
			assert.NotContains(t, output, "secret")
		})
	}
}

// TestSlowNotificationSent verifies that a message is sent once the router
// finishes sending it.
func TestSlowNotificationSent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		shoutrrr, blockingRouter := sendNotificationsWithBlockingRouter(t)

		// Unlock the router
		blockingRouter.unlock <- struct{}{}

		// Wait for the notification to be sent
		synctest.Wait()

		select {
		case <-blockingRouter.sent:
		default:
			t.Fatal("expected notification to be sent")
		}

		shoutrrr.Close()
	})
}

// TestSendNotifications_SendsQueuedMessagesAfterStop verifies that the worker
// sends every message still queued when the stop signal arrives. The worker
// picks at random between a queued message and the stop signal, so with many
// messages queued it reaches the shutdown drain with near certainty, and each
// message must be sent on either path.
func TestSendNotifications_SendsQueuedMessagesAfterStop(t *testing.T) {
	t.Parallel()

	queued := queuedMessages(64)

	router := &recordingRouter{}
	notifier := createTestNotifier(t, newTestLogger(t), []string{}, allButTrace, true, format.StaticData{}, false, 0)
	notifier.Router = router

	for _, message := range queued {
		notifier.messages <- message
	}

	close(notifier.stop)

	sendNotifications(notifier)

	assert.Equal(t, queued, router.sent())
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
