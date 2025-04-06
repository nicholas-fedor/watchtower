// Package types defines core interfaces and structs for Watchtower.
// It provides abstractions for containers, notifications, session reporting, and registry interactions.
//
// Key components:
//   - Container: Interface for container lifecycle and metadata operations.
//   - Notifier: Interface for notification services with templating and batching.
//   - Report: Interface for session results (scanned, updated, etc.).
//   - UpdateParams: Struct for configuring update behavior.
//   - Filter: Function type for container filtering.
//   - ContainerReport: Interface for individual container session status.
//   - RegistryCredentials: Struct for registry authentication.
//
// Usage example:
//
//	var c types.Container
//	params := types.UpdateParams{Filter: someFilter, Cleanup: true}
//	notifier := someNotifierImpl{}
//	notifier.StartNotification()
//	report := session.NewReport(progress)
//	notifier.SendNotification(report)
//
// The package integrates with container, notifications, session, and registry packages,
// using logrus for logging where implemented.
package types
