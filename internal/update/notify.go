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
)

// Notifier sends the notifications of an update session.
type Notifier interface {
	// StartSessionNotification starts collecting log entries for the session's
	// notifications. Entries an earlier owner left collected, such as a check
	// request's, are sent first. The returned send sends a report with the
	// session's collected entries. A split report's notification carries only
	// its container's entries. Any other report ends the session's batch. Send
	// does nothing once the batch has ended. Unless the batch has already
	// ended, the returned end sends the entries no notification took, without a
	// report.
	StartSessionNotification() (send func(*state.Report), end func())
	// ShouldSendNotification reports whether report is worth a notification at
	// the configured notifications level.
	ShouldSendNotification(report *state.Report) bool
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
// A session that starts after the process was told to stop makes no update.
// When ctx is already canceled, it does nothing and returns nil, so it is not
// reported as a failed scan. A ctx past its deadline still runs and fails, since a timeout is a
// failure the caller should see.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - params: The RunUpdatesWithNotificationsParams struct containing all configuration parameters.
//     params.Logger is required and must be non-nil (nil panics on first log call).
//
// Returns:
//   - *metrics.Metric: A pointer to a metric object summarizing the update session (scanned, updated, failed counts).
//     Nil when ctx was already canceled.
//
//nolint:nonamedreturns // The deferred recover sets the result.
func RunUpdatesWithNotifications(
	ctx context.Context,
	params RunUpdatesWithNotificationsParams,
) (metric *metrics.Metric) {
	log := params.Logger

	log.Debug().Msg("Starting RunUpdatesWithNotifications")

	if errors.Is(ctx.Err(), context.Canceled) {
		log.Debug().Err(ctx.Err()).Msg("Update skipped: shutting down")

		return nil
	}

	// The session owns its batch and sends its reports through it. It ends the
	// batch on every return. The end sends what no notification took, without
	// a report. Deferred before the recover, it also runs after a panicked
	// session.
	var send func(*state.Report)

	if params.Notifier != nil {
		var end func()

		send, end = params.Notifier.StartSessionNotification()
		defer end()
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			metric = endPanickedSession(log, params.EventBroadcaster, recovered)
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
	metric = handleUpdateResult(log, result, err, send)
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
		send,
		params.NotificationSplitByContainer,
		params.NotificationReport,
		result,
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
// It logs an update error at error level. When a phase has already notified the error's cause,
// that line is logged with notify=no, which keeps the failure to one notification. An update that
// fails partway still returns the report of the work done before the error. That report is sent
// through the session's batch and counted. Without a report, nothing is sent here. The session's
// end then sends the batch without a report, which lets report templates list the queued error
// entry. On error or nil result, it returns a metric that marks a failed scan. On success, it
// returns nil.
//
// Parameters:
//   - result: The report from the update operation. It may be partial when err is non-nil.
//   - err: Any error encountered during the update.
//   - send: Sends a report through the session's notification batch. Nil without a notifier.
//
// Returns:
//   - *metrics.Metric: A metric marked as a failed scan, with the counts of the partial report
//     on error, or zero counts when there is no report. Nil on success.
func handleUpdateResult(log *zerolog.Logger, result *state.Report, err error, send func(*state.Report)) *metrics.Metric {
	// Check for errors during update execution
	if err != nil {
		event := log.Error().Err(err)
		if _, reported := errors.AsType[reportedError](err); reported {
			event = event.Str("notify", "no")
		}

		event.Msg(logging.UpdateFailed)

		if result == nil {
			return &metrics.Metric{ScanFailed: true}
		}

		if send != nil {
			send(result)
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
// is logged at debug without notifying. The session's end then sends its
// notification batch, which holds the panic entry, without a report.
// Subscribers get scan_failed.
//
// Parameters:
//   - log: Process logger for the session.
//   - broadcaster: Event broadcaster that receives scan_failed. It may be nil.
//   - recovered: The value the session panicked with.
//
// Returns:
//   - *metrics.Metric: A metric marking a failed scan with zero counts.
func endPanickedSession(
	log *zerolog.Logger,
	broadcaster *events.Broadcaster,
	recovered any,
) *metrics.Metric {
	panicValue := fmt.Sprint(recovered)

	log.Error().Str("error", panicValue).Msg(logging.UpdatePanicked)
	log.Debug().
		Str("notify", "no").
		Str("stack", string(debug.Stack())).
		Msg("Stack of the update panic")

	if broadcaster != nil {
		broadcaster.Publish(events.Event{
			Type:      "scan_failed",
			Timestamp: time.Now().UTC(),
			Data: events.ScanFailedData{
				Error: "update panicked: " + panicValue,
			},
		})
	}

	return &metrics.Metric{ScanFailed: true}
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
//   - *state.Report: Results of the update operation.
//   - []cleanup.RemovedImageInfo: Images eligible for cleanup.
//   - error: Non-nil when Update fails.
func executeUpdate(
	log *zerolog.Logger,
	ctx context.Context,
	client containerClient.Client,
	config updateParams.Params,
	rateLimiter *ratelimit.Limiter,
	gitClient *git.Client,
) (*state.Report, []cleanup.RemovedImageInfo, error) {
	// Log before calling the Update function
	log.Debug().Msg("About to call Update function")

	result, cleanupImageInfos, err := update(log, ctx, client, config, rateLimiter, gitClient)

	// Log after Update function returns
	log.Debug().Msg("Update function returned, about to check cleanup")

	return result, cleanupImageInfos, err
}

// performImageCleanup executes image cleanup if enabled.
//
// It removes old images after updates if the cleanup flag is set. An image
// that several containers shared is removed once. The returned slice has an
// entry for each of those containers. The image_cleanup event then lists each
// container that used a removed image.
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

	return cleaned
}

// logUpdateReport logs the update report details for debugging purposes.
//
// It extracts updated container names and logs comprehensive session statistics.
//
// Parameters:
//   - result: The report containing the results of the update operation.
func logUpdateReport(log *zerolog.Logger, result *state.Report) {
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
// It supports both grouped and per-container notifications based on configuration flags.
// A grouped report below the notifications level is not sent. The session's
// end then sends the lines it queued, without the report.
//
// Parameters:
//   - notifier: Decides which reports are worth a notification. It may be nil.
//   - send: Sends a report through the session's notification batch. Nil when notifier is nil.
//   - notificationSplitByContainer: Boolean flag enabling a separate notification for each container.
//   - notificationReport: Boolean flag enabling report-based notifications.
//   - result: The report containing the results of the update operation.
func sendNotifications(log *zerolog.Logger, notifier Notifier, send func(*state.Report),
	notificationSplitByContainer, notificationReport bool,
	result *state.Report,
) {
	if notifier == nil {
		return
	}

	// notify sends a report through the session's batch when it is worth a
	// notification at the configured notifications level.
	notify := func(report *state.Report) {
		if notifier.ShouldSendNotification(report) {
			send(report)
		}
	}

	if notificationSplitByContainer {
		sendSplitNotifications(log, notify, notificationReport, result)

		return
	}

	notify(result)
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
// notifications. A container without a name is skipped. The session report
// lists each container in one category. No container is notified twice.
//
// Parameters:
//   - notify: Sends a report that is worth a notification through the session's notification batch.
//   - notificationReport: Boolean flag enabling report-based notifications.
//   - result: The report containing the results of the update operation.
func sendSplitNotifications(log *zerolog.Logger, notify func(*state.Report),
	notificationReport bool,
	result *state.Report,
) {
	log.Debug().
		Int("updated_count", len(result.Updated())).
		Int("restarted_count", len(result.Restarted())).
		Int("stale_count", len(result.Stale())).
		Int("failed_count", len(result.Failed())).
		Int("skipped_count", len(result.Skipped())).
		Int("fresh_count", len(result.Fresh())).
		Int("scanned_count", len(result.Scanned())).
		Msg("Split notifications: container counts by category")

	// inCooldown reports whether a skipped container waits out its update cooldown.
	inCooldown := func(status *state.ContainerStatus) bool {
		return status.CooldownDelay() != ""
	}

	passes := []struct {
		include  func(*state.ContainerStatus) bool
		category string
		reports  []*state.ContainerStatus
	}{
		{include: nil, category: "updated", reports: result.Updated()},
		{include: nil, category: "restarted", reports: result.Restarted()},
		{include: nil, category: "failed", reports: result.Failed()},
		{include: (*state.ContainerStatus).IsMonitorOnly, category: "monitor-only stale", reports: result.Stale()},
		{include: inCooldown, category: "cooldown-skipped", reports: result.Skipped()},
	}

	for _, pass := range passes {
		notifyContainers(notifyContainersParams{
			log:           log,
			notify:        notify,
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
	result        *state.Report
	notify        func(*state.Report)
	log           *zerolog.Logger
	include       func(*state.ContainerStatus) bool
	category      string
	reports       []*state.ContainerStatus
	logBeforeSend bool
}

// notifyContainers sends per-container notifications for a report collection.
//
// It skips a container without a name. It also skips one that include
// rejects. A nil include notifies every named container.
//
// Parameters:
//   - params: Named inputs for the category-specific notification pass.
func notifyContainers(params notifyContainersParams) {
	for _, report := range params.reports {
		if strings.TrimSpace(report.Name()) == "" {
			params.log.Debug().
				Str("container_id", report.ID().ShortID()).
				Msg("Encountered " + params.category + " container with empty name, skipping notification")

			continue
		}

		if params.include != nil && !params.include(report) {
			continue
		}

		if params.logBeforeSend {
			params.log.Debug().
				Str("container", report.Name()).
				Str("image", report.ImageName()).
				Msg("Sending individual notification for " + params.category + " container")
		}

		params.notify(state.NewSingleContainerReport(report, params.result))
	}
}

// generateAndLogMetric creates a metric from the update results and logs it.
//
// It builds a session summary metric and writes an Info completion line on the
// process logger. The line carries notify=no so the Shoutrrr hook does not send
// a second notification after the session's report has already been sent.
// Without that field, legacy templates would emit a standalone
// "Update session completed" message with only the scanned, updated, failed,
// and skipped counts.
//
// Parameters:
//   - log: Process logger. Required and must be non-nil.
//   - result: The report containing the results of the update operation.
//
// Returns:
//   - *metrics.Metric: A pointer to a metric object summarizing the update session.
func generateAndLogMetric(log *zerolog.Logger, result *state.Report) *metrics.Metric {
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
func newScanMetric(report *state.Report) *metrics.Metric {
	return &metrics.Metric{
		Scanned:   len(report.Scanned()),
		Updated:   len(report.Updated()),
		Failed:    len(report.Failed()),
		Restarted: len(report.Restarted()),
		Skipped:   len(report.Skipped()),
	}
}
