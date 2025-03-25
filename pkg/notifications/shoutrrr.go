// Package notifications provides mechanisms for sending notifications via various services.
// This file implements the core Shoutrrr notification handling with templating and batching.
package notifications

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/nicholas-fedor/shoutrrr"
	"github.com/sirupsen/logrus"

	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"

	"github.com/nicholas-fedor/watchtower/pkg/notifications/templates"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// LocalLog is a logrus logger that does not send entries as notifications.
// It’s used for internal logging to avoid notification loops.
var LocalLog = logrus.WithField("notify", "no")

const (
	shoutrrrType = "shoutrrr"
)

// router defines the interface for sending Shoutrrr notifications.
// It abstracts the underlying service implementation.
type router interface {
	Send(message string, params *shoutrrrTypes.Params) []error
}

// shoutrrrTypeNotifier implements the Notifier and logrus.Hook interfaces for Shoutrrr notifications.
// It manages message queuing, templating, and sending with configurable delay and parameters.
type shoutrrrTypeNotifier struct {
	Urls           []string
	Router         router
	entries        []*logrus.Entry
	logLevel       logrus.Level
	template       *template.Template
	messages       chan string
	done           chan bool
	legacyTemplate bool
	params         *shoutrrrTypes.Params
	data           StaticData
	receiving      bool
	delay          time.Duration
}

// GetScheme extracts the scheme part of a Shoutrrr URL.
// It returns "invalid" if no scheme is found.
func GetScheme(url string) string {
	schemeEnd := strings.Index(url, ":")
	if schemeEnd <= 0 {
		return "invalid"
	}

	return url[:schemeEnd]
}

// GetNames returns a list of notification service names derived from URLs.
// It extracts the scheme from each URL as the service name.
func (n *shoutrrrTypeNotifier) GetNames() []string {
	names := make([]string, len(n.Urls))
	for i, u := range n.Urls {
		names[i] = GetScheme(u)
	}

	return names
}

// GetURLs returns the list of URLs for configured notification services.
// It provides the raw URLs used for sending notifications.
func (n *shoutrrrTypeNotifier) GetURLs() []string {
	return n.Urls
}

// AddLogHook adds the notifier as a logrus hook to receive log messages.
// It starts a goroutine for processing notifications if not already receiving.
func (n *shoutrrrTypeNotifier) AddLogHook() {
	if n.receiving {
		return
	}

	n.receiving = true
	logrus.AddHook(n)

	// Do the sending in a separate goroutine, so we don't block the main process.
	go sendNotifications(n)
}

// initialEntriesCapacity defines the initial capacity for the entries slice in the Shoutrrr notifier.
// It sets a reasonable default for expected log entry batch sizes.
const initialEntriesCapacity = 10

