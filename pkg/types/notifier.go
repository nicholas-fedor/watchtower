package types

import "github.com/sirupsen/logrus"

// Notifier defines the common interface for notification services.
type Notifier interface {
	StartNotification()                 // Begin queuing messages.
	SendNotification(reportType Report) // Send queued messages with report.
	AddLogHook()                        // Add as logrus hook.
	GetNames() []string                 // Service names.
	GetURLs() []string                  // Service URLs.
	Close()                             // Stop and flush notifications.

	// GetEntries returns all queued logrus entries that have been captured during the session.
	// This is used for notification splitting by container in log mode, allowing notifiers
	// to filter and send entries specific to individual containers rather than all entries together.
	GetEntries() []*logrus.Entry

	// SendFilteredEntries sends a subset of log entries with an optional report.
	// This method enables fine-grained notifications where only entries relevant to specific
	// containers are sent, supporting the --notification-split-by-container feature in log mode.
	// The report parameter may be nil when sending filtered log entries without session context.
	SendFilteredEntries(
		entries []*logrus.Entry,
		report Report,
	)
}
