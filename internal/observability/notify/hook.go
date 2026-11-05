package notify

import (
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
)

// Run implements the [zerolog.Hook] interface for *Notifier.
//
// It queues or immediately sends log events as notifications.
//
// Loop prevention:
//   - Events carrying notify=no (including all localLog output) are ignored before
//     any lock is taken, so nested localLog calls never re-enter the queue.
//   - If field extraction fails (nil or unparseable event buffer), the event is
//     fail-closed and not queued. This never risks missing notify=no and looping.
//
// Concurrent application log events are allowed. entriesMutex serializes queue
// access only. sendEntries and debug logging run after unlock so the lock is never
// held across template work or re-entrant hook paths that would deadlock.
//
// Events below the configured notification level are ignored.
//
// Only zerolog events on the hooked process logger are captured. Domain producers
// such as the container client ("Found new image", "Removing image") log through the process
// logger from NewClient so log-mode notifications receive them. Report-mode
// notifications (--notification-report) render from types.Report.
//
// Parameters:
//   - event: Zerolog event (fields are read from the in-progress JSON buffer).
//   - level: Event level.
//   - message: Log message.
func (n *Notifier) Run(event *zerolog.Event, level zerolog.Level, message string) {
	if n.closed.Load() {
		return
	}

	// Filter by notification level (zerolog: higher ordinal is more severe).
	if level < n.logLevel {
		return
	}

	// Fail-closed: if we cannot parse fields, we cannot prove notify!=no, so skip queueing.
	// Done before entriesMutex so nested localLog hooks never contend with a held lock.
	fields, timestamp, ok := eventFieldMap(event)
	if !ok {
		// Emit a warning only on the first extraction failure to aid debugging
		// without flooding logs on every subsequent malformed event.
		n.extractWarnOnce.Do(func() {
			n.ll().Warn().
				Msg("Failed to extract fields from log event; notification queueing skipped for this event")
		})

		return
	}

	notify, has := fields["notify"].(string)
	if has && notify == "no" {
		return // Skip non-notify entries (loop prevention).
	}
	// notify is an internal control field, not template data.
	delete(fields, "notify")

	entry := &format.Entry{
		Message: message,
		Data:    fields,
		Time:    timestamp,
		Level:   format.LevelString(level),
	}

	// Hold the mutex only for queue mutation. Log and send outside the lock.
	// Concurrent Runs from different goroutines are fine — they serialize here only.
	n.entriesMutex.Lock()
	batching := n.batch != nil

	var entriesCount int

	if batching {
		n.batch.entries = append(n.batch.entries, entry)
		entriesCount = len(n.batch.entries)
	}
	n.entriesMutex.Unlock()

	if batching {
		// Name the field entry_level. Zerolog reserves level for event severity.
		// Using Str("level", ...) would overwrite Debug or Info in logfmt output.
		n.ll().Debug().
			Str("message", entry.Message).
			Str("entry_level", entry.Level).
			Int("entries_count", entriesCount).
			Bool("legacy_template", n.template.Legacy()).
			Msg("Log entry queued for batching")

		return
	}

	// Same entry_level naming as the batching path above.
	n.ll().Debug().
		Str("message", entry.Message).
		Str("entry_level", entry.Level).
		Bool("legacy_template", n.template.Legacy()).
		Msg("Log entry sent immediately (not batching)")
	n.sendEntries([]*format.Entry{entry}, nil)
}
