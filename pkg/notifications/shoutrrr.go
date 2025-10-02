// Package notifications provides mechanisms for sending notifications via various services.
// This file implements the core Shoutrrr notification handling with templating and batching.
package notifications

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
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
// Uses mutex for thread-safe access to entries.
type shoutrrrTypeNotifier struct {
	Urls           []string              // Notification service URLs.
	Router         router                // Router for sending messages.
	entries        []*logrus.Entry       // Queued log entries.
	entriesMutex   sync.RWMutex          // Mutex for thread-safe access to entries.
	logLevel       logrus.Level          // Minimum log level for notifications.
	template       *template.Template    // Template for message formatting.
	messages       chan string           // Channel for message queuing.
	done           chan bool             // Signal for send completion.
	legacyTemplate bool                  // Use legacy log-only template if true.
	params         *shoutrrrTypes.Params // Notification parameters.
	data           StaticData            // Static notification data.
	receiving      bool                  // Tracks if receiving logs.
	delay          time.Duration         // Delay between sends.
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
	if n.receiving {
		return
	}

	n.receiving = true
	logrus.AddHook(n)
	LocalLog.WithField("urls", n.Urls).
		Debug("Added Shoutrrr notifier as logrus hook, starting notification goroutine")

	// Send using a separate goroutine to avoid blocking the main process.
	go sendNotifications(n)
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
		done:           make(chan bool),
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
	for msg := range notifier.messages {
		LocalLog.WithField("message", msg).Debug("Sending notification")
		time.Sleep(notifier.delay)
		errs := notifier.Router.Send(msg, notifier.params)

		for i, err := range errs {
			if err != nil {
				scheme := GetScheme(notifier.Urls[i])
				LocalLog.WithFields(logrus.Fields{
					"service": scheme,
					"index":   i,
				}).WithError(err).Error("Failed to send shoutrrr notification")
			}
		}
	}

	notifier.done <- true
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

	// Execute template with data.
	err := n.template.Execute(&body, dataSource)
	if err != nil {
		LocalLog.WithError(err).Debug("Template execution failed")

		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	msg := body.String()
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

	LocalLog.Debug("Queuing notification message")

	n.messages <- msg
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
	n.entries = nil
	n.entriesMutex.Unlock()
	n.sendEntries(entries, report)
}

// Close stops queuing and waits for sends to complete.
//
// It closes the messages channel and blocks until done.
func (n *shoutrrrTypeNotifier) Close() {
	close(n.messages)

	LocalLog.Debug("Waiting for the notification goroutine to finish")

	<-n.done
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

	n.entriesMutex.Lock()

	if n.entries != nil {
		n.entries = append(n.entries, entry) // Queue if batching.
	} else {
		n.sendEntries([]*logrus.Entry{entry}, nil) // Send immediately if not batching.
	}

	n.entriesMutex.Unlock()

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
