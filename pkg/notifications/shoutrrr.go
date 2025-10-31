// Package notifications provides mechanisms for sending notifications via various services.
// This file implements the core Shoutrrr notification handling with templating and batching.
package notifications

import (
	"bytes"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/nicholas-fedor/shoutrrr"
	"github.com/sirupsen/logrus"

	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"

	"github.com/nicholas-fedor/watchtower/pkg/notifications/templates"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// shoutrrrType is the identifier for Shoutrrr notifications.
const shoutrrrType = "shoutrrr"

// initialEntriesCapacity defines the initial capacity for the entries slice in the Shoutrrr notifier.
//
// It sets a reasonable default for expected log entry batch sizes.
const initialEntriesCapacity = 10

// maxURLLengthForLogging defines the maximum length of URLs displayed in logs to avoid exposing sensitive information.
const maxURLLengthForLogging = 50

// LocalLog is a logrus logger that does not send entries as notifications.
//
// It’s used for internal logging to avoid notification loops.
var LocalLog = logrus.WithField("notify", "no")

// router defines the interface for sending Shoutrrr notifications.
//
// It abstracts the underlying service implementation.
type router interface {
	Send(message string, params *shoutrrrTypes.Params) []error
}

// shoutrrrTypeNotifier manages Shoutrrr notifications.
//
// It handles queuing, templating, and sending with delay.
// Uses mutex for thread-safe access to entries and sync.Once for idempotent operations.
type shoutrrrTypeNotifier struct {
	Urls           []string              // Notification service URLs.
	Router         router                // Router for sending messages.
	entries        []*logrus.Entry       // Queued log entries.
	entriesMutex   sync.RWMutex          // Mutex for thread-safe access to entries.
	logLevel       logrus.Level          // Minimum log level for notifications.
	template       *template.Template    // Template for message formatting.
	messages       chan string           // Channel for message queuing.
	done           chan struct{}         // Signal for send completion.
	stop           chan struct{}         // Channel for stopping the notifier.
	legacyTemplate bool                  // Use legacy log-only template if true.
	params         *shoutrrrTypes.Params // Notification parameters.
	data           StaticData            // Static notification data.
	// These fields must only be accessed via sync/atomic (e.g., atomic.Load/atomic.Store) to avoid data races.
	receiving atomic.Bool   // Tracks if receiving logs.
	delay     time.Duration // Delay between sends.
	hookOnce  sync.Once     // Ensures AddLogHook executes only once.
	closeOnce sync.Once     // Ensures Close executes only once.
	// These fields must only be accessed via sync/atomic (e.g., atomic.Load/atomic.Store) to avoid data races.
	closed atomic.Bool // Tracks if the notifier is closed.
}

// GetScheme extracts the scheme from a Shoutrrr URL.
//
// Parameters:
//   - url: URL to parse.
//
// Returns:
//   - string: Scheme or "invalid" if none found.
func GetScheme(url string) string {
	schemeEnd := strings.Index(url, ":")
	if schemeEnd <= 0 {
		return "invalid"
	}

	return url[:schemeEnd]
}

// GetNames returns service names from URLs.
//
// Returns:
//   - []string: List of schemes from URLs.
func (n *shoutrrrTypeNotifier) GetNames() []string {
	names := make([]string, len(n.Urls))
	for i, u := range n.Urls {
		names[i] = GetScheme(u)
	}

	return names
}

// GetURLs returns the configured service URLs.
//
// Returns:
//   - []string: List of URLs.
func (n *shoutrrrTypeNotifier) GetURLs() []string {
	return n.Urls
}

// AddLogHook enables the notifier as a logrus hook.
//
// It starts a send goroutine if not already active.
func (n *shoutrrrTypeNotifier) AddLogHook() {
	n.hookOnce.Do(func() {
		n.receiving.Store(true)
		logrus.AddHook(n)
		LocalLog.WithField("urls", n.Urls).
			Debug("Added Shoutrrr notifier as logrus hook, starting notification goroutine")

		// Send using a separate goroutine to avoid blocking the main process.
		go sendNotifications(n)
	})
}

// createNotifier initializes a Shoutrrr notifier.
//
// Parameters:
//   - urls: Service URLs.
//   - level: Minimum log level.
//   - tplString: Template string.
//   - legacy: Use legacy template if true.
//   - data: Static notification data.
//   - stdout: Log to stdout if true.
//   - delay: Delay between sends.
//
// Returns:
//   - *shoutrrrTypeNotifier: Initialized notifier.
func createNotifier(
	urls []string,
	level logrus.Level,
	tplString string,
	legacy bool,
	data StaticData,
	stdout bool,
	delay time.Duration,
) *shoutrrrTypeNotifier {
	// Parse or fallback to default template.
	tpl, err := getShoutrrrTemplate(tplString, legacy)
	if err != nil {
		LocalLog.WithError(err).
			Error("Could not use configured notification template, falling back to default")
	}

	// Set logger based on stdout flag.
	var logger shoutrrrTypes.StdLogger
	if stdout {
		logger = log.New(os.Stdout, ``, 0)
	} else {
		logger = log.New(logrus.StandardLogger().WriterLevel(logrus.TraceLevel), "Shoutrrr: ", 0)
	}

	// Initialize sender.
	router, err := shoutrrr.NewSender(logger, urls...)
	if err != nil {
		LocalLog.WithError(err).Fatal("Failed to initialize Shoutrrr notifications")
	}

	// Set params with title if provided.
	params := &shoutrrrTypes.Params{}
	if data.Title != "" {
		params.SetTitle(data.Title)
	}

	return &shoutrrrTypeNotifier{
		Urls:           urls,
		Router:         router,
		messages:       make(chan string, 1),
		done:           make(chan struct{}, 1),
		stop:           make(chan struct{}),
		logLevel:       level,
		template:       tpl,
		legacyTemplate: legacy,
		data:           data,
		params:         params,
		delay:          delay,
		entries:        make([]*logrus.Entry, 0, initialEntriesCapacity),
	}
}

// sendNotifications sends queued messages via the router.
//
// Parameters:
//   - notifier: Notifier instance.
func sendNotifications(notifier *shoutrrrTypeNotifier) {
	defer func() { notifier.done <- struct{}{} }()

	for {
		select {
		case msg := <-notifier.messages:
			// Log goroutine receipt of message
			LocalLog.WithFields(logrus.Fields{
				"msg_length":        len(msg),
				"notification_type": shoutrrrType,
				"total_urls":        len(notifier.Urls),
			}).Trace("Notification goroutine received message from channel")

			LocalLog.WithField("message", msg).Debug("Sending notification")
			time.Sleep(notifier.delay)

			// Diagnostic logging: Log attempt details before sending
			LocalLog.WithFields(logrus.Fields{
				"total_urls": len(notifier.Urls),
				"delay":      notifier.delay.String(),
				"msg_length": len(msg),
			}).Trace("Attempting to send notification to configured services")

			// Log before calling Router.Send
			LocalLog.WithFields(logrus.Fields{
				"msg_length":        len(msg),
				"total_urls":        len(notifier.Urls),
				"notification_type": shoutrrrType,
			}).Trace("Calling Router.Send with message")

			errs := notifier.Router.Send(msg, notifier.params)

			failureCount := 0

			var authFailures, networkFailures, rateLimitFailures int

			for i, err := range errs {
				// Index guard against potential errs/Urls length mismatch
				if i >= len(notifier.Urls) {
					LocalLog.WithFields(logrus.Fields{
						"index":        i,
						"urls_length":  len(notifier.Urls),
						"errs_length":  len(errs),
						"failure_type": "index_mismatch",
					}).WithError(err).Error("Error index out of bounds for URLs slice")

					continue
				}

				if err != nil {
					failureCount++
					scheme := GetScheme(notifier.Urls[i])
					sanitizedURL := sanitizeURLForLogging(notifier.Urls[i])

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

						LocalLog.WithFields(logrus.Fields{
							"service":      scheme,
							"index":        i,
							"url":          sanitizedURL,
							"failure_type": "authentication",
						}).WithError(err).Warn("Authentication failure detected - check API keys/tokens")
					case strings.Contains(errStrLower, "timeout") ||
						strings.Contains(errStrLower, "connection") ||
						strings.Contains(errStrLower, "network"):
						networkFailures++

						LocalLog.WithFields(logrus.Fields{
							"service":      scheme,
							"index":        i,
							"url":          sanitizedURL,
							"failure_type": "network",
						}).WithError(err).Warn("Network connectivity failure detected - check internet connection")
					case strings.Contains(errStrLower, "rate limit") ||
						strings.Contains(errStrLower, "too many requests"):
						rateLimitFailures++

						LocalLog.WithFields(logrus.Fields{
							"service":      scheme,
							"index":        i,
							"url":          sanitizedURL,
							"failure_type": "rate_limit",
						}).WithError(err).Warn("Rate limiting detected - consider increasing delays or reducing frequency")
					default:
						LocalLog.WithFields(logrus.Fields{
							"service":      scheme,
							"index":        i,
							"url":          sanitizedURL,
							"failure_type": "unknown",
						}).WithError(err).Error("Failed to send shoutrrr notification - no retry logic implemented")
					}
				}
			}

			// Diagnostic logging: Summary with categorized failures
			if failureCount > 0 {
				LocalLog.WithFields(logrus.Fields{
					"total_urls":          len(notifier.Urls),
					"failed_count":        failureCount,
					"success_count":       len(notifier.Urls) - failureCount,
					"auth_failures":       authFailures,
					"network_failures":    networkFailures,
					"rate_limit_failures": rateLimitFailures,
				}).Warn("Notification send completed with failures - consider implementing retry logic for transient errors")
			} else if len(notifier.Urls) > 0 {
				LocalLog.WithField("total_urls", len(notifier.Urls)).Debug("Notification send completed successfully")
			}
		case <-notifier.stop:
			return
		}
	}
}

