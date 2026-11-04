package notify

import (
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nicholas-fedor/shoutrrr"
	"github.com/rs/zerolog"

	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"

	"github.com/nicholas-fedor/watchtower/internal/container/redact"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
)

// shoutrrrType is the identifier for Shoutrrr notifications.
const shoutrrrType = "shoutrrr"

// initialEntriesCapacity defines the initial capacity for the entries slice in the Shoutrrr notifier.
//
// It sets a reasonable default for expected log entry batch sizes.
const initialEntriesCapacity = 10

// messageChannelBufferSize defines the buffer size for the notification message channel.
const messageChannelBufferSize = 1000

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
// Implements zerolog.Hook so log events are pushed via Run.
type shoutrrrTypeNotifier struct {
	Urls         []string              // Notification service URLs.
	Router       router                // Router for sending messages.
	entries      []*format.Entry       // Queued log entries.
	entriesMutex sync.RWMutex          // Mutex for thread-safe access to entries.
	logLevel     zerolog.Level         // Minimum log level for notifications.
	template     *format.Template      // Template for message formatting.
	messages     chan string           // Channel for message queuing.
	done         chan struct{}         // Signal for send completion.
	stop         chan struct{}         // Channel for stopping the notifier.
	params       *shoutrrrTypes.Params // Notification parameters.
	data         format.StaticData     // Static notification data.
	// These fields must only be accessed via sync/atomic (e.g., atomic.Load/atomic.Store) to avoid data races.
	receiving atomic.Bool   // Tracks if receiving logs.
	delay     time.Duration // Delay between sends.
	hookOnce  sync.Once     // Ensures RegisterHook executes only once.
	closeOnce sync.Once     // Ensures Close executes only once.
	// extractWarnOnce ensures the eventFieldMap failure warning is emitted only once.
	extractWarnOnce sync.Once // Guards the first extraction failure warning.
	// These fields must only be accessed via sync/atomic (e.g., atomic.Load/atomic.Store) to avoid data races.
	closed atomic.Bool // Tracks if the notifier is closed.

	// localLog is a child logger with notify=no for internal logging (loop prevention).
	// Initialized at create time from the process logger and rebound in RegisterHook
	// to a child of the hooked logger. Never a package-level variable.
	// Still inherits hooks. Run short-circuits on notify=no (and fail-closed parse)
	// before taking entriesMutex, so internal logs never re-enter the queue or deadlock.
	// Concurrent application Logs are serialized only by entriesMutex on queue access.
	localLog *zerolog.Logger
}

