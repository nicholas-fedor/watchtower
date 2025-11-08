package actions

import (
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/notifications"
	"github.com/nicholas-fedor/watchtower/pkg/session"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Exported constants for update message literals to ensure consistency across the codebase.
// These constants define the standard messages used in container update logging and notifications.
const (
	// FoundNewImageMessage is the message logged when a new image is found for a container.
	FoundNewImageMessage = "Found new image"
	// StoppingContainerMessage is the message logged when stopping a container for update.
	StoppingContainerMessage = "Stopping container"
	// StartedNewContainerMessage is the message logged when a new container is started after update.
	StartedNewContainerMessage = "Started new container"
	// UpdateSkippedMessage is the message logged when an update is skipped in monitor-only mode.
	UpdateSkippedMessage = "Update available but skipped (monitor-only mode)"
	// ContainerRemainsRunningMessage is the message logged when a container remains running in monitor-only mode.
	ContainerRemainsRunningMessage = "Container remains running (monitor-only mode)"
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
	startNotifications(params.Notifier, params.NotificationSplitByContainer)

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
	result, cleanupImageInfosPtr, err := executeUpdate(params.Client, updateConfig)
	// Process update result, return metric on failure
	if metric := handleUpdateResult(result, err); metric != nil {
		return metric
	}

	// Perform image cleanup if enabled
	cleanedImages := performImageCleanup(params.Client, params.Cleanup, cleanupImageInfosPtr)

	// Log update report details for debugging
	logUpdateReport(result)

	logrus.WithFields(logrus.Fields{
		"notification_split_by_container": params.NotificationSplitByContainer,
		"notification_report":             params.NotificationReport,
		"notifier_present":                params.Notifier != nil,
	}).Debug("About to send notifications")

	// Send notifications about update results
	sendNotifications(
		params.Notifier,
		params.NotificationSplitByContainer,
		params.NotificationReport,
		result,
		cleanedImages,
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

// buildCleanupEntriesForContainer constructs log entries for cleaned image events specific to a container.
//
// It creates a logrus.Entry struct for each cleaned image associated with the specified container
// using a standardized message "Removing image" with the image name and ID in the entry data.
//
// Parameters:
//   - cleanedImages: Slice of CleanedImageInfo containing details of cleaned images.
//   - containerName: Name of the container to filter cleanup entries for.
//
// Returns:
//   - []*logrus.Entry: A slice of log entries for the cleaned images associated with the container.
func buildCleanupEntriesForContainer(
	cleanedImages []types.CleanedImageInfo,
	containerName string,
) []*logrus.Entry {
	entries := make([]*logrus.Entry, 0)
	now := time.Now()

	for _, cleanedImage := range cleanedImages {
		if cleanedImage.ContainerName == containerName {
			entry := &logrus.Entry{
				Level:   logrus.InfoLevel,
				Message: "Removing image",
				Data: logrus.Fields{
					"container_name": cleanedImage.ContainerName,
					"image_name":     cleanedImage.ImageName,
					"image_id":       cleanedImage.ImageID.ShortID(),
				},
				Time: now,
			}
			entries = append(entries, entry)
		}
	}

	return entries
}

// buildUpdateEntries constructs log entries for container update events.
//
// It creates three logrus.Entry structs representing the key stages of a container update:
// finding a new image, stopping the container, and starting the new container.
// For monitor-only containers, it reports detection without action.
//
// Parameters:
//   - c: The container report containing update details.
//   - oldContainerID: The original container ID before update.
//   - newContainerID: The new container ID after update.
//   - now: The current timestamp to use for all entries.
//
// Returns:
//   - []*logrus.Entry: A slice of three log entries for the update events.
func buildUpdateEntries(
	c types.ContainerReport,
	oldContainerID, newContainerID types.ContainerID,
	now time.Time,
) []*logrus.Entry {
	if c.IsMonitorOnly() {
		return []*logrus.Entry{
			{
				Level:   logrus.InfoLevel,
				Message: FoundNewImageMessage,
				Data: logrus.Fields{
					"container": c.Name(),
					"image":     c.ImageName(),
					"new_id":    c.LatestImageID().ShortID(),
				},
				Time: now,
			},
			{
				Level:   logrus.DebugLevel,
				Message: UpdateSkippedMessage,
				Data: logrus.Fields{
					"container": c.Name(),
				},
				Time: now,
			},
			{
				Level:   logrus.DebugLevel,
				Message: ContainerRemainsRunningMessage,
				Data: logrus.Fields{
					"container": c.Name(),
				},
				Time: now,
			},
		}
	}

	return []*logrus.Entry{
		{
			Level:   logrus.InfoLevel,
			Message: FoundNewImageMessage,
			Data: logrus.Fields{
				"container": c.Name(),
				"image":     c.ImageName(),
				"new_id":    c.LatestImageID().ShortID(),
			},
			Time: now,
		},
		{
			Level:   logrus.InfoLevel,
			Message: StoppingContainerMessage,
			Data: logrus.Fields{
				"container": c.Name(),
				"id":        oldContainerID.ShortID(),
				"old_id":    c.CurrentImageID().ShortID(),
			},
			Time: now,
		},
		{
			Level:   logrus.InfoLevel,
			Message: StartedNewContainerMessage,
			Data: logrus.Fields{
				"container": c.Name(),
				"new_id":    newContainerID.ShortID(),
			},
			Time: now,
		},
	}
}

// startNotifications initiates notification batching if a notifier is provided.
//
// It starts the notification process to group update messages, or logs a debug message
// if no notifier is available. When notifications are split by container, it suppresses
// the summary notification to prevent unwanted duplicates.
//
// Parameters:
//   - notifier: The notification system instance for sending update status messages.
//   - notificationSplitByContainer: Boolean flag indicating whether notifications are split by container.
func startNotifications(notifier types.Notifier, notificationSplitByContainer bool) {
	if notifier != nil {
		notifier.StartNotification(notificationSplitByContainer)
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
//   - []types.CleanedImageInfo: Slice of cleaned image info to be cleaned up.
//   - error: Any error encountered during the update execution.
func executeUpdate(
	client container.Client,
	config UpdateConfig,
) (types.Report, []types.CleanedImageInfo, error) {
	// Log before calling the Update function
	logrus.Debug("About to call Update function")

	result, cleanupImageInfos, err := Update(client, config)

	// Log after Update function returns
	logrus.Debug("Update function returned, about to check cleanup")

	return result, cleanupImageInfos, err
}

// performImageCleanup executes image cleanup if enabled.
//
// It removes old images after updates if the cleanup flag is set.
//
// Parameters:
//   - client: The Docker client instance used for container operations.
//   - cleanup: Boolean indicating whether to perform image cleanup.
//   - cleanupImageInfos: Slice of cleaned image info to be removed.
//
// Returns:
//   - []types.CleanedImageInfo: Slice of successfully cleaned image info.
func performImageCleanup(
	client container.Client,
	cleanup bool,
	cleanupImageInfos []types.CleanedImageInfo,
) []types.CleanedImageInfo {
	if cleanup {
		cleaned, err := CleanupImages(client, cleanupImageInfos)
		if err != nil {
			logrus.WithError(err).Warn("Failed to clean up some images after update")
		}

		if cleaned == nil {
			cleaned = []types.CleanedImageInfo{}
		}

		return cleaned
	}

	return []types.CleanedImageInfo{}
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
// including complex logic for splitting notifications by container. The non-split path
// sends notifications asynchronously using a goroutine with proper synchronization
// to ensure the notification completes before the notifier is closed.
//
// Parameters:
//   - notifier: The notification system instance for sending update status messages.
//   - notificationSplitByContainer: Boolean flag enabling separate notifications for each updated container.
//   - notificationReport: Boolean flag enabling report-based notifications.
//   - result: The report containing the results of the update operation.
//   - cleanedImages: Slice of successfully cleaned image info.
func sendNotifications(
	notifier types.Notifier,
	notificationSplitByContainer, notificationReport bool,
	result types.Report,
	cleanedImages []types.CleanedImageInfo,
) {
	logrus.Debug("About to send notifications")

	// Check if notifier is available
	if notifier != nil {
		// Check if notifications should be split by container
		if notificationSplitByContainer {
			sendSplitNotifications(notifier, notificationReport, result, cleanedImages)
		} else {
			// Send grouped notification asynchronously with proper synchronization
			var waitGroup sync.WaitGroup

			waitGroup.Go(func() {
				notifier.SendNotification(result)
			})

			waitGroup.Wait()
		}
	}
}

// sendSplitNotifications handles sending notifications when split by container is enabled.
//
// It processes updated containers and sends either report-based or filtered entry notifications
// based on the notificationReport flag, skipping invalid containers.
// When notificationReport is true, it also sends notifications for monitor-only
// containers from the stale list.
// To prevent duplicate notifications for the same container, a map is used to track
// which container IDs have already been notified during this notification session.
// This tracking mechanism ensures that even if a container appears in multiple lists
// (e.g., due to edge cases in report generation), it receives only one notification,
// maintaining clean and non-redundant communication with users.
//
// Parameters:
//   - notifier: The notification system instance for sending update status messages.
//   - notificationReport: Boolean flag enabling report-based notifications.
//   - result: The report containing the results of the update operation.
//   - cleanedImages: Slice of successfully cleaned image info.
func sendSplitNotifications(
	notifier types.Notifier,
	notificationReport bool,
	result types.Report,
	cleanedImages []types.CleanedImageInfo,
) {
	// Map to track notified container IDs to prevent duplicate notifications.
	// Key is the full container ID string for uniqueness, value is boolean indicating
	// whether a notification has been sent for this container.
	// This map is scoped to the function to ensure tracking is per-notification-session.
	notified := make(map[string]bool)

	if notificationReport {
		// Send individual report notifications for each updated container
		for _, updatedContainer := range result.Updated() {
			// Skip nil container reports
			if updatedContainer == nil {
				logrus.Debug("Encountered nil updated container report, skipping")

				continue
			}

			// Skip containers with empty names
			if strings.TrimSpace(updatedContainer.Name()) == "" {
				logrus.WithField("container_id", updatedContainer.ID().ShortID()).
					Debug("Encountered container with empty name, skipping notification")

				continue
			}

			containerID := string(updatedContainer.ID())
			if notified[containerID] {
				// Skip notification if already sent for this container ID
				continue
			}

			singleContainerReport := buildSingleContainerReport(updatedContainer, result)
			notifier.SendNotification(singleContainerReport)

			notified[containerID] = true
		}

		// Send notifications for monitor-only containers when notificationReport is true
		for _, staleContainer := range result.Stale() {
			// Skip nil container reports
			if staleContainer == nil {
				logrus.Debug("Encountered nil stale container report, skipping")

				continue
			}

			// Skip containers with empty names
			if strings.TrimSpace(staleContainer.Name()) == "" {
				logrus.WithField("container_id", staleContainer.ID().ShortID()).
					Debug("Encountered stale container with empty name, skipping notification")

				continue
			}

			if staleContainer.IsMonitorOnly() {
				containerID := string(staleContainer.ID())
				if notified[containerID] {
					// Skip notification if already sent for this container ID
					continue
				}

				singleContainerReport := buildSingleContainerReport(staleContainer, result)
				notifier.SendNotification(singleContainerReport)

				notified[containerID] = true
			}
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

			containerID := string(updatedContainer.ID())
			if notified[containerID] {
				// Skip notification if already sent for this container ID
				continue
			}

			logrus.WithFields(logrus.Fields{
				"container": updatedContainer.Name(),
				"image":     updatedContainer.ImageName(),
			}).Debug("Sending individual notification for updated container")

			singleContainerReport := buildSingleContainerReport(updatedContainer, result)

			// Create log entries for container update events
			entries := buildUpdateEntries(updatedContainer, updatedContainer.ID(), updatedContainer.NewContainerID(), time.Now())

			// Add cleanup entries for this container
			containerCleanupEntries := buildCleanupEntriesForContainer(cleanedImages, updatedContainer.Name())
			entries = append(entries, containerCleanupEntries...)

			notifier.SendFilteredEntries(entries, singleContainerReport)

			notified[containerID] = true
		}

		// Send notifications for monitor-only containers when notificationReport is false
		for _, staleContainer := range result.Stale() {
			// Skip nil container reports
			if staleContainer == nil {
				logrus.Debug("Encountered nil stale container report, skipping")

				continue
			}

			// Skip containers with empty names
			if strings.TrimSpace(staleContainer.Name()) == "" {
				logrus.WithField("container_id", staleContainer.ID().ShortID()).Debug("Encountered stale container with empty name, skipping notification")

				continue
			}

			if staleContainer.IsMonitorOnly() {
				containerID := string(staleContainer.ID())
				if notified[containerID] {
					// Skip notification if already sent for this container ID
					continue
				}

				logrus.WithFields(logrus.Fields{
					"container": staleContainer.Name(),
					"image":     staleContainer.ImageName(),
				}).Debug("Sending individual notification for monitor-only stale container")

				singleContainerReport := buildSingleContainerReport(staleContainer, result)

				// Create log entries for container update events (monitor-only containers don't get updated, but we still send the same format)
				entries := buildUpdateEntries(staleContainer, staleContainer.ID(), staleContainer.NewContainerID(), time.Now())

				// Add cleanup entries for this container
				containerCleanupEntries := buildCleanupEntriesForContainer(cleanedImages, staleContainer.Name())
				entries = append(entries, containerCleanupEntries...)

				notifier.SendFilteredEntries(entries, singleContainerReport)

				notified[containerID] = true
			}
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
