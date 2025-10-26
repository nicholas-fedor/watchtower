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
	logrus.Debug("Starting RunUpdatesWithNotifications")
	// Start batching notifications to group update messages, if notifier is initialized
	if notifier != nil {
		notifier.StartNotification()
	} else {
		logrus.Debug("Notifier is nil, skipping notification batching")
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

	logrus.Debug("About to call Update function")
	// Execute the update action, capturing results and image IDs for cleanup.
	result, cleanupImageIDs, err := Update(client, updateParams)

	logrus.Debug("Update function returned, about to check cleanup")

	if err != nil {
		logrus.WithError(err).Error("Update execution failed")

		return &metrics.Metric{
			Scanned: 0,
			Updated: 0,
			Failed:  0,
		}
	}

	if result == nil {
		logrus.Debug("Update result is nil, returning zero metric")

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

	logrus.Debug("About to send notifications")

	// Send the batched notification with update results, if notifier is initialized
	// (result is guaranteed non-nil at this point due to earlier nil check)
	if notifier != nil {
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
						ScannedReports: result.Scanned(), // Include all scanned for context
						FailedReports:  result.Failed(),  // Include all failed for context
						SkippedReports: result.Skipped(), // Include all skipped for context
						StaleReports:   result.Stale(),   // Include all stale for context
						FreshReports:   result.Fresh(),   // Include all fresh for context
					}
					notifier.SendNotification(singleContainerReport)
				}
			} else if !notificationReport && len(result.Updated()) > 0 {
				// In log mode: Send separate notifications for each container that was actually updated.
				// Create synthetic log entries to maintain proper container splitting while preventing duplicates.
				// This replaces the previous SendNotification approach with SendFilteredEntries to ensure
				// log-based notifications without duplication from stale containers.
				for _, updatedContainer := range result.Updated() {
					if updatedContainer == nil {
						logrus.Debug("Encountered nil updated container report, skipping")

						continue
					}

					if strings.TrimSpace(updatedContainer.Name()) == "" {
						logrus.Debug("Encountered container with empty name, skipping notification")

						continue
					}

					logrus.WithFields(logrus.Fields{
						"container": updatedContainer.Name(),
						"image":     updatedContainer.ImageName(),
					}).Debug("Sending individual notification for updated container")

					// Create a minimal report focused on this specific updated container,
					// but include all other session results for comprehensive context.
					singleContainerReport := &session.SingleContainerReport{
						UpdatedReports: []types.ContainerReport{updatedContainer},
						ScannedReports: result.Scanned(), // Include all scanned for context
						FailedReports:  result.Failed(),  // Include all failed for context
						SkippedReports: result.Skipped(), // Include all skipped for context
						StaleReports:   result.Stale(),   // Include all stale for context
						FreshReports:   result.Fresh(),   // Include all fresh for context
					}

					// Create synthetic log entries for the updated container with granular details
					entries := []*logrus.Entry{
						{
							Level:   logrus.InfoLevel,
							Message: "Found new image",
							Data: logrus.Fields{
								"container": updatedContainer.Name(),
								"image":     updatedContainer.ImageName(),
								"new_id":    updatedContainer.LatestImageID().ShortID(),
							},
							Time: time.Now(),
						},
						{
							Level:   logrus.InfoLevel,
							Message: "Stopping container",
							Data: logrus.Fields{
								"container": updatedContainer.Name(),
								"id":        updatedContainer.ID().ShortID(),
								"old_id":    updatedContainer.CurrentImageID().ShortID(),
							},
							Time: time.Now(),
						},
						{
							Level:   logrus.InfoLevel,
							Message: "Started new container",
							Data: logrus.Fields{
								"container": updatedContainer.Name(),
								"id":        updatedContainer.ID().ShortID(),
								"new_id":    updatedContainer.LatestImageID().ShortID(),
							},
							Time: time.Now(),
						},
					}

					// Send the synthetic log entries via SendFilteredEntries to maintain log mode behavior
					notifier.SendFilteredEntries(entries, singleContainerReport)
				}
			}

			logrus.Debug("About to return metric")
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
