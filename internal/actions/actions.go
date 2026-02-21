package actions

import (
	"context"
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
	// StoppingLinkedContainerMessage is the message logged when stopping a linked container for restart.
	StoppingLinkedContainerMessage = "Stopping linked container"
	// StartedLinkedContainerMessage is the message logged when a linked container is started after restart.
	StartedLinkedContainerMessage = "Started linked container"
	// UpdateSkippedMessage is the message logged when an update is skipped in monitor-only mode.
	UpdateSkippedMessage = "Update available but skipped (monitor-only mode)"
	// ContainerRemainsRunningMessage is the message logged when a container remains running in monitor-only mode.
	ContainerRemainsRunningMessage = "Container remains running (monitor-only mode)"
)

// RunUpdatesWithNotificationsParams holds the parameters for RunUpdatesWithNotifications.
type RunUpdatesWithNotificationsParams struct {
	Client                       container.Client  // Docker client for container operations
	Notifier                     types.Notifier    // Notification system for sending update status messages
	NotificationSplitByContainer bool              // Enable separate notifications for each updated container
	NotificationReport           bool              // Enable report-based notifications
	Filter                       types.Filter      // Container filter determining which containers are targeted
	Cleanup                      bool              // Remove old images after container updates
	NoRestart                    bool              // Prevent containers from being restarted after updates
	MonitorOnly                  bool              // Monitor containers without performing updates
	LifecycleHooks               bool              // Enable pre- and post-update lifecycle hook commands
	RollingRestart               bool              // Update containers sequentially rather than all at once
	LabelPrecedence              bool              // Give container label settings priority over global flags
	NoPull                       bool              // Skip pulling new images from registry during updates
	Timeout                      time.Duration     // Maximum duration for container stop operations
	LifecycleUID                 int               // Default UID to run lifecycle hooks as
	LifecycleGID                 int               // Default GID to run lifecycle hooks as
	CPUCopyMode                  string            // CPU settings handling when recreating containers
	PullFailureDelay             time.Duration     // Delay after failed Watchtower self-update pulls
	RunOnce                      bool              // Perform one-time update and exit
	SkipSelfUpdate               bool              // Skip Watchtower self-update
	CurrentContainerID           types.ContainerID // ID of the current Watchtower container for self-update logic
}

