package types

// Notifier defines the common interface for notification services.
type Notifier interface {
	StartNotification()                 // Begin queuing messages.
	SendNotification(reportType Report) // Send queued messages with report.
	AddLogHook()                        // Add as logrus hook.
	GetNames() []string                 // Service names.
	GetURLs() []string                  // Service URLs.
	Close()                             // Stop and flush notifications.
}
