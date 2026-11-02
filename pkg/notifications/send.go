package notifications

import (
	"strings"
	"time"

	"github.com/nicholas-fedor/watchtower/internal/container/redact"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// processSendErrors processes errors from router send operations.
//
// Parameters:
//   - notifier: Notifier instance.
//   - errs: Errors returned from router send.
func processSendErrors(notifier *shoutrrrTypeNotifier, errs []error) {
	log := notifier.ll()
	failureCount := 0

	var authFailures, networkFailures, rateLimitFailures int

	for i, err := range errs {
		// Index guard against potential errs/Urls length mismatch
		if i >= len(notifier.Urls) {
			log.Error().
				Err(err).
				Int("index", i).
				Int("urls_length", len(notifier.Urls)).
				Int("errs_length", len(errs)).
				Str("failure_type", "index_mismatch").
				Msg("Error index out of bounds for URLs slice")

			continue
		}

		// Increment failure count and prepare URL details for logging on notification failure.
		if err != nil {
			failureCount++
			scheme := getScheme(notifier.Urls[i])
			sanitizedURL := redact.ServiceURL(notifier.Urls[i])

			// Diagnostic logging: Categorize failure types
			errStr := err.Error()

			errStrLower := strings.ToLower(errStr) // Compute lowercase once for efficiency
			switch {
			case strings.Contains(errStrLower, "unauthorized") ||
				strings.Contains(errStrLower, "authentication") ||
				strings.Contains(errStrLower, "invalid token") ||
				strings.Contains(errStrLower, "invalid api") ||
				strings.Contains(errStrLower, "invalid key") ||
				strings.Contains(errStrLower, "invalid credentials"):
				authFailures++

				log.Warn().
					Err(err).
					Str("service", scheme).
					Int("index", i).
					Str("url", sanitizedURL).
					Str("failure_type", "authentication").
					Msg("Authentication failure detected - check API keys/tokens")
			case strings.Contains(errStrLower, "timeout") ||
				strings.Contains(errStrLower, "connection") ||
				strings.Contains(errStrLower, "network"):
				networkFailures++

				log.Warn().
					Err(err).
					Str("service", scheme).
					Int("index", i).
					Str("url", sanitizedURL).
					Str("failure_type", "network").
					Msg("Network connectivity failure detected - check internet connection")
			case strings.Contains(errStrLower, "rate limit") ||
				strings.Contains(errStrLower, "too many requests"):
				rateLimitFailures++

				log.Warn().
					Err(err).
					Str("service", scheme).
					Int("index", i).
					Str("url", sanitizedURL).
					Str("failure_type", "rate_limit").
					Msg("Rate limiting detected - consider increasing delays or reducing frequency")
			default:
				log.Error().
					Err(err).
					Str("service", scheme).
					Int("index", i).
					Str("url", sanitizedURL).
					Str("failure_type", "unknown").
					Msg("Failed to send shoutrrr notification")
			}
		}
	}

	// Diagnostic logging: Summary with categorized failures
	if failureCount > 0 {
		log.Warn().
			Int("total_urls", len(notifier.Urls)).
			Int("failed_count", failureCount).
			Int("success_count", len(notifier.Urls)-failureCount).
			Int("auth_failures", authFailures).
			Int("network_failures", networkFailures).
			Int("rate_limit_failures", rateLimitFailures).
			Msg("Notification send completed with failures")
	} else if len(notifier.Urls) > 0 {
		log.Debug().
			Int("total_urls", len(notifier.Urls)).
			Msg("Notification send completed successfully")
	}
}

// sendNotifications sends queued messages via the router.
//
// Parameters:
//   - notifier: Notifier instance.
func sendNotifications(notifier *shoutrrrTypeNotifier) {
	log := notifier.ll()
	defer func() { notifier.done <- struct{}{} }()

	for {
		select {
		case msg := <-notifier.messages:
			// Log goroutine receipt of message
			log.Trace().
				Int("msg_length", len(msg)).
				Str("notification_type", shoutrrrType).
				Int("total_urls", len(notifier.Urls)).
				Msg("Notification goroutine received message from channel")

			log.Debug().Str("message", msg).Msg("Sending notification")

			// Only delay if a positive delay is configured.
			// Use a context-aware select to allow interruption when the context is canceled.
			if notifier.delay > 0 {
				timer := time.NewTimer(notifier.delay)

				select {
				case <-timer.C:
					// Delay completed normally - stop the timer
					timer.Stop()
				case <-notifier.ctx.Done():
					// Context canceled - stop the timer and return early
					timer.Stop()
					log.Debug().Msg("Context canceled during delay, skipping send")

					return
				}
			}

			// Diagnostic logging: Log attempt details before sending
			log.Trace().
				Int("total_urls", len(notifier.Urls)).
				Str("delay", notifier.delay.String()).
				Int("msg_length", len(msg)).
				Msg("Attempting to send notification to configured services")

			// Log before calling Router.Send
			log.Trace().
				Int("msg_length", len(msg)).
				Int("total_urls", len(notifier.Urls)).
				Str("notification_type", shoutrrrType).
				Msg("Calling Router.Send with message")

			notifier.send(msg)
		case <-notifier.stop:
			// Shutdown mode: drain all remaining messages from the channel
			log.Debug().Msg("Shutdown signal received, draining remaining messages without delay")

			for {
				select {
				case msg := <-notifier.messages:
					// Log goroutine receipt of message during shutdown
					log.Trace().
						Int("msg_length", len(msg)).
						Str("notification_type", shoutrrrType).
						Int("total_urls", len(notifier.Urls)).
						Bool("shutdown_mode", true).
						Msg("Processing remaining notification message during shutdown")

					log.Debug().Str("message", msg).Msg("Sending notification during shutdown")

					// Skip delay during shutdown to expedite processing

					// Diagnostic logging: Log attempt details before sending
					log.Trace().
						Int("total_urls", len(notifier.Urls)).
						Str("delay", notifier.delay.String()).
						Int("msg_length", len(msg)).
						Msg("Attempting to send notification to configured services during shutdown")

					// Log before calling Router.Send
					log.Trace().
						Int("msg_length", len(msg)).
						Int("total_urls", len(notifier.Urls)).
						Str("notification_type", shoutrrrType).
						Msg("Calling Router.Send with message during shutdown")

					notifier.send(msg)
				default:
					// Channel is empty, all messages drained
					log.Debug().Msg("All remaining messages drained during shutdown")

					return
				}
			}
		case <-notifier.ctx.Done():
			// Context canceled
			log.Debug().Msg("Context canceled, stopping notification goroutine")

			return
		}
	}
}

// send sends a message via the notification router.
//
// Cancellation before Send skips the delivery. An in-flight Send is abandoned
// after shutdownGracePeriod so Close cannot block indefinitely.
//
// Parameters:
//   - msg: Message to send.
func (n *shoutrrrTypeNotifier) send(msg string) {
	// Skip delivery when Close already canceled the worker.
	select {
	case <-n.ctx.Done():
		n.ll().Debug().Err(n.ctx.Err()).Msg("Notification send canceled")

		return
	default:
	}

	errsCh := make(chan []error, 1)

	go func() {
		errsCh <- n.Router.Send(msg, n.params)
	}()

	select {
	case errs := <-errsCh:
		processSendErrors(n, errs)
	case <-n.ctx.Done():
		timer := time.NewTimer(shutdownGracePeriod)
		defer timer.Stop()

		select {
		case errs := <-errsCh:
			processSendErrors(n, errs)
		case <-timer.C:
			n.ll().Debug().Err(n.ctx.Err()).Msg("Notification send canceled")
		}
	}
}

// sendEntries sends batched entries and optional report.
//
// Parameters:
//   - entries: Log entries to send.
//   - report: Optional scan report.
func (n *shoutrrrTypeNotifier) sendEntries(entries []*format.Entry, report types.Report) {
	log := n.ll()

	msg, err := n.template.Render(log, format.Data{StaticData: n.data, Entries: entries, Report: report})
	if err != nil {
		log.Debug().
			Err(err).
			Str("message", msg).
			Msg("Preparing to send entries")
	} else {
		log.Debug().
			Str("message", msg).
			Msg("Preparing to send entries")
	}

	if msg == "" {
		if err != nil {
			log.Error().Err(err).Msg("Notification template error")
		}

		log.Debug().Msg("Message empty, skipping send")

		return
	}

	// Use select with non-blocking send to coordinate with shutdown.
	// This ensures we can't enqueue messages after shutdown has begun.
	select {
	case n.messages <- msg:
		// Message sent successfully to channel
		log.Debug().
			Int("entries_count", len(entries)).
			Int("msg_length", len(msg)).
			Str("channel_status", "sent").
			Msg("Successfully sent message to notification channel")
	default:
		// Non-blocking send failed - check if closed or done before returning.
		// This check is done AFTER the send attempt to catch the race condition
		// where Close() signaled stop but sendEntries was already in progress.
		if n.closed.Load() {
			log.Debug().
				Int("entries_count", len(entries)).
				Int("msg_length", len(msg)).
				Str("channel_status", "closed").
				Msg("Notifier closed, skipping send")

			return
		}

		// Check if worker goroutine has exited
		select {
		case <-n.done:
			log.Debug().
				Int("entries_count", len(entries)).
				Int("msg_length", len(msg)).
				Str("channel_status", "worker_done").
				Msg("Worker goroutine done, skipping send")

			return
		default:
			// Channel is full (not closed, not done), apply backpressure
			log.Debug().
				Int("entries_count", len(entries)).
				Int("msg_length", len(msg)).
				Str("channel_status", "full").
				Msg("Channel full, skipping send (backpressure)")
		}
	}
}
