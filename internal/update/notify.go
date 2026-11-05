package update

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/rs/zerolog"

	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/internal/observability/events"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	"github.com/nicholas-fedor/watchtower/internal/update/cleanup"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Notifier sends the notifications of an update session.
type Notifier interface {
	// StartNotification starts collecting log entries for a notification.
	// When suppressSummary is true, entries collected before it are dropped
	// instead of sent.
	StartNotification(suppressSummary bool)
	// SendNotification sends the collected entries with report. A split
	// report's notification carries only its container's entries.
	SendNotification(report types.Report)
	// Flush sends the collected entries without a report. Later entries are
	// sent as they are logged until the next StartNotification.
	Flush()
	// ShouldSendNotification reports whether report is worth a notification at
	// the configured notifications level.
	ShouldSendNotification(report types.Report) bool
}

// RunUpdatesWithNotificationsParams holds runtime dependencies and update policy.
//
// Update carries a complete [updateParams.Params] snapshot built from the
// process configuration.
type RunUpdatesWithNotificationsParams struct {
	// Client is the Docker client for container operations.
	Client containerClient.Client
	// Notifier sends update status messages to configured channels.
	Notifier Notifier
	// Logger is the process logger for this update session. Required and must be non-nil.
	Logger *zerolog.Logger
	// GitClient inspects remotes and clones repositories when the Git watcher is used.
	GitClient *git.Client
	// RateLimiter paces the session's registry requests. It must be the limiter
	// Client uses, so release tag lookups share the client's registry budget.
	RateLimiter *ratelimit.Limiter
	// EventBroadcaster publishes SSE events during the update session.
	EventBroadcaster *events.Broadcaster
	// OnOldSelfDetected is called when Watchtower finds that it runs in an old
	// instance's container, after that container's restart policy is disabled.
	// The caller should stop the process. Nil means the session simply ends.
	OnOldSelfDetected func()
	// Update is the complete update policy for this invocation (filter, cleanup, timeouts, etc.).
	Update updateParams.Params
	// NotificationSplitByContainer enables a separate notification for each container.
	NotificationSplitByContainer bool
	// NotificationReport enables report-based notification templates.
	NotificationReport bool
}

// reportedError marks an update error whose cause a phase has already logged
// as a notification.
//
// The session then logs "Update execution failed" without notifying, so one
// failure reaches notifications once. The marker does not change the error's
// text, and errors.Is and errors.As see through it.
type reportedError struct {
	// err is the error whose cause was already notified.
	err error
}

// Error returns the text of the wrapped error.
//
// Returns:
//   - string: The wrapped error's text.
func (e reportedError) Error() string {
	return e.err.Error()
}

// Unwrap returns the wrapped error.
//
// Returns:
//   - error: The error whose cause was already notified.
func (e reportedError) Unwrap() error {
	return e.err
}