// RunUpdatesWithNotifications performs container updates and sends notifications about the results.
//
// It executes the update action with configured parameters, batches notifications, and returns a metric
// summarizing the session for monitoring purposes, ensuring users are informed of update outcomes.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - params: The RunUpdatesWithNotificationsParams struct containing all configuration parameters.
//
// Returns:
//   - *metrics.Metric: A pointer to a metric object summarizing the update session (scanned, updated, failed counts).
func RunUpdatesWithNotifications(
	ctx context.Context,
	params RunUpdatesWithNotificationsParams,
) *metrics.Metric {
	logrus.Debug("Starting RunUpdatesWithNotifications")

	// Initiate notification batching
	startNotifications(params.Notifier, params.NotificationSplitByContainer)

	// Configure update parameters based on provided flags
	updateConfig := types.UpdateParams{
		Filter:             params.Filter,             // Container filter determining which containers are targeted
		Cleanup:            params.Cleanup,            // Remove old images after container updates
		NoRestart:          params.NoRestart,          // Prevent containers from being restarted after updates
		MonitorOnly:        params.MonitorOnly,        // Monitor containers without performing updates
		LifecycleHooks:     params.LifecycleHooks,     // Enable pre- and post-update lifecycle hook commands
		RollingRestart:     params.RollingRestart,     // Update containers sequentially rather than all at once
		LabelPrecedence:    params.LabelPrecedence,    // Give container label settings priority over global flags
		NoPull:             params.NoPull,             // Skip pulling new images from registry during updates
		Timeout:            params.Timeout,            // Maximum duration for container stop operations
		PullFailureDelay:   params.PullFailureDelay,   // Delay after failed Watchtower self-update pulls
		LifecycleUID:       params.LifecycleUID,       // Default UID to run lifecycle hooks as
		LifecycleGID:       params.LifecycleGID,       // Default GID to run lifecycle hooks as
		CPUCopyMode:        params.CPUCopyMode,        // CPU settings handling when recreating containers
		RunOnce:            params.RunOnce,            // Perform one-time update and exit
		SkipSelfUpdate:     params.SkipSelfUpdate,     // Skip Watchtower self-update
		CurrentContainerID: params.CurrentContainerID, // ID of the current Watchtower container for self-update logic
	}

	// Execute the container update operation
	result, cleanupImageInfosPtr, err := executeUpdate(
		ctx,
		params.Client,
		updateConfig,
	)
	// Process update result, return metric on failure
	if metric := handleUpdateResult(result, err, params.Notifier); metric != nil {
		return metric
	}

	// Perform image cleanup if enabled
	cleanedImages := performImageCleanup(
		ctx,
		params.Client,
		params.Cleanup,
		cleanupImageInfosPtr,
	)

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

// emptyReport is a non-nil empty report used when sending notifications about errors.
// It prevents panics in notifier implementations that may dereference the report.
type emptyReport struct{}

func (emptyReport) Scanned() []types.ContainerReport   { return nil }
func (emptyReport) Updated() []types.ContainerReport   { return nil }
func (emptyReport) Failed() []types.ContainerReport    { return nil }
func (emptyReport) Skipped() []types.ContainerReport   { return nil }
func (emptyReport) Stale() []types.ContainerReport     { return nil }
func (emptyReport) Fresh() []types.ContainerReport     { return nil }
func (emptyReport) Restarted() []types.ContainerReport { return nil }
func (emptyReport) All() []types.ContainerReport       { return nil }

// handleUpdateResult processes the result of an update operation and returns an appropriate metric.
//
// It checks for errors or nil results, logging accordingly. If an error occurred, it sends a
// notification via the provided notifier (if not nil) to alert about the failure. On error or
// nil result, it returns a zero metric to indicate the failure state. On success, it returns nil
// to indicate continuation of the update process.
//
// Parameters:
//   - result: The report from the update operation.
//   - err: Any error encountered during the update.
//   - notifier: The notification system for sending error alerts; may be nil.
//
// Returns:
//   - *metrics.Metric: A zero metric if an error occurred or result is nil, nil otherwise.
func handleUpdateResult(result types.Report, err error, notifier types.Notifier) *metrics.Metric {
	// Check for errors during update execution
	if err != nil {
		logrus.WithError(err).Error("Update execution failed")

		// Send notification about the error
		if notifier != nil {
			notifier.SendNotification(emptyReport{})
		}

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

// buildSingleRestartedContainerReport creates a SingleContainerReport for a specific restarted container.
//
// It populates the report with the restarted container as the primary item and includes
// all other session results (scanned, failed, skipped, stale, fresh) for comprehensive context.
//
// Parameters:
//   - restartedContainer: The container that was restarted.
//   - result: The full session report containing all container statuses.
//
// Returns:
//   - *session.SingleContainerReport: A report focused on the restarted container with full session context.
func buildSingleRestartedContainerReport(
	restartedContainer types.ContainerReport,
	result types.Report,
) *session.SingleContainerReport {
	return &session.SingleContainerReport{
		RestartedReports: []types.ContainerReport{restartedContainer},
		ScannedReports:   result.Scanned(),
		FailedReports:    result.Failed(),
		SkippedReports:   result.Skipped(),
		StaleReports:     result.Stale(),
		FreshReports:     result.Fresh(),
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
	cleanedImages []types.RemovedImageInfo,
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
//   - containerReport: The container report containing update details.
//   - oldContainerID: The original container ID before update.
//   - newContainerID: The new container ID after update.
//   - now: The current timestamp to use for all entries.
//
// Returns:
//   - []*logrus.Entry: A slice of three log entries for the update events.
func buildUpdateEntries(
	containerReport types.ContainerReport,
	oldContainerID, newContainerID types.ContainerID,
	now time.Time,
) []*logrus.Entry {
	if containerReport.IsMonitorOnly() {
		return []*logrus.Entry{
			{
				Level:   logrus.InfoLevel,
				Message: FoundNewImageMessage,
				Data: logrus.Fields{
					"container": containerReport.Name(),
					"image":     containerReport.ImageName(),
					"new_id":    containerReport.LatestImageID().ShortID(),
				},
				Time: now,
			},
			{
				Level:   logrus.DebugLevel,
				Message: UpdateSkippedMessage,
				Data: logrus.Fields{
					"container": containerReport.Name(),
				},
				Time: now,
			},
			{
				Level:   logrus.DebugLevel,
				Message: ContainerRemainsRunningMessage,
				Data: logrus.Fields{
					"container": containerReport.Name(),
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
				"container": containerReport.Name(),
				"image":     containerReport.ImageName(),
				"new_id":    containerReport.LatestImageID().ShortID(),
			},
			Time: now,
		},
		{
			Level:   logrus.InfoLevel,
			Message: StoppingContainerMessage,
			Data: logrus.Fields{
				"container": containerReport.Name(),
				"id":        oldContainerID.ShortID(),
				"old_id":    containerReport.CurrentImageID().ShortID(),
			},
			Time: now,
		},
		{
			Level:   logrus.InfoLevel,
			Message: StartedNewContainerMessage,
			Data: logrus.Fields{
				"container": containerReport.Name(),
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
//   - ctx: Context for cancellation and timeouts.
//   - client: The Docker client instance used for container operations.
//   - config: The UpdateParams struct containing all update configuration parameters.
//
// Returns:
//   - types.Report: The report containing the results of the update operation.
//   - []types.CleanedImageInfo: Slice of cleaned image info to be cleaned up.
//   - error: Any error encountered during the update execution.
func executeUpdate(
	ctx context.Context,
	client container.Client,
	config types.UpdateParams,
) (types.Report, []types.RemovedImageInfo, error) {
	// Log before calling the Update function
	logrus.Debug("About to call Update function")

	result, cleanupImageInfos, err := Update(ctx, client, config)

	// Log after Update function returns
	logrus.Debug("Update function returned, about to check cleanup")

	return result, cleanupImageInfos, err
}

// performImageCleanup executes image cleanup if enabled.
//
// It removes old images after updates if the cleanup flag is set.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - client: The Docker client instance used for container operations.
//   - cleanup: Boolean indicating whether to perform image cleanup.
//   - cleanupImageInfos: Slice of cleaned image info to be removed.
//
// Returns:
//   - []types.CleanedImageInfo: Slice of successfully cleaned image info.
func performImageCleanup(
	ctx context.Context,
	client container.Client,
	cleanup bool,
	cleanupImageInfos []types.RemovedImageInfo,
) []types.RemovedImageInfo {
	if cleanup {
		cleaned, err := RemoveImages(ctx, client, cleanupImageInfos)
		if err != nil {
			logrus.WithError(err).Warn("Failed to clean up some images after update")
		}

		if cleaned == nil {
			cleaned = []types.RemovedImageInfo{}
		}

		return cleaned
	}

	return []types.RemovedImageInfo{}
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
	for _, report := range result.Updated() {
		updatedNames = append(updatedNames, report.Name())
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
	cleanedImages []types.RemovedImageInfo,
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
				if notifier.ShouldSendNotification(result) {
					notifier.SendNotification(result)
				}
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
	cleanedImages []types.RemovedImageInfo,
) {
	// Map to track notified container IDs to prevent duplicate notifications.
	// Key is the full container ID string for uniqueness, value is boolean indicating
	// whether a notification has been sent for this container.
	// This map is scoped to the function to ensure tracking is per-notification-session.
	notified := make(map[string]bool)

	logrus.WithFields(logrus.Fields{
		"updated_count":   len(result.Updated()),
		"restarted_count": len(result.Restarted()),
		"stale_count":     len(result.Stale()),
		"failed_count":    len(result.Failed()),
		"skipped_count":   len(result.Skipped()),
		"fresh_count":     len(result.Fresh()),
		"scanned_count":   len(result.Scanned()),
	}).Debug("Split notifications: container counts by category")

	if notificationReport {
		// Log updated containers for debugging
		updatedNames := make([]string, 0, len(result.Updated()))
		for _, report := range result.Updated() {
			updatedNames = append(updatedNames, report.Name())
		}

		logrus.WithField("updated_containers", updatedNames).
			Debug("Split notifications: sending report notifications for updated containers")

		// Send individual report notifications for each updated container
		for _, c := range result.Updated() {
			// Skip nil container reports
			if c == nil {
				logrus.Debug("Encountered nil updated container report, skipping")

				continue
			}

			// Skip containers with empty names
			if strings.TrimSpace(c.Name()) == "" {
				logrus.WithField("container_id", c.ID().ShortID()).
					Debug("Encountered container with empty name, skipping notification")

				continue
			}

			containerID := string(c.ID())
			if notified[containerID] {
				// Skip notification if already sent for this container ID
				continue
			}

			singleContainerReport := buildSingleContainerReport(c, result)
			if notifier.ShouldSendNotification(singleContainerReport) {
				notifier.SendNotification(singleContainerReport)
			}

			notified[containerID] = true
		}

		// Send individual report notifications for each restarted container
		for _, report := range result.Restarted() {
			// Skip nil container reports
			if report == nil {
				logrus.Debug("Encountered nil restarted container report, skipping")

				continue
			}

			// Skip containers with empty names
			if strings.TrimSpace(report.Name()) == "" {
				logrus.WithField("container_id", report.ID().ShortID()).
					Debug("Encountered restarted container with empty name, skipping notification")

				continue
			}

			containerID := string(report.ID())
			if notified[containerID] {
				// Skip notification if already sent for this container ID
				continue
			}

			singleContainerReport := buildSingleRestartedContainerReport(report, result)
			if notifier.ShouldSendNotification(singleContainerReport) {
				notifier.SendNotification(singleContainerReport)
			}

			notified[containerID] = true
		}

		// Send notifications for monitor-only containers when notificationReport is true
		for _, report := range result.Stale() {
			// Skip nil container reports
			if report == nil {
				logrus.Debug("Encountered nil stale container report, skipping")

				continue
			}

			// Skip containers with empty names
			if strings.TrimSpace(report.Name()) == "" {
				logrus.WithField("container_id", report.ID().ShortID()).
					Debug("Encountered stale container with empty name, skipping notification")

				continue
			}

			if report.IsMonitorOnly() {
				containerID := string(report.ID())
				if notified[containerID] {
					// Skip notification if already sent for this container ID
					continue
				}

				singleContainerReport := buildSingleContainerReport(
					report,
					result,
				)
				if notifier.ShouldSendNotification(singleContainerReport) {
					notifier.SendNotification(singleContainerReport)
				}

				notified[containerID] = true
			}
		}
	} else {
		// Log updated containers for debugging
		updatedNames := make([]string, 0, len(result.Updated()))
		for _, report := range result.Updated() {
			updatedNames = append(updatedNames, report.Name())
		}

		logrus.WithField("updated_containers", updatedNames).
			Debug("Split notifications: sending filtered entry notifications for updated containers")

		// Send individual filtered entry notifications for each updated container
		for _, report := range result.Updated() {
			// Skip nil container reports
			if report == nil {
				logrus.Debug("Encountered nil updated container report, skipping")

				continue
			}

			// Skip containers with empty names
			if strings.TrimSpace(report.Name()) == "" {
				logrus.WithField("container_id", report.ID().ShortID()).
					Debug("Encountered container with empty name, skipping notification")

				continue
			}

			containerID := string(report.ID())
			if notified[containerID] {
				// Skip notification if already sent for this container ID
				continue
			}

			logrus.WithFields(logrus.Fields{
				"container": report.Name(),
				"image":     report.ImageName(),
			}).Debug("Sending individual notification for updated container")

			singleContainerReport := buildSingleContainerReport(report, result)

			// Create log entries for container update events
			entries := buildUpdateEntries(
				report,
				report.ID(),
				report.NewContainerID(),
				time.Now(),
			)

			// Add cleanup entries for this container
			containerCleanupEntries := buildCleanupEntriesForContainer(
				cleanedImages,
				report.Name(),
			)
			entries = append(entries, containerCleanupEntries...)

			if notifier.ShouldSendNotification(singleContainerReport) {
				notifier.SendFilteredEntries(entries, singleContainerReport)
			}

			notified[containerID] = true
		}

		// Send individual filtered entry notifications for each restarted container
		for _, report := range result.Restarted() {
			// Skip nil container reports
			if report == nil {
				logrus.Debug("Encountered nil restarted container report, skipping")

				continue
			}

			// Skip containers with empty names
			if strings.TrimSpace(report.Name()) == "" {
				logrus.WithField("container_id", report.ID().ShortID()).
					Debug("Encountered restarted container with empty name, skipping notification")

				continue
			}

			containerID := string(report.ID())
			if notified[containerID] {
				// Skip notification if already sent for this container ID
				continue
			}

			logrus.WithFields(logrus.Fields{
				"container": report.Name(),
				"image":     report.ImageName(),
			}).Debug("Sending individual notification for restarted container")

			singleContainerReport := buildSingleRestartedContainerReport(report, result)

			now := time.Now()

			newID := report.NewContainerID()
			if newID == "" {
				newID = report.ID()
			}

			// Build cleanup entries first to preallocate the entries slice with correct capacity
			containerCleanupEntries := buildCleanupEntriesForContainer(
				cleanedImages,
				report.Name(),
			)

			// Create log entries for container restart events (similar to update but without "Found new image")
			// Base entries: StoppingLinkedContainerMessage + StartedLinkedContainerMessage
			const baseEntryCount = 2

			entries := make([]*logrus.Entry, 0, baseEntryCount+len(containerCleanupEntries))
			entries = append(entries,
				&logrus.Entry{
					Level:   logrus.InfoLevel,
					Message: StoppingLinkedContainerMessage,
					Data: logrus.Fields{
						"container": report.Name(),
						"id":        report.ID().ShortID(),
						"old_id":    report.CurrentImageID().ShortID(),
					},
					Time: now,
				},
				&logrus.Entry{
					Level:   logrus.InfoLevel,
					Message: StartedLinkedContainerMessage,
					Data: logrus.Fields{
						"container": report.Name(),
						"new_id":    newID.ShortID(),
					},
					Time: now,
				},
			)
			entries = append(entries, containerCleanupEntries...)

			if notifier.ShouldSendNotification(singleContainerReport) {
				notifier.SendFilteredEntries(entries, singleContainerReport)
			}

			notified[containerID] = true
		}

		// Send notifications for monitor-only containers when notificationReport is false
		for _, report := range result.Stale() {
			// Skip nil container reports
			if report == nil {
				logrus.Debug("Encountered nil stale container report, skipping")

				continue
			}

			// Skip containers with empty names
			if strings.TrimSpace(report.Name()) == "" {
				logrus.WithField("container_id", report.ID().ShortID()).
					Debug("Encountered stale container with empty name, skipping notification")

				continue
			}

			if report.IsMonitorOnly() {
				containerID := string(report.ID())
				if notified[containerID] {
					// Skip notification if already sent for this container ID
					continue
				}

				logrus.WithFields(logrus.Fields{
					"container": report.Name(),
					"image":     report.ImageName(),
				}).Debug("Sending individual notification for monitor-only stale container")

				singleContainerReport := buildSingleContainerReport(report, result)

				// Create log entries for container update events (monitor-only containers don't get updated, but we still send the same format)
				entries := buildUpdateEntries(
					report,
					report.ID(),
					report.NewContainerID(),
					time.Now(),
				)

				// Add cleanup entries for this container
				containerCleanupEntries := buildCleanupEntriesForContainer(
					cleanedImages,
					report.Name(),
				)
				entries = append(entries, containerCleanupEntries...)

				if notifier.ShouldSendNotification(singleContainerReport) {
					notifier.SendFilteredEntries(entries, singleContainerReport)
				}

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
