package notify

import (
	"sync"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// batch holds the log entries queued for one owner's notification.
//
// An update session, the startup message or a check request owns a batch. It
// holds the pointer to its batch. It can then tell whether the batch is still
// the notifier's open one.
type batch struct {
	entries []*format.Entry // Queued log entries.
	// holders counts the check requests that hold the batch open. It is zero
	// for the batch of an update session or of startup. A check batch that
	// ended keeps a count of one.
	holders int
}

// StartNotification opens the batch of an update session or of startup.
//
// Log entries are then queued until the batch is sent. A batch an earlier owner
// left open is sent first, without a report. Its entries are either the lines
// logged before startup or the lines of check requests that this batch takes
// over. Those are sent as the checks' release would send them. The checks'
// release then sends nothing.
//
// Returns:
//   - func(): Ends the batch without sending it, if it is still open. Entries
//     no notification took are dropped. Calling it again has no effect.
func (n *Notifier) StartNotification() func() {
	owned := &batch{}

	n.entriesMutex.Lock()
	earlier := n.batch
	n.batch = owned
	n.entriesMutex.Unlock()

	// Sending after unlock keeps template work and internal logging outside the
	// queue lock.
	if earlier != nil {
		n.sendEnded(earlier)
	}

	return func() {
		dropped, ok := n.takeBatch(owned)
		if ok && len(dropped) > 0 {
			n.ll().Debug().
				Int("entries_count", len(dropped)).
				Msg("Discarded queued entries")
		}
	}
}

// HoldNotifications holds back the log entries of a check request.
//
// Checks that overlap share one batch. The last of them to end sends its
// entries without a report. It sends them in one notification by default. With
// the notification-split-by-container configuration option it sends one per
// container. Entries that name no container then go together. During an update
// session or startup the check's entries join that batch instead. Its owner
// decides what to send. A split update session sends only the entries that
// name a container it reports. When a session takes the checks' batch over,
// release sends nothing.
//
// Returns:
//   - func(): Ends the check's hold on its batch. Calling it again has no
//     effect.
func (n *Notifier) HoldNotifications() func() {
	n.entriesMutex.Lock()
	defer n.entriesMutex.Unlock()

	held := n.batch

	switch {
	case held == nil:
		held = &batch{}
		n.batch = held
	case held.holders == 0:
		// The batch of an update session or of startup. Its owner sends it.
		return func() {}
	default:
		// Another check's batch. It stays open until every check holding it
		// ends.
	}

	held.holders++

	return sync.OnceFunc(func() {
		n.entriesMutex.Lock()

		if n.batch != held {
			n.entriesMutex.Unlock()

			return
		}

		// The last check keeps its count. The ended batch is then still known
		// as a check's.
		if held.holders > 1 {
			held.holders--
			n.entriesMutex.Unlock()

			return
		}

		n.batch = nil
		n.entriesMutex.Unlock()

		n.sendEnded(held)
	})
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

	var matched []*format.Entry

	n.entriesMutex.Lock()

	// The batch stays open for the session's other split notifications.
	if n.batch != nil {
		rest := make([]*format.Entry, 0, len(n.batch.entries))

		for _, e := range n.batch.entries {
			if entryMatchesFocus(e, split.Focus) {
				matched = append(matched, e)
			} else {
				rest = append(rest, e)
			}
		}

		n.batch.entries = rest
	}

	n.entriesMutex.Unlock()

	n.sendDeduplicated(matched, report)
}

// Flush sends every queued message in one notification without a report and
// ends the open batch.
//
// Report templates then list the queued messages themselves. Callers use it
// when no scan produced a report, such as for the startup message. An empty
// queue sends nothing. Messages logged after it are sent one at a time until
// the next batch opens.
func (n *Notifier) Flush() {
	n.sendWithoutReport(n.takeEntries())
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

// takeEntries ends the open batch, whoever owns it.
//
// Returns:
//   - []*format.Entry: The entries that were queued. Nil when no batch is open.
func (n *Notifier) takeEntries() []*format.Entry {
	n.entriesMutex.Lock()
	defer n.entriesMutex.Unlock()

	open := n.batch
	n.batch = nil

	if open == nil {
		return nil
	}

	return open.entries
}

// takeBatch ends owned if it is still the open batch.
//
// Parameters:
//   - owned: The batch its owner opened.
//
// Returns:
//   - []*format.Entry: The entries owned holds.
//   - bool: True when owned was still open. False when another owner took the
//     batch over or owned already ended.
func (n *Notifier) takeBatch(owned *batch) ([]*format.Entry, bool) {
	n.entriesMutex.Lock()
	defer n.entriesMutex.Unlock()

	if n.batch != owned {
		return nil, false
	}

	n.batch = nil

	return owned.entries, true
}

// sendEnded sends the entries of a batch that ended without a report.
//
// A check request's batch is sent one notification per container with the
// notification-split-by-container configuration option. Any other batch is
// sent in one notification.
//
// Parameters:
//   - ended: A batch that is no longer the open one.
func (n *Notifier) sendEnded(ended *batch) {
	if ended.holders > 0 && n.splitByContainer {
		n.sendByContainer(ended.entries)

		return
	}

	n.sendWithoutReport(ended.entries)
}

// sendWithoutReport sends entries in one notification without a report.
//
// Report templates then list the entries themselves. No entries send nothing,
// since templates such as json.v1 would still render a document.
//
// Parameters:
//   - entries: Entries taken out of the queue.
func (n *Notifier) sendWithoutReport(entries []*format.Entry) {
	if len(entries) == 0 {
		return
	}

	n.sendDeduplicated(entries, nil)
}

// sendByContainer sends one notification without a report for each container
// the entries name. Entries that name no container are sent together.
//
// Parameters:
//   - entries: Entries taken out of the queue.
func (n *Notifier) sendByContainer(entries []*format.Entry) {
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