// getScheme extracts the scheme from a Shoutrrr URL.
//
// Parameters:
//   - url: URL to parse.
//
// Returns:
//   - string: Scheme or "invalid" if none found.
func getScheme(url string) string {
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
		names[i] = getScheme(u)
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

// RegisterHook attaches this notifier as a zerolog.Hook on the given logger instance.
//
// It updates *log in place to the hooked logger so the composition root continues
// using the same pointer for subsequent application logging. A notify=no child of
// the hooked logger is stored for internal loop-safe logging. Registration is
// idempotent via hookOnce.
//
// Parameters:
//   - log: Process logger pointer. Replaced with the hooked instance.
func (n *shoutrrrTypeNotifier) RegisterHook(log *zerolog.Logger) {
	if log == nil {
		return
	}

	n.hookOnce.Do(func() {
		n.receiving.Store(true)

		// Hook returns Logger by value. Write back so the composition root's pointer is hooked.
		hooked := log.Hook(n)
		*log = hooked

		local := log.With().Str("notify", "no").Logger()
		n.localLog = &local

		n.localLog.Debug().
			Strs("urls", redact.ServiceURLs(n.Urls)).
			Msg("Added Shoutrrr notifier as zerolog hook, starting notification goroutine")

		// Send using a separate goroutine to avoid blocking the main process.
		go sendNotifications(n)
	})
}

// createNotifier initializes a Shoutrrr notifier.
//
// Parameters:
//   - log: Process logger for config-time logs and Shoutrrr's log records.
//   - urls: Service URLs.
//   - level: Minimum log level for notifications.
//   - tplString: Template string.
//   - legacy: Use legacy template if true.
//   - data: Static notification data.
//   - stdout: Print the logger service's text to stdout if true.
//   - delay: Delay between sends.
//
// Returns:
//   - *shoutrrrTypeNotifier: Initialized notifier.
func createNotifier(
	log *zerolog.Logger,
	urls []string,
	level zerolog.Level,
	tplString string,
	legacy bool,
	data format.StaticData,
	stdout bool,
	delay time.Duration,
) *shoutrrrTypeNotifier {
	// Child logger that must never re-enter the notification hook.
	local := log.With().Str("notify", "no").Logger()
	localLog := &local

	// Parse or fallback to default template.
	tpl, err := format.New(localLog, tplString, legacy)
	if err != nil {
		localLog.Error().Err(err).
			Msg("Could not use configured notification template, falling back to default")

		tpl = format.Default(legacy)
	}

	// Shoutrrr's records go to the loop-safe logger at their level. The logger
	// service's text goes to stdout when stdout is set. Otherwise it goes to the
	// same logger without a level.
	var text io.Writer = localLog

	prefix := shoutrrrLogPrefix
	if stdout {
		text, prefix = os.Stdout, ""
	}

	// Initialize sender with default options.
	router, err := shoutrrr.NewSenderWithOptions(
		newShoutrrrLogger(localLog, text, prefix),
		shoutrrrTypes.SenderOptions{},
		urls...,
	)
	if err != nil {
		localLog.Fatal().Err(err).Msg("Failed to initialize Shoutrrr notifications")
	}

	// Set params with title if provided.
	params := &shoutrrrTypes.Params{}
	if data.Title != "" {
		params.SetTitle(data.Title)
	}

	return &shoutrrrTypeNotifier{
		Urls:   urls,   // Notification service URLs.
		Router: router, // Router for sending messages.
		messages: make(
			chan string,
			messageChannelBufferSize,
		), // Channel buffer size for notification messages
		done: make(
			chan struct{},
			1,
		), // Signal for send completion.
		stop: make(
			chan struct{},
		), // Channel for stopping the notifier.
		logLevel: level,                                            // Minimum log level for notifications.
		template: tpl,                                              // Template for message formatting.
		data:     data,                                             // Static notification data.
		params:   params,                                           // Notification parameters.
		delay:    delay,                                            // Delay between sends.
		entries:  make([]*format.Entry, 0, initialEntriesCapacity), // Queued log entries.
		localLog: localLog,                                         // Loop-safe internal logger.
	}
}

// Close gracefully shuts down the Shoutrrr notifier.
//
// It signals the notification goroutine to stop and waits for it to send every
// queued message, including one it is sending. These sends skip the configured
// delay. Each send is bounded by its service's time budget in Shoutrrr. Close
// returns at once when no goroutine was started. Messages sent after Close are
// dropped.
func (n *shoutrrrTypeNotifier) Close() {
	log := n.ll()

	n.closeOnce.Do(func() {
		n.closed.Store(true)

		// Without a worker goroutine there is nothing to wait for.
		if !n.receiving.Load() {
			log.Debug().Msg("No notification worker running")

			return
		}

		// Signal goroutine to stop processing new messages.
		log.Debug().Msg("Closing stop channel to signal shutdown")
		close(n.stop)

		// Wait for the worker goroutine to exit. The messages channel stays
		// open, because a concurrent sendEntries may still enqueue to it.
		log.Debug().Msg("Waiting for the notification goroutine to finish")
		<-n.done
	})
}

// ll returns the notifier's loop-safe logger, falling back to a discarded logger.
//
// Returns:
//   - *zerolog.Logger: Child logger with notify=no, or a nop logger when unset.
func (n *shoutrrrTypeNotifier) ll() *zerolog.Logger {
	if n != nil && n.localLog != nil {
		return n.localLog
	}

	nop := zerolog.Nop()

	return &nop
}
