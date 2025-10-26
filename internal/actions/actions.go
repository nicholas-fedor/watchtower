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

// handleUpdateResult processes the result of an update operation and returns an appropriate metric.
//
// It checks for errors or nil results, logging accordingly, and returns a zero metric on failure
// or nil on success to indicate continuation of the update process.
//
// Parameters:
//   - result: The report from the update operation.
//   - err: Any error encountered during the update.
//
// Returns:
//   - *metrics.Metric: A zero metric if an error occurred or result is nil, nil otherwise.
func handleUpdateResult(result types.Report, err error) *metrics.Metric {
	// Check for errors during update execution
	if err != nil {
		logrus.WithError(err).Error("Update execution failed")

		return &metrics.Metric{
			Scanned: 0,
			Updated: 0,
			Failed:  0,
		}
	}

	// Check if update result is nil
	if result == nil {
		logrus.Debug("Update result is nil, returning zero metric")

		return &metrics.Metric{
			Scanned: 0,
			Updated: 0,
			Failed:  0,
		}
	}

	return nil
}

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

	// Initiate notification batching
	startNotifications(notifier)

	// Configure update parameters based on provided flags
	updateParams := configureUpdateParams(
		filter,
		cleanup, noRestart, monitorOnly, lifecycleHooks, rollingRestart, labelPrecedence, noPull,
		timeout,
		lifecycleUID, lifecycleGID,
		cpuCopyMode,
	)

	// Execute the container update operation
	result, cleanupImageIDs, err := executeUpdate(client, updateParams)
	// Process update result, return metric on failure
	if metric := handleUpdateResult(result, err); metric != nil {
		return metric
	}

	// Perform image cleanup if enabled
	performImageCleanup(client, cleanup, cleanupImageIDs)

	// Log update report details for debugging
	logUpdateReport(result)

	// Send notifications about update results
	sendNotifications(notifier, notificationSplitByContainer, notificationReport, result)

	// Generate and return metric summarizing the session
	return generateAndLogMetric(result)
}

// buildSingleContainerReport creates a SingleContainerReport for a specific updated container.
//
// It populates the report with the updated container as the primary item and includes
// all other session results (scanned, failed, skipped, stale, fresh) for comprehensive context.
//
// Parameters:
//   - updatedContainer: The container that was updated.
//   - result: The full session report containing all container statuses.
//
// Returns:
//   - *session.SingleContainerReport: A report focused on the updated container with full session context.
func buildSingleContainerReport(
	updatedContainer types.ContainerReport,
	result types.Report,
) *session.SingleContainerReport {
	return &session.SingleContainerReport{
		UpdatedReports: []types.ContainerReport{updatedContainer},
		ScannedReports: result.Scanned(),
		FailedReports:  result.Failed(),
		SkippedReports: result.Skipped(),
		StaleReports:   result.Stale(),
		FreshReports:   result.Fresh(),
	}
}

// startNotifications initiates notification batching if a notifier is provided.
//
// It starts the notification process to group update messages, or logs a debug message
// if no notifier is available.
//
// Parameters:
//   - notifier: The notification system instance for sending update status messages.
func startNotifications(notifier types.Notifier) {
	if notifier != nil {
		notifier.StartNotification()
	} else {
		logrus.Debug("Notifier is nil, skipping notification batching")
	}
}

