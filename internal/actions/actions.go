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

// RunUpdatesWithNotificationsParams holds the parameters for RunUpdatesWithNotifications.
type RunUpdatesWithNotificationsParams struct {
	Client                       container.Client
	Notifier                     types.Notifier
	NotificationSplitByContainer bool
	NotificationReport           bool
	Filter                       types.Filter
	Cleanup                      bool
	NoRestart                    bool
	MonitorOnly                  bool
	LifecycleHooks               bool
	RollingRestart               bool
	LabelPrecedence              bool
	NoPull                       bool
	Timeout                      time.Duration
	LifecycleUID                 int
	LifecycleGID                 int
	CPUCopyMode                  string
	PullFailureDelay             time.Duration
}

// UpdateConfig holds the configuration parameters for container updates.
type UpdateConfig struct {
	Filter           types.Filter
	Cleanup          bool
	NoRestart        bool
	MonitorOnly      bool
	LifecycleHooks   bool
	RollingRestart   bool
	LabelPrecedence  bool
	NoPull           bool
	Timeout          time.Duration
	LifecycleUID     int
	LifecycleGID     int
	CPUCopyMode      string
	PullFailureDelay time.Duration
}

// RunUpdatesWithNotifications performs container updates and sends notifications about the results.
//
// It executes the update action with configured parameters, batches notifications, and returns a metric
// summarizing the session for monitoring purposes, ensuring users are informed of update outcomes.
//
// Parameters:
//   - params: The RunUpdatesWithNotificationsParams struct containing all configuration parameters.
//
// Returns:
//   - *metrics.Metric: A pointer to a metric object summarizing the update session (scanned, updated, failed counts).
func RunUpdatesWithNotifications(params RunUpdatesWithNotificationsParams) *metrics.Metric {
	logrus.Debug("Starting RunUpdatesWithNotifications")

	// Initiate notification batching
	startNotifications(params.Notifier)

	defer func() {
		if params.Notifier != nil {
			params.Notifier.Close()
		}
	}()

	// Configure update parameters based on provided flags
	updateConfig := UpdateConfig{
		Filter:           params.Filter,
		Cleanup:          params.Cleanup,
		NoRestart:        params.NoRestart,
		MonitorOnly:      params.MonitorOnly,
		LifecycleHooks:   params.LifecycleHooks,
		RollingRestart:   params.RollingRestart,
		LabelPrecedence:  params.LabelPrecedence,
		NoPull:           params.NoPull,
		Timeout:          params.Timeout,
		PullFailureDelay: params.PullFailureDelay,
		LifecycleUID:     params.LifecycleUID,
		LifecycleGID:     params.LifecycleGID,
		CPUCopyMode:      params.CPUCopyMode,
	}

	// Execute the container update operation
	result, cleanupImageIDs, err := executeUpdate(params.Client, updateConfig)
	// Process update result, return metric on failure
	if metric := handleUpdateResult(result, err); metric != nil {
		return metric
	}

	// Perform image cleanup if enabled
	performImageCleanup(params.Client, params.Cleanup, cleanupImageIDs)

	// Log update report details for debugging
	logUpdateReport(result)

	// Send notifications about update results
	sendNotifications(
		params.Notifier,
		params.NotificationSplitByContainer,
		params.NotificationReport,
		result,
	)

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

// buildUpdateEntries constructs log entries for container update events.
//
// It creates three logrus.Entry structs representing the key stages of a container update:
// finding a new image, stopping the container, and starting the new container.
//
// Parameters:
//   - c: The container report containing update details.
//   - now: The current timestamp to use for all entries.
//
// Returns:
//   - []*logrus.Entry: A slice of three log entries for the update events.
func buildUpdateEntries(c types.ContainerReport, now time.Time) []*logrus.Entry {
	return []*logrus.Entry{
		{
			Level:   logrus.InfoLevel,
			Message: "Found new image",
			Data: logrus.Fields{
				"container": c.Name(),
				"image":     c.ImageName(),
				"new_id":    c.LatestImageID().ShortID(),
			},
			Time: now,
		},
		{
			Level:   logrus.InfoLevel,
			Message: "Stopping container",
			Data: logrus.Fields{
				"container": c.Name(),
				"id":        c.ID().ShortID(),
				"old_id":    c.CurrentImageID().ShortID(),
			},
			Time: now,
		},
		{
			Level:   logrus.InfoLevel,
			Message: "Started new container",
			Data: logrus.Fields{
				"container": c.Name(),
				"new_id":    c.LatestImageID().ShortID(),
			},
			Time: now,
		},
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

// executeUpdate performs the container update operation and handles errors.
//
// It calls the Update function with the provided parameters, captures the results,
// and returns them along with any error encountered.
//
// Parameters:
//   - client: The Docker client instance used for container operations.
//   - config: The UpdateConfig struct containing all update configuration parameters.
//
// Returns:
//   - types.Report: The report containing the results of the update operation.
//   - map[types.ImageID]bool: Set of image IDs to be cleaned up.
//   - error: Any error encountered during the update execution.
func executeUpdate(
	client container.Client,
	config UpdateConfig,
) (types.Report, map[types.ImageID]bool, error) {
	// Log before calling the Update function
	logrus.Debug("About to call Update function")

	result, cleanupImageIDs, err := Update(client, config)

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
			sendSplitNotifications(notifier, notificationReport, result)
		} else {
			// Send grouped notification if not splitting by container
			notifier.SendNotification(result)
		}
	}
}

// sendSplitNotifications handles sending notifications when split by container is enabled.
//
// It processes updated containers and sends either report-based or filtered entry notifications
// based on the notificationReport flag, skipping invalid containers.
//
// Parameters:
//   - notifier: The notification system instance for sending update status messages.
//   - notificationReport: Boolean flag enabling report-based notifications.
//   - result: The report containing the results of the update operation.
func sendSplitNotifications(notifier types.Notifier, notificationReport bool, result types.Report) {
	if notificationReport {
		// Send individual report notifications for each updated container
		for _, updatedContainer := range result.Updated() {
			singleContainerReport := buildSingleContainerReport(updatedContainer, result)
			notifier.SendNotification(singleContainerReport)
		}
	} else {
		// Send individual filtered entry notifications for each updated container
		for _, updatedContainer := range result.Updated() {
			// Skip nil container reports
			if updatedContainer == nil {
				logrus.Debug("Encountered nil updated container report, skipping")

				continue
			}

			// Skip containers with empty names
			if strings.TrimSpace(updatedContainer.Name()) == "" {
				logrus.WithField("container_id", updatedContainer.ID().ShortID()).Debug("Encountered container with empty name, skipping notification")

				continue
			}

			logrus.WithFields(logrus.Fields{
				"container": updatedContainer.Name(),
				"image":     updatedContainer.ImageName(),
			}).Debug("Sending individual notification for updated container")

			singleContainerReport := buildSingleContainerReport(updatedContainer, result)

			// Create log entries for container update events
			entries := buildUpdateEntries(updatedContainer, time.Now())

			notifier.SendFilteredEntries(entries, singleContainerReport)
		}
	}

	logrus.Debug("Finished sending notifications")
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
