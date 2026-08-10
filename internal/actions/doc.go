// Package actions provides core logic for Watchtower's container update operations.
// It handles container staleness checks, updates, and lifecycle management.
//
// Key components:
//   - Update: Scans and updates containers based on parameters.
//   - ValidateRollingRestartDependencies: Validates environment for rolling restarts.
//   - CheckForMultipleWatchtowerContainers: Ensures single Watchtower container.
//   - RunUpdatesWithNotifications: Performs container updates and sends notifications about the results.
//   - CleanupImages: Removes specified image IDs from the Docker environment.
//   - UpdateImplicitRestart: Marks containers linked to restarting ones for proper restart order.
//
// Logging: every function that logs requires a non-nil *zerolog.Logger (first parameter
// or Logger field on params structs). Callers (typically cmd) construct the logger via
// internal/logging and pass it explicitly. There is no package-level or global logger.
//
// Usage example:
//
//	report, _, err := actions.Update(log, ctx, client, params)
//	if err != nil {
//	    log.Error().Err(err).Msg("Update failed")
//	}
//	useComposeDependsOn := true
//	err = actions.ValidateRollingRestartDependencies(log, ctx, client, filter, useComposeDependsOn)
//	if err != nil {
//	    log.Error().Err(err).Msg("Sanity check failed")
//	}
//	runParams := actions.RunUpdatesWithNotificationsParams{
//		Logger:                       log,
//		Client:                       client,
//		Notifier:                     notifier,
//		NotificationSplitByContainer: false,
//		NotificationReport:           false,
//		Update: types.UpdateParams{
//			Filter:  filter,
//			Cleanup: true,
//			Timeout: 30 * time.Second,
//		},
//	}
//	metric := actions.RunUpdatesWithNotifications(ctx, runParams)
//
// The package integrates with the container package for Docker operations, session package for update reporting, sorter package for container ordering, and lifecycle package for pre/post-update
// hooks, using github.com/rs/zerolog for logging operations and errors.
package actions
