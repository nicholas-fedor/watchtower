// Package actions provides core logic for Watchtower’s container update operations.
// It handles container staleness checks, updates, and lifecycle management.
//
// Key components:
//   - Update: Scans and updates containers based on parameters.
//   - CheckForSanity: Validates environment for rolling restarts.
//   - CheckForMultipleWatchtowerInstances: Ensures single Watchtower instance.
//
// Usage example:
//
//	report, err := actions.Update(client, params)
//	if err != nil {
//	    logrus.WithError(err).Error("Update failed")
//	}
//	if err := actions.CheckForSanity(client, filter, true); err != nil {
//	    logrus.WithError(err).Error("Sanity check failed")
//	}
//
// The package integrates with container, session, sorter, and lifecycle packages,
// using logrus for logging operations and errors.
package actions
