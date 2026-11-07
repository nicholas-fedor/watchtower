package notify

import (
	"strings"
	"time"

	"github.com/nicholas-fedor/watchtower/internal/container/redact"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
)

// processSendErrors processes errors from router send operations.
//
// Parameters:
//   - notifier: Notifier instance.
//   - errs: Errors returned from router send.
func processSendErrors(notifier *Notifier, errs []error) {
	log := notifier.ll()
	failureCount := 0

	var authFailures, networkFailures, rateLimitFailures int

	for i, err := range errs {
		// Index guard against potential errs/urls length mismatch
		if i >= len(notifier.urls) {
			log.Error().
				Err(err).
				Int("index", i).
				Int("urls_length", len(notifier.urls)).
				Int("errs_length", len(errs)).
				Str("failure_type", "index_mismatch").
				Msg("Error index out of bounds for URLs slice")

			continue
		}

		// Increment failure count and prepare URL details for logging on notification failure.
		if err != nil {
			failureCount++
			scheme := getScheme(notifier.urls[i])
			sanitizedURL := redact.ServiceURL(notifier.urls[i])

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
			Int("total_urls", len(notifier.urls)).
			Int("failed_count", failureCount).
			Int("success_count", len(notifier.urls)-failureCount).
			Int("auth_failures", authFailures).
			Int("network_failures", networkFailures).
			Int("rate_limit_failures", rateLimitFailures).
			Msg("Notification send completed with failures")
	} else if len(notifier.urls) > 0 {
		log.Debug().
			Int("total_urls", len(notifier.urls)).
			Msg("Notification send completed successfully")
	}
}

// sendNotifications sends queued messages via the router.
//
// Parameters:
//   - notifier: Notifier instance.
func sendNotifications(notifier *Notifier) {
	log := notifier.ll()
	defer func() { notifier.done <- struct{}{} }()

	for {
		select {
		case msg := <-notifier.messages:
			// Log goroutine receipt of message
			log.Trace().
				Int("msg_length", len(msg)).
				Str("notification_type", shoutrrrType).
				Int("total_urls", len(notifier.urls)).
				Msg("Notification goroutine received message from channel")

			log.Debug().Str("message", msg).Msg("Sending notification")

			// Wait the configured delay before sending a message taken from the
			// queue. A stop signal ends the wait. Shutdown sends without the delay.
			select {
			case <-time.After(notifier.delay):
			case <-notifier.stop:
			}

			// Diagnostic logging: Log attempt details before sending
			log.Trace().
				Int("total_urls", len(notifier.urls)).
				Str("delay", notifier.delay.String()).
				Int("msg_length", len(msg)).
				Msg("Attempting to send notification to configured services")

			// Log before calling Router.Send
			log.Trace().
				Int("msg_length", len(msg)).
				Int("total_urls", len(notifier.urls)).
				Str("notification_type", shoutrrrType).
				Msg("Calling Router.Send with message")

			processSendErrors(notifier, notifier.sender.Send(msg, notifier.params))
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
						Int("total_urls", len(notifier.urls)).
						Bool("shutdown_mode", true).
						Msg("Processing remaining notification message during shutdown")

					log.Debug().Str("message", msg).Msg("Sending notification during shutdown")

					// Skip delay during shutdown to expedite processing

					// Diagnostic logging: Log attempt details before sending
					log.Trace().
						Int("total_urls", len(notifier.urls)).
						Str("delay", notifier.delay.String()).
						Int("msg_length", len(msg)).
						Msg("Attempting to send notification to configured services during shutdown")

					// Log before calling Router.Send
					log.Trace().
						Int("msg_length", len(msg)).
						Int("total_urls", len(notifier.urls)).
						Str("notification_type", shoutrrrType).
						Msg("Calling Router.Send with message during shutdown")

					processSendErrors(notifier, notifier.sender.Send(msg, notifier.params))
				default:
					// Channel is empty, all messages drained
					log.Debug().Msg("All remaining messages drained during shutdown")

					return
				}
			}
		}
	}
}

// sendEntries sends batched entries and optional report.
//
// Parameters:
//   - entries: Log entries to send.
//   - report: Optional scan report.
func (n *Notifier) sendEntries(entries []*format.Entry, report *state.Report) {
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

	// The worker sends nothing queued after Close. Skip the message.
	if n.closed.Load() {
		log.Debug().
			Int("entries_count", len(entries)).
			Int("msg_length", len(msg)).
			Str("channel_status", "closed").
			Msg("Notifier closed, skipping send")

		return
	}

	// Enqueue without blocking so a full queue never stalls the caller.
	select {
	case n.messages <- msg:
		log.Debug().
			Int("entries_count", len(entries)).
			Int("msg_length", len(msg)).
			Str("channel_status", "sent").
			Msg("Successfully sent message to notification channel")
	default:
		// The channel is full. Apply backpressure.
		log.Debug().
			Int("entries_count", len(entries)).
			Int("msg_length", len(msg)).
			Str("channel_status", "full").
			Msg("Channel full, skipping send (backpressure)")
	}
}
