// Package logging provides functions for logging startup information and configuring startup logging in Watchtower.
// It handles the initialization messages, notifier setup logging, and schedule information display.
package logging

import (
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// StartupParams holds resolved process values for startup messaging.
//
// Callers must populate these from config.Load output. Do not read CLI flags here.
// Schedule and mode fields live on the embedded ScheduleInfo (single source of truth).
type StartupParams struct {
	// ScheduleInfo holds run-once, update-on-start, HTTP API, and next-run schedule values.
	ScheduleInfo

	// NoStartupMessage suppresses all startup logs and notifications when true.
	NoStartupMessage bool
	// Filtering is a human-readable description of the container filter.
	Filtering string
	// Scope is the operational scope name, or empty when unset.
	Scope string
	// Client is the Docker client used for API version reporting.
	Client container.Client
	// Notifier sends batched startup messages when not suppressed.
	Notifier types.Notifier
	// Version is the Watchtower version string.
	Version string
}

// WriteStartupMessage logs or notifies startup information from resolved configuration.
//
// It reports Watchtower's version, notification setup, container filtering details, scheduling information,
// and HTTP API status, providing users with a comprehensive overview of the application's initial state.
//
// Parameters:
//   - params: Resolved startup messaging inputs from config.Load (no CLI flag reads).
func WriteStartupMessage(params StartupParams) {
	// If startup messages are suppressed, skip all logging and notifier batching.
	if params.NoStartupMessage {
		return
	}

	// Batch startup lines through the notifier when present (suppression already returned).
	startupLog := SetupStartupLogger(params.Notifier)

	var apiVersion string
	if params.Client != nil {
		apiVersion = params.Client.GetVersion()
	}

	startupLog.Info("Watchtower ", params.Version, " using Docker API v", apiVersion)

	// Log details about configured notifiers or lack thereof.
	var notifierNames []string
	if params.Notifier != nil {
		notifierNames = params.Notifier.GetNames()
	}

	LogNotifierInfo(startupLog, notifierNames)

	// Log filtering information, using structured logging for scope when set.
	if params.Scope != "" {
		startupLog.WithField("scope", params.Scope).
			Info("Only checking containers in scope")
	} else {
		startupLog.Debug(params.Filtering)
	}

	// Log scheduling or run mode information based on configuration.
	LogScheduleInfo(startupLog, params.ScheduleInfo)

	// Send batched notifications if not suppressed, ensuring startup info reaches users.
	if params.Notifier != nil {
		params.Notifier.SendNotification(nil)
	}

	// Warn about trace-level logging if enabled, as it may expose sensitive data.
	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		startupLog.Warn(
			"Trace-level logging enabled: Sensitive credentials and tokens may be included in logs",
		)
	}
}

// SetupStartupLogger configures a log entry for startup messages and starts notifier batching.
//
// Callers that suppress startup messages must return before invoking this helper.
// When notifier is non-nil, StartNotification batches subsequent startup lines for
// a single SendNotification.
//
// Parameters:
//   - notifier: The notification system instance for batching messages, or nil.
//
// Returns:
//   - *logrus.Entry: A configured log entry for writing startup messages.
func SetupStartupLogger(notifier types.Notifier) *logrus.Entry {
	log := logrus.NewEntry(logrus.StandardLogger())

	if notifier != nil {
		notifier.StartNotification(false)
	}

	return log
}

// LogNotifierInfo logs details about the notification setup for Watchtower.
//
// It reports the list of configured notifier names (e.g., "email, slack") or indicates no notifications
// are set up, providing visibility into how update statuses will be communicated.
//
// Parameters:
//   - log: The logrus.Entry used to write the notification information.
//   - notifierNames: A slice of strings representing the names of configured notifiers.
func LogNotifierInfo(log *logrus.Entry, notifierNames []string) {
	if len(notifierNames) > 0 {
		log.Info("Using notifications: " + strings.Join(notifierNames, ", "))
	} else {
		log.Info("Using no notifications")
	}
}

// ScheduleInfo holds resolved schedule and mode values for startup schedule messaging.
//
// Values come from config.Load projections rather than CLI flag reads.
type ScheduleInfo struct {
	// RunOnce indicates a single update run then exit.
	RunOnce bool
	// UpdateOnStart is the effective update-on-start value, or nil when unset or false.
	UpdateOnStart *bool
	// HTTPAPIUpdate is true when the HTTP update API is enabled.
	HTTPAPIUpdate bool
	// HTTPAPIPeriodicPolls is true when scheduled polls run with the HTTP API.
	HTTPAPIPeriodicPolls bool
	// Sched is the time of the first scheduled run, or zero if none.
	Sched time.Time
}

// LogScheduleInfo logs information about the scheduling or run mode configuration.
//
// It handles scheduled runs with timing details, one-time updates, or indicates no periodic runs,
// ensuring users understand when and how updates will occur. It also warns about flag conflicts
// such as when both run-once and update-on-start are enabled.
//
// Parameters:
//   - log: The logrus.Entry used to write the schedule information.
//   - info: Resolved schedule and mode values (no flag reads).
func LogScheduleInfo(log *logrus.Entry, info ScheduleInfo) {
	// Use provided update-on-start value when set; otherwise treat as disabled.
	var updateOnStartVal bool
	if info.UpdateOnStart != nil {
		updateOnStartVal = *info.UpdateOnStart
	}

	// Check if run-once is enabled.
	if info.RunOnce {
		// Warn if disregarding update-on-start when already performing a one-time update.
		if updateOnStartVal {
			log.Warn("Run once mode: Disregarding update on start")
		} else {
			log.Info("Running a one time update")
		}

		return
	}

	// Check if update on start is enabled.
	if updateOnStartVal {
		log.Info(
			"Update on startup enabled: Performing immediate check",
		)
	}

	// Handle HTTP API update configurations.
	if info.HTTPAPIUpdate {
		if info.HTTPAPIPeriodicPolls {
			log.Info("HTTP API and periodic updates enabled")
		} else {
			log.Info("HTTP API enabled and periodic updates disabled")

			return
		}
	}

	// Log details of the next scheduled run if scheduling is active.
	if !info.Sched.IsZero() {
		until := util.FormatDuration(time.Until(info.Sched))
		// Example: Next scheduled run: 2025-10-22 00:31:25 MST in 24 hours.
		log.Info(
			"Next scheduled run: " + info.Sched.Format(
				"2006-01-02 15:04:05 MST",
			) + " in " + until,
		)
	}

	// Default periodic updates are enabled.
	if !updateOnStartVal && !info.HTTPAPIUpdate && info.Sched.IsZero() {
		log.Info("Periodic updates are enabled with default schedule")
	}
}
