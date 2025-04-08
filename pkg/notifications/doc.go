// Package notifications provides mechanisms for sending notifications via various services in Watchtower.
// It supports multiple notifiers (e.g., email, Slack, Microsoft Teams, Gotify) with templating, batching,
// and JSON marshaling, integrating with Shoutrrr for service delivery.
//
// Key components:
//   - Notifier Creation: Configures notifiers from flags (notifier.go).
//   - Service Notifiers: Implement specific services (email.go, slack.go, etc.).
//   - Shoutrrr Integration: Handles message sending and batching (shoutrrr.go).
//   - JSON Marshaling: Formats notification data (json.go).
//   - Preview: Renders notification previews (preview.go).
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
