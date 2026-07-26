// Package notifications provides the notification client for sending Watchtower update messages.
// It integrates with Shoutrrr for service delivery, supporting custom templates, batching, and JSON marshaling.
//
// Key components:
//   - NewNotifier: Constructs the client from internal/config/notify.Notify (notifier.go).
//   - NewNotifierFromFlags: Test helper that reads Cobra flags then calls NewNotifier.
//   - Shoutrrr Integration: Handles message sending and batching (shoutrrr.go).
//   - JSON Marshaling: Formats notification data (json.go).
//   - Preview: Renders notification previews (preview.go).
//
// Note: The legacy notification types (email, slack, msteams, gotify) and their individual flags
// (e.g., --notification-email-from, --notification-slack-hook-url) are deprecated.
// Use --notification-url with the appropriate shoutrrr URL scheme instead.
// See the deprecation notices on specific types and functions for details.
//
// Usage example (after config.Load):
//
//	notifier := notifications.NewNotifier(cfg.Notify)
//	notifier.StartNotification()
//	notifier.SendNotification(report)
//	notifier.Close()
//
// The package uses Shoutrrr for service abstraction and custom templates, with logging via logrus.
package notifications