// RunUpdatesWithNotifications performs container updates and sends notifications about the results.
//
// It executes the update action with configured parameters, batches notifications, and returns a metric
// summarizing the session for monitoring purposes, ensuring users are informed of update outcomes.
//
// A panic during the session ends it as an update error does. The panic is
// logged and sent with the session's notification batch. Subscribers get
// scan_failed, and the returned metric marks a failed scan.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - params: The RunUpdatesWithNotificationsParams struct containing all configuration parameters.
//     params.Logger is required and must be non-nil (nil panics on first log call).
//
// Returns:
//   - *metrics.Metric: A pointer to a metric object summarizing the update session (scanned, updated, failed counts).
//
//nolint:nonamedreturns // The deferred recover sets the result.
func RunUpdatesWithNotifications(
	ctx context.Context,
	params RunUpdatesWithNotificationsParams,
) (metric *metrics.Metric) {
	log := params.Logger

	log.Debug().Msg("Starting RunUpdatesWithNotifications")

	// Initiate notification batching.
	startNotifications(log, params.Notifier, params.NotificationSplitByContainer)

	defer func() {
		if recovered := recover(); recovered != nil {
			metric = endPanickedSession(log, params, recovered)
		}
	}()

	updateConfig := params.Update

	// Publish redacted policy flags only to avoid updateParams.Params leaking internal IDs.
	if params.EventBroadcaster != nil {
		params.EventBroadcaster.Publish(events.Event{
			Type:      "scan_started",
			Timestamp: time.Now().UTC(),
			Data:      updateConfig.ScanStartedData(),
		})
	}

	// Execute the container update operation
	result, cleanupImageInfosPtr, err := executeUpdate(log,
		ctx,
		params.Client,
		updateConfig,
		params.RateLimiter,
		params.GitClient,
	)
	// An old instance stops instead of reporting a failed update, since nothing failed.
	if errors.Is(err, errOldSelfDetected) {
		// Process log only. Nothing failed, so this is not a notification.
		log.Info().
			Str("notify", "no").
			Msg("Watchtower is running in an old instance's container, stopping this instance")

		// End the scan for event subscribers, with nothing scanned.
		if params.EventBroadcaster != nil {
			params.EventBroadcaster.Publish(events.Event{
				Type:      "scan_completed",
				Timestamp: time.Now().UTC(),
				Data:      events.ScanCompletedData{},
			})
		}

		if params.OnOldSelfDetected != nil {
			params.OnOldSelfDetected()
		}

		return &metrics.Metric{}
	}

	// Perform image cleanup if enabled. This runs before the result is checked,
	// so the old images of containers replaced before an update error are
	// still removed.
	cleanedImages := performImageCleanup(log,
		ctx,
		params.Client,
		updateConfig.Cleanup,
		cleanupImageInfosPtr,
		updateConfig.DetachedTimeout(),
	)

	// Publish image cleanup event
	if params.EventBroadcaster != nil && len(cleanedImages) > 0 {
		entries := make([]events.ImageCleanupEntry, len(cleanedImages))
		for i, img := range cleanedImages {
			entries[i] = events.ImageCleanupEntry{
				ImageID:       string(img.ImageID),
				ImageName:     img.ImageName,
				ContainerID:   string(img.ContainerID),
				ContainerName: img.ContainerName,
			}
		}

		params.EventBroadcaster.Publish(events.Event{
			Type:      "image_cleanup",
			Timestamp: time.Now().UTC(),
			Data: events.ImageCleanupData{
				Images: entries,
			},
		})
	}

	// Process update result, return metric on failure
	metric = handleUpdateResult(log, result, err, params.Notifier)
	if metric != nil {
		if params.EventBroadcaster != nil {
			errMsg := "unknown error"
			if err != nil {
				errMsg = err.Error()
			}

			params.EventBroadcaster.Publish(events.Event{
				Type:      "scan_failed",
				Timestamp: time.Now().UTC(),
				Data: events.ScanFailedData{
					Error: errMsg,
				},
			})
		}

		return metric
	}

	// Log update report details for debugging
	logUpdateReport(log, result)

	log.Debug().
		Bool("notification_split_by_container", params.NotificationSplitByContainer).
		Bool("notification_report", params.NotificationReport).
		Bool("notifier_present", params.Notifier != nil).
		Msg("About to send notifications")

	// Send notifications about update results
	sendNotifications(log,
		params.Notifier,
		params.NotificationSplitByContainer,
		params.NotificationReport,
		result,
		cleanedImages,
	)

	metric = generateAndLogMetric(log, result)

	if params.EventBroadcaster != nil {
		params.EventBroadcaster.Publish(events.Event{
			Type:      "scan_completed",
			Timestamp: time.Now().UTC(),
			Data: events.ScanCompletedData{
				Scanned: metric.Scanned,
				Updated: metric.Updated,
				Failed:  metric.Failed,
				Skipped: metric.Skipped,
			},
		})
	}

	return metric
}

// handleUpdateResult processes the result of an update operation and returns an appropriate metric.
//
// It checks for errors or nil results, logging accordingly. If an error occurred, it sends a
// notification via the provided notifier (if not nil) to alert about the failure. An update that
// fails partway still returns the report of the work done before the error, so that report is
// notified and counted. Without a partial report, the notification is sent without a report, so
// report templates list the queued error entry. When a phase has already logged the error's cause
// as a notification, the "Update execution failed" line is logged with notify=no, so the failure
// is notified once. On error or nil result, it returns a non-nil metric to indicate the failure
// state. On success, it returns nil to indicate continuation of the update process.
//
// Parameters:
//   - result: The report from the update operation. It may be partial when err is non-nil.
//   - err: Any error encountered during the update.
//   - notifier: The notification system for sending error alerts. It may be nil.
//
// Returns:
//   - *metrics.Metric: A metric marked as a failed scan, with the counts of the partial report
//     on error, or zero counts when there is no report. Nil on success.
func handleUpdateResult(log *zerolog.Logger, result types.Report, err error, notifier Notifier) *metrics.Metric {
	// Check for errors during update execution
	if err != nil {
		event := log.Error().Err(err)
		if _, reported := errors.AsType[reportedError](err); reported {
			event = event.Str("notify", "no")
		}

		event.Msg(logging.UpdateFailed)

		if result == nil {
			// Without a report, report templates list the batch's entries.
			if notifier != nil {
				notifier.Flush()
			}

			return &metrics.Metric{ScanFailed: true}
		}

		if notifier != nil {
			notifier.SendNotification(result)
		}

		metric := newScanMetric(result)
		metric.ScanFailed = true

		return metric
	}

	// Check if update result is nil
	if result == nil {
		log.Debug().Msg("Update result is nil, returning a failed zero metric")

		return &metrics.Metric{ScanFailed: true}
	}

	return nil
}

