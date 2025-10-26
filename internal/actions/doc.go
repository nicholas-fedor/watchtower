// Package actions provides core logic for Watchtower’s container update operations.
// It handles container staleness checks, updates, and lifecycle management.
//
// Key components:
//   - Update: Scans and updates containers based on parameters.
//   - CheckForSanity: Validates environment for rolling restarts.
//   - CheckForMultipleWatchtowerInstances: Ensures single Watchtower instance.
//   - RunUpdatesWithNotifications: Performs container updates and sends notifications about the results.
//   - CleanupImages: Removes specified image IDs from the Docker environment.
//   - UpdateImplicitRestart: Marks containers linked to restarting ones for proper restart order.
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
//	params := actions.RunUpdatesWithNotificationsParams{
//		Client:                       client,
//		Notifier:                     notifier,
//		NotificationSplitByContainer: false,
//		NotificationReport:           false,
//		Filter:                       filter,
//		Cleanup:                      true,
//		NoRestart:                    false,
//		MonitorOnly:                  false,
//		LifecycleHooks:               false,
//		RollingRestart:               false,
//		LabelPrecedence:              false,
//		NoPull:                       false,
//		Timeout:                      30 * time.Second,
//		LifecycleUID:                 0,
//		LifecycleGID:                 0,
//		CPUCopyMode:                  "",
//	}
//	metric := actions.RunUpdatesWithNotifications(params)
//
// The package integrates with the container package for Docker operations, session package for update reporting, sorter package for container ordering, and lifecycle package for pre/post-update hooks, using logrus for logging operations and errors.
package actions
