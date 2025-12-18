// Package lifecycle manages execution of lifecycle hooks for Watchtower containers.
// It runs pre-check, post-check, pre-update, and post-update commands during updates.
//
// Key components:
//   - Execute Functions: Handle lifecycle hook execution (e.g., ExecutePreUpdateCommand).
//   - Client Integration: Uses types.Client for command execution.
//
// Usage example:
//
//	lifecycle.ExecutePreChecks(client, params)
//	success, err := lifecycle.ExecutePreUpdateCommand(client, container)
//	if err != nil {
//	    logrus.WithError(err).Error("Pre-update failed")
//	}
//
// The package integrates with types.Client, supports error handling, and uses logrus for logging.
package lifecycle
