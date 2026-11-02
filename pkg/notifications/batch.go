package notifications

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
func (n *shoutrrrTypeNotifier) StartNotification(suppressSummary bool) {
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
// When the report focuses on a single container (exactly one container across
// Updated/Restarted, or a single Skipped/Stale focus with no updates), matching
// queued entries are filtered out of the queue and sent with that report. This
// supports --notification-split-by-container without exposing entry state.
// Otherwise the entire queue is drained and sent.
//
// Parameters:
//   - report: Scan report to include.
func (n *shoutrrrTypeNotifier) SendNotification(report types.Report) {
	n.entriesMutex.Lock()

	var entries []*format.Entry

	focus, ok := singleFocusContainer(report)
	if ok {
		// Keep non-nil empty slices so Run continues treating batching as active
		// when every queued entry matched the focus.
		matched := make([]*format.Entry, 0)
		rest := make([]*format.Entry, 0)

		for _, e := range n.entries {
			if entryMatchesFocus(e, focus) {
				matched = append(matched, e)
			} else {
				rest = append(rest, e)
			}
		}

		n.entries = rest
		entries = matched
	} else {
		entries = n.entries
		n.entries = nil // Clear the queue after copying to prevent re-sending
	}

	n.entriesMutex.Unlock()

	n.ll().Debug().
		Int("entries_count", len(entries)).
		Bool("legacy_template", n.template.Legacy()).
		Bool("report_available", report != nil).
		Msg("SendNotification called - sending queued entries and report")

	// Deduplicate entries to prevent repeated messages when multiple containers
	// share the same image (e.g., two containers using nginx:latest will both
	// log "Found new image" but only one notification should be sent).
	entries = deduplicateEntries(entries)

	n.sendEntries(entries, report)
}

// FlushSplitByContainer sends one notification per distinct container value in the
// queued entries, then clears the queue.
//
// Used by the check API split path. This is intentionally a package-level helper
// (not on types.Notifier): the only production Notifier is *shoutrrrTypeNotifier.
// Adding a split method to the interface would force every mock or stub to implement
// it. Non-shoutrrr values fall back to a single SendNotification(nil).
//
// Parameters:
//   - notifier: Active notifier instance (typically *shoutrrrTypeNotifier).
func FlushSplitByContainer(notifier types.Notifier) {
	if notifier == nil {
		return
	}

	shoutrrr, ok := notifier.(*shoutrrrTypeNotifier)
	if !ok {
		notifier.SendNotification(nil)

		return
	}

	shoutrrr.entriesMutex.Lock()
	entries := shoutrrr.entries
	shoutrrr.entries = nil
	shoutrrr.entriesMutex.Unlock()

	if len(entries) == 0 {
		return
	}

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
		if len(containerEntries) > 0 {
			shoutrrr.sendEntries(containerEntries, nil)
		}
	}
}

// singleFocusContainer returns the sole focus container when the report is a
// state.SingleContainerReport (split path), or false for a full session report.
// Type assertion is required because a full report with one update is otherwise
// indistinguishable from a split single-container report.
func singleFocusContainer(report types.Report) (types.ContainerReport, bool) {
	if report == nil {
		return nil, false
	}

	scr, ok := report.(*state.SingleContainerReport)
	if !ok {
		return nil, false
	}

	if len(scr.UpdatedReports) == 1 {
		return scr.UpdatedReports[0], true
	}

	if len(scr.RestartedReports) == 1 {
		return scr.RestartedReports[0], true
	}

	if len(scr.SkippedReports) == 1 {
		return scr.SkippedReports[0], true
	}

	if len(scr.StaleReports) == 1 {
		return scr.StaleReports[0], true
	}

	return nil, false
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
func (n *shoutrrrTypeNotifier) ShouldSendNotification(report types.Report) bool {
	if report != nil && n.logLevel == zerolog.ErrorLevel {
		if len(report.Failed()) == 0 {
			return false
		}
	}

	return true
}
