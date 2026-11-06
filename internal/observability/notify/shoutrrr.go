package notify

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"

	"github.com/nicholas-fedor/watchtower/internal/container/redact"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
)

// shoutrrrType is the identifier for Shoutrrr notifications.
const shoutrrrType = "shoutrrr"

// messageChannelBufferSize defines the buffer size for the notification message channel.
const messageChannelBufferSize = 1000

// router defines the interface for sending Shoutrrr notifications.
//
// It abstracts the underlying service implementation.
type router interface {
	Send(message string, params *shoutrrrTypes.Params) []error
}

// Notifier sends notifications through Shoutrrr.
//
// It receives log events as a [zerolog.Hook] and queues them. A send renders
// the queued events with the notification template and delivers the message
// after the configured delay. Once [Notifier.RegisterHook] has returned, its
// methods are safe for concurrent use. [NewNotifier] builds it.
type Notifier struct {
	sender   router                // Router for sending messages.
	template *format.Template      // Template for message formatting.
	messages chan string           // Channel for message queuing.
	done     chan struct{}         // Signal for send completion.
	stop     chan struct{}         // Channel for stopping the notifier.
	params   *shoutrrrTypes.Params // Notification parameters.

	// localLog is a child logger with notify=no for internal logging (loop prevention).
	// Initialized at create time from the process logger and rebound in RegisterHook
	// to a child of the hooked logger. Never a package-level variable.
	// Still inherits hooks. Run short-circuits on notify=no (and fail-closed parse)
	// before taking entriesMutex, so internal logs never re-enter the queue or deadlock.
	// Concurrent application Logs are serialized only by entriesMutex on queue access.
	localLog *zerolog.Logger
	// batch holds the queued log entries of the open batch. Nil when log
	// entries are sent as they are logged.
	batch        *batch
	data         format.StaticData // Static notification data.
	urls         []string          // Notification service URLs.
	delay        time.Duration     // Delay between sends.
	entriesMutex sync.RWMutex      // Guards batch and its entries.
	hookOnce     sync.Once         // Ensures RegisterHook executes only once.
	closeOnce    sync.Once         // Ensures Close executes only once.
	// extractWarnOnce ensures the eventFieldMap failure warning is emitted only once.
	extractWarnOnce sync.Once // Guards the first extraction failure warning.
	// These fields must only be accessed via sync/atomic (e.g., atomic.Load/atomic.Store) to avoid data races.
	receiving atomic.Bool // Tracks if receiving logs.
	// These fields must only be accessed via sync/atomic (e.g., atomic.Load/atomic.Store) to avoid data races.
	closed atomic.Bool // Tracks if the notifier is closed.

	// splitByContainer sends a check request's notifications one per container.
	splitByContainer bool

	logLevel zerolog.Level // Minimum log level for notifications.
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
func (n *Notifier) GetNames() []string {
	names := make([]string, len(n.urls))
	for i, u := range n.urls {
		names[i] = getScheme(u)
	}

	return names
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
func (n *Notifier) RegisterHook(log *zerolog.Logger) {
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
			Strs("urls", redact.ServiceURLs(n.urls)).
			Msg("Added Shoutrrr notifier as zerolog hook, starting notification goroutine")

		// Send using a separate goroutine to avoid blocking the main process.
		go sendNotifications(n)
	})
}

// createNotifier initializes a Shoutrrr notifier.
//
// Parameters:
//   - log: Process logger for config-time logs.
//   - sender: Router that sends to every service in urls, in the same order.
//   - urls: Service URLs.
//   - level: Minimum log level for notifications.
//   - tplString: Template string.
//   - legacy: Use legacy template if true.
//   - data: Static notification data.
//   - delay: Delay between sends.
//
// Returns:
//   - *Notifier: Initialized notifier.
func createNotifier(
	log *zerolog.Logger,
	sender router,
	urls []string,
	level zerolog.Level,
	tplString string,
	legacy bool,
	data format.StaticData,
	delay time.Duration,
) *Notifier {
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

	// Set params with title if provided.
	params := &shoutrrrTypes.Params{}
	if data.Title != "" {
		params.SetTitle(data.Title)
	}

	return &Notifier{
		urls:   urls,   // Notification service URLs.
		sender: sender, // Router for sending messages.
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
		logLevel: level,    // Minimum log level for notifications.
		template: tpl,      // Template for message formatting.
		data:     data,     // Static notification data.
		params:   params,   // Notification parameters.
		delay:    delay,    // Delay between sends.
		localLog: localLog, // Loop-safe internal logger.
		// The lines logged before the startup message are queued until it.
		batch: &batch{},
	}
}

// Close gracefully shuts down the Shoutrrr notifier.
//
// It first sends the lines still held in an open batch without a report, as a
// new batch sends one an earlier owner left open. It then signals the
// notification goroutine to stop and waits for it to send every queued
// message, including one it is sending. These sends skip the configured delay.
// Each send is bounded by its service's time budget in Shoutrrr. Close returns
// at once when no goroutine was started. Messages sent after Close are
// dropped.
func (n *Notifier) Close() {
	log := n.ll()

	n.closeOnce.Do(func() {
		// Queue the held lines before the closed flag makes the send drop them.
		n.sendEnded(n.takeOpen())

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
func (n *Notifier) ll() *zerolog.Logger {
	if n != nil && n.localLog != nil {
		return n.localLog
	}

	nop := zerolog.Nop()

	return &nop
}
