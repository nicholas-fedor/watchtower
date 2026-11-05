package notify

import (
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// StartNotification begins queuing messages for batching.
//
// It sends any existing queued entries before resetting the entries slice to ensure a fresh queue for each run.
// When suppressSummary is true, skip sending queued entries.
// sendEntries is invoked only after releasing entriesMutex so template work and
// internal logging never hold the queue lock (avoids re-entrant hook deadlocks).
func (n *Notifier) StartNotification(suppressSummary bool) {
	n.entriesMutex.Lock()

	var toSend []*format.Entry
	// Capture residual entries to send after unlock when not suppressed.
	if len(n.entries) > 0 && !suppressSummary {
		toSend = n.entries
	}

	// Capture the count before resetting the slice.
	preResetCount := len(n.entries)

	// Reset the entries slice to an empty slice with initial capacity for new batching.
	n.entries = make([]*format.Entry, 0, initialEntriesCapacity)

	n.entriesMutex.Unlock()

	if len(toSend) > 0 {
		n.sendEntries(toSend, nil)
	}

	n.ll().Debug().
		Bool("legacy_template", n.template.Legacy()).
		Bool("receiving", n.receiving.Load()).
		Int("entries_count", preResetCount).
		Bool("suppress_summary", suppressSummary).
		Msg("StartNotification called - batching mode enabled")
}

// SendNotification sends queued messages with a report.
//
// A split report names the container it is about in its Focus field. For such
// a report, the queued entries that match the focus container are taken out of
// the queue and sent with it. This supports the notification-split-by-container
// configuration option without exposing entry state. Otherwise the entire
// queue is drained and sent. [Notifier.Flush] sends without a report.
//
// Parameters:
//   - report: Scan report to include.
func (n *Notifier) SendNotification(report types.Report) {
	split, ok := report.(*state.SingleContainerReport)
	if !ok || split.Focus == nil {
		n.sendDeduplicated(n.takeEntries(), report)

		return
	}

	n.entriesMutex.Lock()

	// Keep rest non-nil so Run continues treating batching as active when
	// every queued entry matched the focus.
	var matched []*format.Entry

	rest := make([]*format.Entry, 0, len(n.entries))

	for _, e := range n.entries {
		if entryMatchesFocus(e, split.Focus) {
			matched = append(matched, e)
		} else {
			rest = append(rest, e)
		}
	}

	n.entries = rest

	n.entriesMutex.Unlock()

	n.sendDeduplicated(matched, report)
}

// Flush sends every queued message without a report and empties the queue.
//
// Report templates then list the queued messages themselves. Callers use it
// when no scan produced a report, such as for the startup message. An empty
// queue sends nothing, since templates such as json.v1 would still render a
// document. Messages logged after it are sent one at a time until the next
// [Notifier.StartNotification].
func (n *Notifier) Flush() {
	entries := n.takeEntries()
	if len(entries) == 0 {
		return
	}

	n.sendDeduplicated(entries, nil)
}

// FlushSplitByContainer takes every entry out of the queue and sends one
// notification for each container the entries name. Entries that name no
// container are sent together.
//
// The check API calls it for the notification-split-by-container configuration
// option, since a check has no report that names a container.
func (n *Notifier) FlushSplitByContainer() {
	entries := n.takeEntries()
	byContainer := make(map[string][]*format.Entry)

	for _, entry := range entries {
		container, _ := entry.Data["container"].(string)
		if container == "" {
			name, ok := entry.Data["container_name"].(string)
			if ok && name != "" {
				container = name
			} else {
				container = "unknown"
			}
		}

		byContainer[container] = append(byContainer[container], entry)
	}

	for _, containerEntries := range byContainer {
		n.sendEntries(containerEntries, nil)
	}
}

// Discard empties the queue without sending it and ends batching.
//
// An update session calls it when it ends, after whatever it sent. The entries
// that no notification took are dropped. Messages logged after it are sent one
// at a time until the next [Notifier.StartNotification].
func (n *Notifier) Discard() {
	dropped := n.takeEntries()
	if len(dropped) == 0 {
		return
	}

	n.ll().Debug().
		Int("entries_count", len(dropped)).
		Msg("Discarded queued entries")
}

// entryMatchesFocus reports whether a queued entry belongs to the focus container.
func entryMatchesFocus(entry *format.Entry, focus types.ContainerReport) bool {
	if entry == nil || focus == nil {
		return false
	}

	name := focus.Name()
	if c, ok := entry.Data["container"].(string); ok && c == name {
		return true
	}

	if c, ok := entry.Data["container_name"].(string); ok && c == name {
		return true
	}

	// Image-level messages (e.g. cooldown) without a container field match by image name.
	_, hasContainer := entry.Data["container"]
	if !hasContainer {
		_, hasCN := entry.Data["container_name"]
		if !hasCN {
			img, ok := entry.Data["image"].(string)
			if ok && img != "" && img == focus.ImageName() {
				return true
			}
		}
	}

	return false
}

// ShouldSendNotification checks if a notification should be sent for the given report based on the notifier's log level.
//
// Parameters:
//   - report: The report to check.
//
// Returns:
//   - bool: True if notification should be sent, false otherwise.
func (n *Notifier) ShouldSendNotification(report types.Report) bool {
	if report != nil && n.logLevel == zerolog.ErrorLevel {
		if len(report.Failed()) == 0 {
			return false
		}
	}

	return true
}

// takeEntries empties the queue and ends batching, since the hook batches
// only while the queue is non-nil.
//
// Returns:
//   - []*format.Entry: The entries that were queued.
func (n *Notifier) takeEntries() []*format.Entry {
	n.entriesMutex.Lock()
	defer n.entriesMutex.Unlock()

	entries := n.entries
	n.entries = nil

	return entries
}

// sendDeduplicated sends entries with report after dropping repeated messages.
//
// Containers that share an image each log "Found new image". One notification
// carries that message once.
//
// Parameters:
//   - entries: Entries taken out of the queue.
//   - report: Scan report to include. It may be nil.
func (n *Notifier) sendDeduplicated(entries []*format.Entry, report types.Report) {
	n.ll().Debug().
		Int("entries_count", len(entries)).
		Bool("legacy_template", n.template.Legacy()).
		Bool("report_available", report != nil).
		Msg("Sending queued entries and report")

	n.sendEntries(deduplicateEntries(entries), report)
}