// buildMessage constructs a notification message from data.
//
// Parameters:
//   - data: Notification data.
//
// Returns:
//   - string: Rendered message.
//   - error: Non-nil if templating fails, nil on success.
func (n *shoutrrrTypeNotifier) buildMessage(data Data) (string, error) {
	var body bytes.Buffer

	dataSource := any(data)
	if n.legacyTemplate {
		dataSource = data.Entries // Use entries only for legacy mode.
	}

	// Log template processing start with nil-safe report access
	containerCount := 0

	reportAvailable := data.Report != nil
	if reportAvailable {
		containerCount = len(data.Report.All())
	}

	LocalLog.WithFields(logrus.Fields{
		"legacy_template":  n.legacyTemplate,
		"entries_count":    len(data.Entries),
		"container_count":  containerCount,
		"report_available": reportAvailable,
	}).Debug("Starting template processing for notification message")

	// Execute template with data.
	err := n.template.Execute(&body, dataSource)
	if err != nil {
		LocalLog.WithError(err).WithFields(logrus.Fields{
			"legacy_template": n.legacyTemplate,
			"template_name":   n.template.Name(),
		}).Debug("Template execution failed")

		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	msg := body.String()

	// Log template processing result
	LocalLog.WithFields(logrus.Fields{
		"msg_length":      len(msg),
		"legacy_template": n.legacyTemplate,
		"template_name":   n.template.Name(),
		"entries_count":   len(data.Entries),
	}).Debug("Template processing completed successfully")

	LocalLog.WithField("message", msg).Debug("Generated notification message")

	return msg, nil
}

// sendEntries sends batched entries and optional report.
//
// Parameters:
//   - entries: Log entries to send.
//   - report: Optional scan report.
func (n *shoutrrrTypeNotifier) sendEntries(entries []*logrus.Entry, report types.Report) {
	msg, err := n.buildMessage(Data{n.data, entries, report})

	LocalLog.WithError(err).
		WithFields(logrus.Fields{"message": msg}).
		Debug("Preparing to send entries")

	if msg == "" {
		// Log in go func in case we entered from Fire to avoid stalling
		go func() { // Avoid blocking if called from Fire.
			if err != nil {
				LocalLog.WithError(err).Fatal("Notification template error")
			} else if len(n.Urls) > 1 {
				LocalLog.Info("Skipping notification due to empty message")
			}
		}()

		LocalLog.Debug("Message empty, skipping send")

		return
	}

	// Log state check before queuing
	closed := n.closed.Load()
	LocalLog.WithFields(logrus.Fields{
		"closed":        closed,
		"receiving":     n.receiving.Load(),
		"entries_count": len(entries),
		"msg_length":    len(msg),
	}).Debug("Checking notifier state before queuing message")

	if closed {
		LocalLog.WithFields(logrus.Fields{
			"entries_count": len(entries),
			"msg_length":    len(msg),
		}).Debug("Notifier closed, skipping send")

		return
	}

	// Log queuing attempt
	LocalLog.WithFields(logrus.Fields{
		"entries_count":     len(entries),
		"msg_length":        len(msg),
		"notification_type": shoutrrrType,
	}).Debug("Queuing notification message to channel")

	select {
	case n.messages <- msg:
		// Message sent successfully to channel
		LocalLog.WithFields(logrus.Fields{
			"entries_count":  len(entries),
			"msg_length":     len(msg),
			"channel_status": "sent",
		}).Debug("Successfully sent message to notification channel")
	default:
		// Channel is closed or full, skip sending
		closedAfter := n.closed.Load()
		if closedAfter {
			LocalLog.WithFields(logrus.Fields{
				"entries_count":  len(entries),
				"msg_length":     len(msg),
				"channel_status": "closed",
			}).Debug("Channel closed, skipping send")

			return
		} else {
			LocalLog.WithFields(logrus.Fields{
				"entries_count":  len(entries),
				"msg_length":     len(msg),
				"channel_status": "full",
			}).Debug("Channel full, skipping send (backpressure)")
		}
	}
}

// StartNotification begins queuing messages for batching.
//
// It resets the entries slice if nil.
func (n *shoutrrrTypeNotifier) StartNotification() {
	n.entriesMutex.Lock()

	if n.entries == nil {
		n.entries = make([]*logrus.Entry, 0, initialEntriesCapacity)
	}

	n.entriesMutex.Unlock()
}

// SendNotification sends queued messages with a report.
//
// Parameters:
//   - report: Scan report to include.
//
// It clears the queue after sending.
func (n *shoutrrrTypeNotifier) SendNotification(report types.Report) {
	n.entriesMutex.Lock()
	entries := n.entries
	n.entries = nil // Clear the queue after copying to prevent re-sending
	n.entriesMutex.Unlock()
	n.sendEntries(entries, report)
}

// Close stops queuing and waits for sends to complete.
//
// It closes the stop channel and blocks until done if a goroutine is running.
// This method is idempotent and can be called multiple times safely.
func (n *shoutrrrTypeNotifier) Close() {
	n.closeOnce.Do(func() {
		n.closed.Store(true)

		if n.stop != nil {
			close(n.stop)
		}

		if n.receiving.Load() {
			LocalLog.Debug("Waiting for the notification goroutine to finish")

			<-n.done
		}
	})
}

// GetEntries returns a copy of the queued log entries.
//
// Returns:
//   - []*logrus.Entry: Copy of queued entries.
func (n *shoutrrrTypeNotifier) GetEntries() []*logrus.Entry {
	n.entriesMutex.RLock()
	defer n.entriesMutex.RUnlock()

	entries := make([]*logrus.Entry, len(n.entries))
	copy(entries, n.entries)

	return entries
}

// SendFilteredEntries sends filtered log entries with an optional report.
//
// Parameters:
//   - entries: Log entries to send.
//   - report: Optional scan report.
func (n *shoutrrrTypeNotifier) SendFilteredEntries(entries []*logrus.Entry, report types.Report) {
	n.sendEntries(entries, report)
}

// Levels returns log levels that trigger notifications.
//
// Returns:
//   - []logrus.Level: Levels up to configured log level.
func (n *shoutrrrTypeNotifier) Levels() []logrus.Level {
	return logrus.AllLevels[:n.logLevel+1]
}

// Fire handles a log entry as a logrus hook.
//
// Parameters:
//   - entry: Log entry to process.
//
// Returns:
//   - error: Always nil.
func (n *shoutrrrTypeNotifier) Fire(entry *logrus.Entry) error {
	if entry.Data["notify"] == "no" {
		return nil // Skip non-notify entries.
	}

	if n.closed.Load() {
		return nil // Skip if closed.
	}

	n.entriesMutex.Lock()
	defer n.entriesMutex.Unlock()

	if n.entries != nil {
		n.entries = append(n.entries, entry) // Queue if batching.
	} else {
		n.sendEntries([]*logrus.Entry{entry}, nil) // Send immediately if not batching.
	}

	return nil
}

// getShoutrrrTemplate retrieves or generates a template.
//
// Parameters:
//   - tplString: Template string.
//   - legacy: Use legacy mode if true.
//
// Returns:
//   - *template.Template: Parsed or default template.
//   - error: Non-nil if parsing fails, nil on success.
func getShoutrrrTemplate(tplString string, legacy bool) (*template.Template, error) {
	tplBase := template.New("").Funcs(templates.Funcs)

	// Use common template if specified.
	if builtin, found := commonTemplates[tplString]; found {
		LocalLog.WithField("template", tplString).Debug("Using common template")
		tplString = builtin
	}

	var tpl *template.Template

	var err error

	// Parse provided template or use default based on presence of tplString.
	switch {
	case tplString != "":
		// Parse provided template if non-empty.
		tpl, err = tplBase.Parse(tplString)
		if err != nil {
			LocalLog.WithError(err).Debug("Parse failed")

			return nil, fmt.Errorf("failed to parse template: %w", err)
		}
	default:
		// Fall back to default template.
		key := "default"
		if legacy {
			key = "default-legacy"
		}

		tpl = template.Must(tplBase.Parse(commonTemplates[key]))
	}

	return tpl, nil
}

// safeTruncate truncates a string to a maximum length for logging, appending "..." if truncated.
// Uses rune-aware truncation to avoid breaking UTF-8 sequences.
// If the string is shorter than or equal to maxURLLengthForLogging, returns it unchanged.
//
// Parameters:
//   - s: String to truncate.
//
// Returns:
//   - string: Truncated string or original if no truncation needed.
func safeTruncate(s string) string {
	runes := []rune(s)
	if len(runes) <= maxURLLengthForLogging {
		return s
	}

	return string(runes[:maxURLLengthForLogging]) + "..."
}

// sanitizeURLForLogging removes credentials and query parameters from URLs before truncating.
// Falls back to safeTruncate on parse errors to ensure safe logging.
//
// Parameters:
//   - rawURL: URL string to sanitize.
//
// Returns:
//   - string: Sanitized and truncated URL safe for logging.
func sanitizeURLForLogging(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		// Fallback to safe truncation if URL parsing fails
		return safeTruncate(rawURL)
	}

	// Remove user info (credentials)
	parsedURL.User = nil

	// Remove query parameters
	parsedURL.RawQuery = ""
	parsedURL.Fragment = ""

	// Remove path and opaque data (tokens/webhook IDs often live here)
	parsedURL.Path = ""
	parsedURL.RawPath = ""
	parsedURL.Opaque = ""

	// Reconstruct the URL without sensitive parts
	sanitized := parsedURL.String()

	return safeTruncate(sanitized)
}