// endPanickedSession ends an update session that panicked, as an update error
// ends one.
//
// The panic value is logged at error, so it reaches notifications. The stack
// is logged at debug without notifying. The session's notification batch, which
// holds the panic entry, is then sent without a report. Subscribers get
// scan_failed.
//
// Parameters:
//   - log: Process logger for the session.
//   - params: The session's parameters, for its notifier and event broadcaster.
//   - recovered: The value the session panicked with.
//
// Returns:
//   - *metrics.Metric: A metric marking a failed scan with zero counts.
func endPanickedSession(
	log *zerolog.Logger,
	params RunUpdatesWithNotificationsParams,
	recovered any,
) *metrics.Metric {
	panicValue := fmt.Sprint(recovered)

	log.Error().Str("error", panicValue).Msg(logging.UpdatePanicked)
	log.Debug().
		Str("notify", "no").
		Str("stack", string(debug.Stack())).
		Msg("Stack of the update panic")

	// Without a report, report templates list the batch's entries, so the
	// panic entry is in the message. A zero-count report would hide it.
	if params.Notifier != nil {
		params.Notifier.Flush()
	}

	if params.EventBroadcaster != nil {
		params.EventBroadcaster.Publish(events.Event{
			Type:      "scan_failed",
			Timestamp: time.Now().UTC(),
			Data: events.ScanFailedData{
				Error: "update panicked: " + panicValue,
			},
		})
	}

	return &metrics.Metric{ScanFailed: true}
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
func startNotifications(log *zerolog.Logger, notifier Notifier, notificationSplitByContainer bool) {
	if notifier != nil {
		notifier.StartNotification(notificationSplitByContainer)
	} else {
		log.Debug().Msg("Notifier is nil, skipping notification batching")
	}
}

// executeUpdate performs the container update operation and handles errors.
//
// It calls Update with the provided parameters, including the optional Git
// monitor client used for associated containers.
//
// Parameters:
//   - log: Process logger.
//   - ctx: Context for cancellation and timeouts.
//   - client: Docker client used for container operations.
//   - config: Update parameters.
//   - rateLimiter: Rate limiter shared by registry requests.
//   - gitClient: Git monitor client. Nil when Git monitoring is unused.
//
// Returns:
//   - types.Report: Results of the update operation.
//   - []cleanup.RemovedImageInfo: Images eligible for cleanup.
//   - error: Non-nil when Update fails.
func executeUpdate(
	log *zerolog.Logger,
	ctx context.Context,
	client containerClient.Client,
	config updateParams.Params,
	rateLimiter *ratelimit.Limiter,
	gitClient *git.Client,
) (types.Report, []cleanup.RemovedImageInfo, error) {
	// Log before calling the Update function
	log.Debug().Msg("About to call Update function")

	result, cleanupImageInfos, err := update(log, ctx, client, config, rateLimiter, gitClient)

	// Log after Update function returns
	log.Debug().Msg("Update function returned, about to check cleanup")

	return result, cleanupImageInfos, err
}

// performImageCleanup executes image cleanup if enabled.
//
// It removes old images after updates if the cleanup flag is set.
// When multiple containers share the same old image, the image is only removed once
// (preventing duplicate "Removing image" log entries), but the returned slice includes
// all container associations so that split-by-container notifications report correctly.
// Docker calls use a detached timeout so SIGTERM after a Watchtower self-update
// cannot abort leftover image removal.
//
// Parameters:
//   - ctx: Parent session context. Cancellation is detached for Docker calls.
//   - client: The Docker client instance used for container operations.
//   - enabled: Whether image cleanup is enabled.
//   - cleanupImageInfos: Slice of cleaned image info to be removed.
//   - timeout: Bound for the detached cleanup Docker calls.
//
// Returns:
//   - []cleanup.RemovedImageInfo: Slice of successfully cleaned image info.
func performImageCleanup(log *zerolog.Logger, ctx context.Context,
	client containerClient.Client,
	enabled bool,
	cleanupImageInfos []cleanup.RemovedImageInfo,
	timeout time.Duration,
) []cleanup.RemovedImageInfo {
	if !enabled || len(cleanupImageInfos) == 0 {
		return []cleanup.RemovedImageInfo{}
	}

	// Detach from the session context so SIGTERM after self-update cannot
	// abort leftover image removal.
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		timeout,
	)
	defer cancel()

	cleaned, err := cleanup.RemoveImages(log, cleanupCtx, client, cleanupImageInfos)
	if err != nil {
		log.Warn().
			Err(err).
			Msg("Failed to clean up some images after update")
	}

	if len(cleaned) == 0 {
		return []cleanup.RemovedImageInfo{}
	}

	// Build a set of successfully removed image IDs.
	removedIDs := make(map[types.ImageID]bool, len(cleaned))
	for _, c := range cleaned {
		removedIDs[c.ImageID] = true
	}

	// Expand to include all container associations for each removed image.
	// This ensures split-by-container notifications report cleanup for every
	// container that used the old image, not just the one that triggered removal.
	result := make([]cleanup.RemovedImageInfo, 0, len(cleanupImageInfos))
	for _, info := range cleanupImageInfos {
		if removedIDs[info.ImageID] {
			result = append(result, info)
		}
	}

	return result
}