// createNotifier initializes a new Shoutrrr notifier for sending notifications through multiple services.
// It configures the notifier with the provided URLs, log level, template string, and static data, setting up
// a router using shoutrrr.NewSender to handle message delivery. The function parses the template string
// or falls back to a default if parsing fails or no template is provided, supporting both legacy (log-only)
// and full report modes. It optionally enables stdout logging if specified, otherwise directs Shoutrrr logs
// to the logrus trace level. The notifier is returned fully initialized with channels for message queuing
// and parameters like a custom title if present in the static data.
func createNotifier(
	urls []string,
	level logrus.Level,
	tplString string,
	legacy bool,
	data StaticData,
	stdout bool,
	delay time.Duration,
) *shoutrrrTypeNotifier {
	tpl, err := getShoutrrrTemplate(tplString, legacy)
	if err != nil {
		logrus.Errorf(
			"Could not use configured notification template: %s. Using default template",
			err,
		)
	}

	var logger shoutrrrTypes.StdLogger
	if stdout {
		logger = log.New(os.Stdout, ``, 0)
	} else {
		logger = log.New(logrus.StandardLogger().WriterLevel(logrus.TraceLevel), "Shoutrrr: ", 0)
	}

	router, err := shoutrrr.NewSender(logger, urls...)
	if err != nil {
		logrus.Fatalf("Failed to initialize Shoutrrr notifications: %s\n", err.Error())
	}

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

// sendNotifications processes queued messages and sends them via the router.
// It applies the configured delay between sends and logs errors locally.
func sendNotifications(notifier *shoutrrrTypeNotifier) {
	for msg := range notifier.messages {
		time.Sleep(notifier.delay)
		errs := notifier.Router.Send(msg, notifier.params)

		for i, err := range errs {
			if err != nil {
				scheme := GetScheme(notifier.Urls[i])
				// Use fmt so it doesn't trigger another notification.
				LocalLog.WithFields(logrus.Fields{
					"service": scheme,
					"index":   i,
				}).WithError(err).Error("Failed to send shoutrrr notification")
			}
		}
	}

	notifier.done <- true
}

// buildMessage constructs a notification message from the provided data using the configured template.
// It supports both legacy (log entries only) and full report templating.
func (n *shoutrrrTypeNotifier) buildMessage(data Data) (string, error) {
	var body bytes.Buffer

	var templateData any = data
	if n.legacyTemplate {
		templateData = data.Entries
	}

	if err := n.template.Execute(&body, templateData); err != nil {
		return "", fmt.Errorf("failed to execute notification template: %w", err)
	}

	return body.String(), nil
}

// sendEntries sends a batch of log entries and an optional report as a notification.
// It skips sending if the resulting message is empty.
func (n *shoutrrrTypeNotifier) sendEntries(entries []*logrus.Entry, report types.Report) {
	msg, err := n.buildMessage(Data{n.data, entries, report})

	if msg == "" {
		// Log in go func in case we entered from Fire to avoid stalling
		go func() {
			if err != nil {
				LocalLog.WithError(err).Fatal("Notification template error")
			} else if len(n.Urls) > 1 {
				LocalLog.Info("Skipping notification due to empty message")
			}
		}()

		return
	}
	n.messages <- msg
}

// StartNotification begins queuing up messages to send them as a batch.
// It initializes the entries slice if not already set.
func (n *shoutrrrTypeNotifier) StartNotification() {
	if n.entries == nil {
		n.entries = make([]*logrus.Entry, 0, initialEntriesCapacity)
	}
}

// SendNotification sends the queued messages as a notification.
// It clears the queue after sending.
func (n *shoutrrrTypeNotifier) SendNotification(report types.Report) {
	n.sendEntries(n.entries, report)
	n.entries = nil
}

// Close prevents further messages from being queued and waits until all queued messages are sent.
// It closes the messages channel and blocks until the sending goroutine completes.
func (n *shoutrrrTypeNotifier) Close() {
	close(n.messages)

	// Use fmt so it doesn't trigger another notification.
	LocalLog.Info("Waiting for the notification goroutine to finish")

	<-n.done
}

// Levels returns the log levels that trigger notifications.
// It includes all levels up to and including the configured log level.
func (n *shoutrrrTypeNotifier) Levels() []logrus.Level {
	return logrus.AllLevels[:n.logLevel+1]
}

// Fire handles a new log message as a logrus hook.
// It queues or sends the message based on whether batching is active.
func (n *shoutrrrTypeNotifier) Fire(entry *logrus.Entry) error {
	if entry.Data["notify"] == "no" {
		// Skip logging if explicitly tagged as non-notify
		return nil
	}

	if n.entries != nil {
		n.entries = append(n.entries, entry)
	} else {
		// Log output generated outside a cycle is sent immediately.
		n.sendEntries([]*logrus.Entry{entry}, nil)
	}

	return nil
}

// getShoutrrrTemplate retrieves or generates a template for Shoutrrr notifications.
// It uses a provided template string or falls back to a default based on legacy mode.
func getShoutrrrTemplate(tplString string, legacy bool) (*template.Template, error) {
	tplBase := template.New("").Funcs(templates.Funcs)

	if builtin, found := commonTemplates[tplString]; found {
		logrus.WithField(`template`, tplString).Debug(`Using common template`)
		tplString = builtin
	}

	var tpl *template.Template

	var err error

	// If we succeed in getting a non-empty template configuration
	// try to parse the template string.
	if tplString != "" {
		tpl, err = tplBase.Parse(tplString)
		if err != nil {
			return nil, fmt.Errorf("failed to parse notification template string: %w", err)
		}
	}

	// If a template wasn't configured (empty string), fall back to using the default template.
	if tplString == "" {
		defaultKey := `default`
		if legacy {
			defaultKey = `default-legacy`
		}

		tpl = template.Must(tplBase.Parse(commonTemplates[defaultKey]))
	}

	return tpl, nil
}
