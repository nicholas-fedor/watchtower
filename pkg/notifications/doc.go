// Package notifications provides mechanisms for sending notifications via various services in Watchtower.
// It integrates with Shoutrrr for service delivery, supporting custom templates, batching, and JSON marshaling.
//
// Key components:
//   - Notifier Creation: Configures notifiers from flags (notifier.go).
//   - Shoutrrr Integration: Handles message sending and batching (shoutrrr.go).
//   - JSON Marshaling: Formats notification data (json.go).
//   - Preview: Renders notification previews (preview.go).
//
// Note: The legacy notification types (email, slack, msteams, gotify) and their individual flags
// (e.g., --notification-email-from, --notification-slack-hook-url) are deprecated.
// Use --notification-url with the appropriate shoutrrr URL scheme instead.
// See the deprecation notices on specific types and functions for details.
//
// Usage example:
//
//	notifier := notifications.NewNotifier(cmd)
//	notifier.StartNotification()
//	notifier.SendNotification(report)
//	notifier.Close()
//
// The package uses Shoutrrr for service abstraction, supports custom templates, and allows configuration
// via command-line flags or environment variables, with logging handled through logrus.
package notifications