// logUpdateReport logs the update report details for debugging purposes.
//
// It extracts updated container names and logs comprehensive session statistics.
//
// Parameters:
//   - result: The report containing the results of the update operation.
func logUpdateReport(log *zerolog.Logger, result types.Report) {
	// Initialize slice for updated container names
	updatedNames := make([]string, 0, len(result.Updated()))
	// Collect names of all updated containers
	for _, report := range result.Updated() {
		updatedNames = append(updatedNames, report.Name())
	}

	log.Debug().
		Int("scanned", len(result.Scanned())).
		Int("updated", len(result.Updated())).
		Int("failed", len(result.Failed())).
		Strs("updated_names", updatedNames).
		Msg("Report before notification")
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
//   - notificationSplitByContainer: Boolean flag enabling a separate notification for each container.
//   - notificationReport: Boolean flag enabling report-based notifications.
//   - result: The report containing the results of the update operation.
//   - cleanedImages: Slice of successfully cleaned image info.
func sendNotifications(log *zerolog.Logger, notifier Notifier,
	notificationSplitByContainer, notificationReport bool,
	result types.Report,
	cleanedImages []cleanup.RemovedImageInfo,
) {
	// Check if notifier is available
	if notifier != nil {
		// Check if notifications should be split by container
		if notificationSplitByContainer {
			sendSplitNotifications(log, notifier, notificationReport, result, cleanedImages)
		} else if notifier.ShouldSendNotification(result) {
			notifier.SendNotification(result)
		}
	}
}

// sendSplitNotifications handles sending notifications when split by container is enabled.
//
// It sends one notification for each of these containers:
//   - an updated or restarted container
//   - a container whose update failed
//   - a monitor-only container with a newer image
//   - a container skipped during its update cooldown
//
// The notificationReport flag picks report-based or filtered entry
// notifications. Invalid containers are skipped.
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
func sendSplitNotifications(log *zerolog.Logger, notifier Notifier,
	notificationReport bool,
	result types.Report,
	cleanedImages []cleanup.RemovedImageInfo,
) {
	// Map to track notified container IDs to prevent duplicate notifications.
	// Key is the full container ID string for uniqueness, value is boolean indicating
	// whether a notification has been sent for this container.
	// This map is scoped to the function to ensure tracking is per-notification-session.
	notified := make(map[string]bool)

	log.Debug().
		Int("updated_count", len(result.Updated())).
		Int("restarted_count", len(result.Restarted())).
		Int("stale_count", len(result.Stale())).
		Int("failed_count", len(result.Failed())).
		Int("skipped_count", len(result.Skipped())).
		Int("fresh_count", len(result.Fresh())).
		Int("scanned_count", len(result.Scanned())).
		Msg("Split notifications: container counts by category")

	if !notificationReport {
		// Log-mode split: hook-captured entries are filtered by container inside
		// SendNotification when the report is a SingleContainerReport.
		//
		// Image removal logs once per ImageID without container scope. Expanded
		// cleanedImages retains every container association for shared images, so
		// emit a hooked container-scoped cleanup event for each entry so both
		// containers that shared an old image receive cleanup in split mode.
		for _, img := range cleanedImages {
			log.Info().
				Str("notify", "yes").
				Str("container_name", img.ContainerName).
				Str("image_name", img.ImageName).
				Str("image_id", img.ImageID.ShortID()).
				Msg(logging.RemovingImage)
		}
	}

	// inCooldown reports whether a skipped container waits out its update cooldown.
	inCooldown := func(r types.ContainerReport) bool {
		status, ok := r.(*state.ContainerStatus)

		return ok && status.CooldownDelay() != ""
	}

	passes := []struct {
		include  func(types.ContainerReport) bool
		category string
		reports  []types.ContainerReport
	}{
		{include: nil, category: "updated", reports: result.Updated()},
		{include: nil, category: "restarted", reports: result.Restarted()},
		{include: nil, category: "failed", reports: result.Failed()},
		{include: types.ContainerReport.IsMonitorOnly, category: "monitor-only stale", reports: result.Stale()},
		{include: inCooldown, category: "cooldown-skipped", reports: result.Skipped()},
	}

	for _, pass := range passes {
		notifyContainers(notifyContainersParams{
			log:           log,
			notifier:      notifier,
			notified:      notified,
			reports:       pass.reports,
			result:        result,
			category:      pass.category,
			logBeforeSend: !notificationReport,
			include:       pass.include,
		})
	}

	log.Debug().Msg("Finished sending notifications")
}

// notifyContainersParams groups inputs for notifyContainers.
type notifyContainersParams struct {
	notifier      Notifier
	result        types.Report
	log           *zerolog.Logger
	notified      map[string]bool
	include       func(types.ContainerReport) bool
	category      string
	reports       []types.ContainerReport
	logBeforeSend bool
}

// notifyContainers sends per-container notifications for a report collection.
//
// It applies nil and empty-name filtering, container-ID de-duplication via notified,
// optional category eligibility (include, nil for every container), and
// ShouldSendNotification before send.
//
// Parameters:
//   - params: Named inputs for the category-specific notification pass.
func notifyContainers(params notifyContainersParams) {
	for _, report := range params.reports {
		if report == nil {
			params.log.Debug().Msg("Encountered nil " + params.category + " container report, skipping")

			continue
		}

		if strings.TrimSpace(report.Name()) == "" {
			params.log.Debug().
				Str("container_id", report.ID().ShortID()).
				Msg("Encountered " + params.category + " container with empty name, skipping notification")

			continue
		}

		if params.include != nil && !params.include(report) {
			continue
		}

		containerID := string(report.ID())
		if params.notified[containerID] {
			continue
		}

		if params.logBeforeSend {
			params.log.Debug().
				Str("container", report.Name()).
				Str("image", report.ImageName()).
				Msg("Sending individual notification for " + params.category + " container")
		}

		singleContainerReport := state.NewSingleContainerReport(report, params.result)
		if params.notifier.ShouldSendNotification(singleContainerReport) {
			params.notifier.SendNotification(singleContainerReport)
		}

		params.notified[containerID] = true
	}
}

// generateAndLogMetric creates a metric from the update results and logs it.
//
// It builds a session summary metric and writes an Info completion line on the
// process logger. The line carries notify=no so the Shoutrrr hook does not send
// a second notification after SendNotification has already flushed the session
// batch. Without that field, legacy templates would emit a standalone
// "Update session completed" message with only the scanned, updated, failed,
// and skipped counts.
//
// Parameters:
//   - log: Process logger. Required and must be non-nil.
//   - result: The report containing the results of the update operation.
//
// Returns:
//   - *metrics.Metric: A pointer to a metric object summarizing the update session.
func generateAndLogMetric(log *zerolog.Logger, result types.Report) *metrics.Metric {
	metricResults := newScanMetric(result)

	// Process log only. Session content was already notified via sendNotifications.
	log.Info().
		Str("notify", "no").
		Int("scanned", metricResults.Scanned).
		Int("updated", metricResults.Updated).
		Int("failed", metricResults.Failed).
		Int("skipped", metricResults.Skipped).
		Msg("Update session completed")

	return metricResults
}

// newScanMetric counts the containers in each category of a session report.
//
// Parameters:
//   - report: The session report. It must not be nil.
//
// Returns:
//   - *metrics.Metric: The session's counts, with ScanFailed unset.
func newScanMetric(report types.Report) *metrics.Metric {
	return &metrics.Metric{
		Scanned:   len(report.Scanned()),
		Updated:   len(report.Updated()),
		Failed:    len(report.Failed()),
		Restarted: len(report.Restarted()),
		Skipped:   len(report.Skipped()),
	}
}
