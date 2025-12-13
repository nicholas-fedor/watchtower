// Package session manages container states and reporting during a Watchtower update session.
// It tracks container progress, categorizes outcomes, and generates reports for scanned, updated,
// failed, skipped, stale, fresh, and restarted containers.
//
// Key components:
//   - State: Enum for container states (e.g., Updated, Failed).
//   - ContainerStatus: Tracks individual container details and state.
//   - Progress: Maps container statuses during a session.
//   - Report: Categorizes and sorts container outcomes.
//
// Usage example:
//
//	progress := session.Progress{}
//	progress.AddScanned(container, newImageID)
//	progress.MarkForUpdate(container.ID())
//	report := progress.Report()
//	scanned := report.Scanned()
//
// The package integrates with types.Container and uses logrus for logging session events.
package session
