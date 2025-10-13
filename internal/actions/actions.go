package actions

import (
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/notifications"
	"github.com/nicholas-fedor/watchtower/pkg/session"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// RunUpdatesWithNotifications performs container updates and sends notifications about the results.
//
// It executes the update action with configured parameters, batches notifications, and returns a metric
// summarizing the session for monitoring purposes, ensuring users are informed of update outcomes.
//
// Parameters:
//   - client: The Docker client instance used for container operations.
//   - notifier: The notification system instance for sending update status messages.
//   - notificationSplitByContainer: Boolean flag enabling separate notifications for each updated container.
//   - notificationReport: Boolean flag enabling report-based notifications.
//   - filter: The types.Filter determining which containers are targeted for updates.
//   - cleanup: Boolean indicating whether to remove old images after updates.
//   - noRestart: Boolean flag preventing containers from being restarted after updates.
//   - timeout: Maximum duration allowed for container stop operations during updates.
//   - monitorOnly: Boolean flag enabling mode where Watchtower monitors without updating.
//   - lifecycleHooks: Boolean flag enabling execution of pre- and post-update lifecycle hooks.
//   - rollingRestart: Boolean flag enabling rolling restarts for sequential updates.
//   - labelPrecedence: Boolean flag giving container label settings priority over global flags.
//   - noPull: Boolean flag skipping image pulls during updates.
//   - lifecycleUID: Default UID for running lifecycle hooks.
//   - lifecycleGID: Default GID for running lifecycle hooks.
//   - cpuCopyMode: Specifies how CPU settings are handled during container recreation.
//
// Returns:
//   - *metrics.Metric: A pointer to a metric object summarizing the update session (scanned, updated, failed counts).
func RunUpdatesWithNotifications(
	client container.Client,
	notifier types.Notifier,
	notificationSplitByContainer, notificationReport bool,
	filter types.Filter,
	cleanup, noRestart, monitorOnly, lifecycleHooks, rollingRestart, labelPrecedence, noPull bool,
	timeout time.Duration,
	lifecycleUID, lifecycleGID int,
	cpuCopyMode string,
) *metrics.Metric {
	// Start batching notifications to group update messages, if notifier is initialized
	if notifier != nil {
		notifier.StartNotification()
	} else {
		logrus.Warn("Notifier is nil, skipping notification batching")
	}

	// Configure update parameters based on provided flags and settings.
	updateParams := types.UpdateParams{
		Filter:          filter,
		Cleanup:         cleanup,
		NoRestart:       noRestart,
		Timeout:         timeout,
		MonitorOnly:     monitorOnly,
		LifecycleHooks:  lifecycleHooks,
		RollingRestart:  rollingRestart,
		LabelPrecedence: labelPrecedence,
		NoPull:          noPull,
		LifecycleUID:    lifecycleUID,
		LifecycleGID:    lifecycleGID,
		CPUCopyMode:     cpuCopyMode,
	}

	// Execute the update action, capturing results and image IDs for cleanup.
	result, cleanupImageIDs, err := Update(client, updateParams)
	if err != nil {
		logrus.WithError(err).Error("Update execution failed")

		return &metrics.Metric{
			Scanned: 0,
			Updated: 0,
			Failed:  0,
		}
	}

	// Perform deferred image cleanup if enabled.
	if cleanup {
		CleanupImages(client, cleanupImageIDs)
	}

	// Log update report for debugging.
	updatedNames := make([]string, 0, len(result.Updated()))
	for _, r := range result.Updated() {
		updatedNames = append(updatedNames, r.Name())
	}

	logrus.WithFields(logrus.Fields{
		"scanned":       len(result.Scanned()),
		"updated":       len(result.Updated()),
		"failed":        len(result.Failed()),
		"updated_names": updatedNames,
	}).Debug("Report before notification")

	// Send the batched notification with update results, if notifier and result are initialized
	if notifier != nil && result != nil {
		if notificationSplitByContainer {
			// Notification splitting by container is enabled - send separate notifications for each container
			// instead of a single grouped notification. This provides more granular notifications when
			// multiple containers are updated simultaneously.
			if notificationReport && len(result.Updated()) > 0 {
				// In report mode: Send separate notifications for each updated container.
				// Each notification contains a SingleContainerReport with the specific container
				// as the primary updated item, but includes all other containers for context
				// (failed, skipped, stale, fresh) to provide complete session information.
				for _, updatedContainer := range result.Updated() {
					// Create a minimal report focused on this specific updated container,
					// but include all other session results for comprehensive context.
					singleContainerReport := &session.SingleContainerReport{
						UpdatedReports: []types.ContainerReport{updatedContainer},
						ScannedReports: []types.ContainerReport{
							updatedContainer,
						}, // Include all scanned for context
						FailedReports:  result.Failed(),  // Include all failed for context
						SkippedReports: result.Skipped(), // Include all skipped for context
						StaleReports:   result.Stale(),   // Include all stale for context
						FreshReports:   result.Fresh(),   // Include all fresh for context
					}
					notifier.SendNotification(singleContainerReport)
				}
			} else if !notificationReport {
				// In log mode: Send separate notifications for each container that had "Found new image" logs.
				// This handles cases where containers may not have been updated (e.g., monitor-only mode)
				// but still triggered relevant log entries that should be notified separately.
				entries := notifier.GetEntries()
				containerNames := make(map[string]bool)

				// Extract unique container names from log entries that indicate new images were found.
				// This ensures we only send notifications for containers that actually had update activity.
				for _, entry := range entries {
					if strings.Contains(entry.Message, "Found new image") {
						if containerName, ok := entry.Data["container"].(string); ok {
							containerNames[containerName] = true
						}
					}
				}

				// For each container with update activity, filter and send only its relevant log entries.
				// This prevents mixing logs from different containers in the same notification.
				for containerName := range containerNames {
					filteredEntries := make([]*logrus.Entry, 0)

					for _, entry := range entries {
						if cn, ok := entry.Data["container"].(string); ok && cn == containerName {
							filteredEntries = append(filteredEntries, entry)
						}
					}

					notifier.SendFilteredEntries(filteredEntries, nil)
				}
			}
		} else {
			// Standard behavior: Send a single grouped notification containing all session results
			notifier.SendNotification(result)
		}
	}

	// Generate and log a metric summarizing the update session.
	metricResults := metrics.NewMetric(result)
	notifications.LocalLog.WithFields(logrus.Fields{
		"scanned": metricResults.Scanned,
		"updated": metricResults.Updated,
		"failed":  metricResults.Failed,
	}).Info("Update session completed")

	return metricResults
}
