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
	// ended keeps a count of one. It is then still known as a check's.
	holders int
}

// StartNotification opens the batch of the startup message.
//
// Log entries are then queued until the batch is sent. A batch an earlier owner
// left open is sent first, without a report. Its entries are either the lines
// logged before startup or the lines of check requests that this batch takes
// over. Those are sent as the checks' release would send them. The checks'
// release then sends nothing. An update session opens its batch with
// [Notifier.StartSessionNotification] instead.
//
// Returns:
//   - func(): Ends the batch, if it is still open. The entries no notification
//     took are sent without a report, as a check request's entries are.
//     Calling it again has no effect.
func (n *Notifier) StartNotification() func() {
	owned := n.openBatch()

	return func() { n.endBatch(owned) }
}

// StartSessionNotification opens the batch of an update session.
//
// The batch opens as one from [Notifier.StartNotification] does. The session
// sends its report through the returned send, which reaches only this batch.
//
// Returns:
//   - func(types.Report): Sends a report with the entries the batch queued,
//     while the batch is open. A split report names its container in its Focus
//     field. Its notification takes only that container's entries. The batch
//     stays open for the session's other split notifications. Any other report
//     takes every queued entry and ends the batch. Once the batch has ended,
//     send sends nothing.
//   - func(): Ends the batch, as the end from [Notifier.StartNotification] does.
func (n *Notifier) StartSessionNotification() (func(types.Report), func()) {
	owned := n.openBatch()

	return func(report types.Report) { n.sendReport(owned, report) },
		func() { n.endBatch(owned) }
}

// HoldNotifications holds back the log entries of a check request.
//
// Checks that overlap share one batch. Each check's release sends the entries
// queued so far without a report. No entry then waits longer than the check
// that is running when it is logged. The batch closes when the last check ends.
// Entries are sent in one notification by default. With the
// notification-split-by-container configuration option they are sent one per
// container. Entries that name no container then go together. During an update
// session or startup the check's entries join that batch instead. Its owner
// sends them. When a session takes the checks' batch over, release sends
// nothing.
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

		queued := held.entries
		held.entries = nil

		// The batch stays open for the checks that still hold it.
		if held.holders > 1 {
			held.holders--
		} else {
			n.batch = nil
		}

		n.entriesMutex.Unlock()

		n.sendUnreported(queued)
	})
}

// entryContainer returns the name of the container a queued entry is about.
//
// The container field names it. Image cleanup lines name it in the
// container_name field instead. An entry that names only an image is not
// about a single container, since several containers can share an image.
//
// Parameters:
//   - entry: A queued log entry.
//
// Returns:
//   - string: The container's name. Empty when the entry names no container.
func entryContainer(entry *format.Entry) string {
	if container, _ := entry.Data["container"].(string); container != "" {
		return container
	}

	container, _ := entry.Data["container_name"].(string)

	return container
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

// takeOpen ends the open batch, whoever owns it.
//
// Returns:
//   - *batch: The batch that was open. Nil when no batch is open.
func (n *Notifier) takeOpen() *batch {
	n.entriesMutex.Lock()
	defer n.entriesMutex.Unlock()

	open := n.batch
	n.batch = nil

	return open
}

// openBatch makes a new batch the open one, owned by its caller.
//
// The batch an earlier owner left open is sent first, without a report.
//
// Returns:
//   - *batch: The new open batch.
func (n *Notifier) openBatch() *batch {
	owned := &batch{}

	n.entriesMutex.Lock()
	earlier := n.batch
	n.batch = owned
	n.entriesMutex.Unlock()

	// Sending after unlock keeps template work and internal logging outside the
	// queue lock.
	n.sendEnded(earlier)

	return owned
}

// endBatch ends owned if it is still the open batch.
//
// The entries no notification took are sent without a report. A batch that
// already ended or that another owner took over sends nothing.
//
// Parameters:
//   - owned: The batch its owner opened.
func (n *Notifier) endBatch(owned *batch) {
	n.entriesMutex.Lock()

	if n.batch != owned {
		n.entriesMutex.Unlock()

		return
	}

	n.batch = nil

	n.entriesMutex.Unlock()

	// Sending after unlock keeps template work and internal logging outside the
	// queue lock.
	n.sendUnreported(owned.entries)
}

// sendReport sends report with the entries owned queued, while owned is the
// open batch.
//
// A split report names the container it is about in its Focus field. Only the
// queued entries that name that container are taken out of the batch and sent
// with it. The batch stays open for the session's other split notifications.
// Any other report takes every queued entry and ends the batch. Once owned has
// ended or another owner took it over, nothing is sent.
//
// Parameters:
//   - owned: The batch its owner opened.
//   - report: Scan report to include.
func (n *Notifier) sendReport(owned *batch, report types.Report) {
	split, ok := report.(*state.SingleContainerReport)
	focused := ok && split.Focus != nil

	n.entriesMutex.Lock()

	if n.batch != owned {
		n.entriesMutex.Unlock()

		return
	}

	var entries []*format.Entry

	if focused {
		// An unnamed focus would otherwise match the entries that name no
		// container, so it takes none.
		if name := split.Focus.Name(); name != "" {
			rest := make([]*format.Entry, 0, len(owned.entries))

			for _, e := range owned.entries {
				if entryContainer(e) == name {
					entries = append(entries, e)
				} else {
					rest = append(rest, e)
				}
			}

			owned.entries = rest
		}
	} else {
		entries = owned.entries
		n.batch = nil
	}

	n.entriesMutex.Unlock()

	n.sendDeduplicated(entries, report)
}

// sendEnded sends the entries of a batch that ended without a report.
//
// A check request's batch is sent as its checks would send it. Any other batch
// is sent in one notification. A nil batch sends nothing.
//
// Parameters:
//   - ended: A batch that is no longer the open one, or nil.
func (n *Notifier) sendEnded(ended *batch) {
	if ended == nil {
		return
	}

	if ended.holders > 0 {
		n.sendUnreported(ended.entries)

		return
	}

	n.sendWithoutReport(ended.entries)
}

// sendUnreported sends entries that no report carries.
//
// They go out one notification per container with the
// notification-split-by-container configuration option. Otherwise they go out
// in one notification.
//
// Parameters:
//   - entries: Entries taken out of a batch.
func (n *Notifier) sendUnreported(entries []*format.Entry) {
	if n.splitByContainer {
		n.sendByContainer(entries)

		return
	}

	n.sendWithoutReport(entries)
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

	// Entries that name no container share the empty key. No container name
	// can take it.
	for _, entry := range entries {
		container := entryContainer(entry)
		byContainer[container] = append(byContainer[container], entry)
	}

	for _, containerEntries := range byContainer {
		n.sendDeduplicated(containerEntries, nil)
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