// configureUpdateParams creates and configures the update parameters based on provided flags and settings.
//
// It constructs a types.UpdateParams struct with all necessary configuration for the update operation.
//
// Parameters:
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
//   - types.UpdateParams: The configured update parameters.
func configureUpdateParams(
	filter types.Filter,
	cleanup, noRestart, monitorOnly, lifecycleHooks, rollingRestart, labelPrecedence, noPull bool,
	timeout time.Duration,
	lifecycleUID, lifecycleGID int,
	cpuCopyMode string,
) types.UpdateParams {
	return types.UpdateParams{
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
}

// executeUpdate performs the container update operation and handles errors.
//
// It calls the Update function with the provided parameters, captures the results,
// and returns them along with any error encountered.
//
// Parameters:
//   - client: The Docker client instance used for container operations.
//   - updateParams: The configured parameters for the update operation.
//
// Returns:
//   - types.Report: The report containing the results of the update operation.
//   - map[types.ImageID]bool: Set of image IDs to be cleaned up.
//   - error: Any error encountered during the update execution.
func executeUpdate(
	client container.Client,
	updateParams types.UpdateParams,
) (types.Report, map[types.ImageID]bool, error) {
	// Log before calling the Update function
	logrus.Debug("About to call Update function")

	result, cleanupImageIDs, err := Update(client, updateParams)

	// Log after Update function returns
	logrus.Debug("Update function returned, about to check cleanup")

	return result, cleanupImageIDs, err
}

// performImageCleanup executes image cleanup if enabled.
//
// It removes old images after updates if the cleanup flag is set.
//
// Parameters:
//   - client: The Docker client instance used for container operations.
//   - cleanup: Boolean indicating whether to perform image cleanup.
//   - cleanupImageIDs: Set of image IDs to be removed.
func performImageCleanup(
	client container.Client,
	cleanup bool,
	cleanupImageIDs map[types.ImageID]bool,
) {
	if cleanup {
		CleanupImages(client, cleanupImageIDs)
	}
}

// logUpdateReport logs the update report details for debugging purposes.
//
// It extracts updated container names and logs comprehensive session statistics.
//
// Parameters:
//   - result: The report containing the results of the update operation.
func logUpdateReport(result types.Report) {
	// Initialize slice for updated container names
	updatedNames := make([]string, 0, len(result.Updated()))
	// Collect names of all updated containers
	for _, r := range result.Updated() {
		updatedNames = append(updatedNames, r.Name())
	}

	logrus.WithFields(logrus.Fields{
		"scanned":       len(result.Scanned()),
		"updated":       len(result.Updated()),
		"failed":        len(result.Failed()),
		"updated_names": updatedNames,
	}).Debug("Report before notification")
}

// sendNotifications handles sending notifications about update results.
//
// It supports both grouped and per-container notifications based on configuration flags,
// including complex logic for splitting notifications by container.
//
// Parameters:
//   - notifier: The notification system instance for sending update status messages.
//   - notificationSplitByContainer: Boolean flag enabling separate notifications for each updated container.
//   - notificationReport: Boolean flag enabling report-based notifications.
//   - result: The report containing the results of the update operation.
func sendNotifications(
	notifier types.Notifier,
	notificationSplitByContainer, notificationReport bool,
	result types.Report,
) {
	logrus.Debug("About to send notifications")

	// Check if notifier is available
	if notifier != nil {
		// Check if notifications should be split by container
		if notificationSplitByContainer {
			// Check if report-based notifications are enabled and there are updated containers
			if notificationReport && len(result.Updated()) > 0 {
				// Loop through updated containers and send individual report notifications
				for _, updatedContainer := range result.Updated() {
					singleContainerReport := buildSingleContainerReport(updatedContainer, result)
					notifier.SendNotification(singleContainerReport)
				}
			} else if !notificationReport && len(result.Updated()) > 0 {
				// Loop through updated containers for individual non-report notifications
				for _, updatedContainer := range result.Updated() {
					// Skip nil container reports
					if updatedContainer == nil {
						logrus.Debug("Encountered nil updated container report, skipping")

						continue
					}

					// Skip containers with empty names
					if strings.TrimSpace(updatedContainer.Name()) == "" {
						logrus.Debug("Encountered container with empty name, skipping notification")

						continue
					}

					logrus.WithFields(logrus.Fields{
						"container": updatedContainer.Name(),
						"image":     updatedContainer.ImageName(),
					}).Debug("Sending individual notification for updated container")

					singleContainerReport := buildSingleContainerReport(updatedContainer, result)

					// Create log entries for container update events
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

					notifier.SendFilteredEntries(entries, singleContainerReport)
				}
			}

			logrus.Debug("About to return metric")
		} else {
			// Send grouped notification if not splitting by container
			notifier.SendNotification(result)
		}
	}
}

// generateAndLogMetric creates a metric from the update results and logs it.
//
// It generates a summary metric of the session and logs the completion details.
//
// Parameters:
//   - result: The report containing the results of the update operation.
//
// Returns:
//   - *metrics.Metric: A pointer to a metric object summarizing the update session.
func generateAndLogMetric(result types.Report) *metrics.Metric {
	// Create metric from update results
	metricResults := metrics.NewMetric(result)
	// Log session completion with metric details
	notifications.LocalLog.WithFields(logrus.Fields{
		"scanned": metricResults.Scanned,
		"updated": metricResults.Updated,
		"failed":  metricResults.Failed,
	}).Info("Update session completed")

	return metricResults
}
