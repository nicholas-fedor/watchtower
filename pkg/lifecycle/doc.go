// Package lifecycle manages execution of lifecycle hooks for Watchtower containers.
// It runs pre-check, post-check, pre-update, and post-update commands during updates.
//
// Key components:
//   - Execute Functions: Handle lifecycle hook execution (e.g., ExecutePreUpdateCommand).
//   - Client Integration: Uses container.Client for command execution.
//
// Usage example:
//
//	// log is the process *zerolog.Logger. ctx is the request/operation context.
//	lifecycle.ExecutePreChecks(log, ctx, client, params)
//	success, err := lifecycle.ExecutePreUpdateCommand(log, ctx, client, container, uid, gid)
//	if err != nil {
//	    log.Error().Err(err).Msg("Pre-update failed")
//	}
//
// The package integrates with container.Client, supports error handling, and uses zerolog for logging.
package lifecycle
