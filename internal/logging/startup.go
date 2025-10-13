// Package logging provides functions for logging startup information and configuring startup logging in Watchtower.
// It handles the initialization messages, notifier setup logging, and schedule information display.
package logging

import (
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/notifications"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// WriteStartupMessage logs or notifies startup information based on configuration flags.
//
// It reports Watchtower's version, notification setup, container filtering details, scheduling information,
// and HTTP API status, providing users with a comprehensive overview of the application's initial state.
//
// Parameters:
//   - c: The cobra.Command instance, providing access to flags like --no-startup-message.
//   - sched: The time.Time of the first scheduled run, or zero if no schedule is set.
//   - filtering: A string describing the container filter applied (e.g., "Watching all containers").
//   - scope: The scope name for structured logging, empty string if no scope is set.
//   - client: The Docker client instance used to retrieve API version information.
//   - notifier: The notification system instance for sending startup messages.
//   - watchtowerVersion: The version string of Watchtower to include in startup messages.
func WriteStartupMessage(
	c *cobra.Command,
	sched time.Time,
	filtering string,
	scope string,
	client container.Client,
	notifier types.Notifier,
	watchtowerVersion string,
) {
	// Retrieve flags controlling startup message behavior and API setup.
	noStartupMessage, _ := c.PersistentFlags().GetBool("no-startup-message")
	enableUpdateAPI, _ := c.PersistentFlags().GetBool("http-api-update")

	apiListenAddr, _ := c.PersistentFlags().GetString("http-api-host")

	apiPort, _ := c.PersistentFlags().GetString("http-api-port")
	if apiPort == "" {
		apiPort = "8080"
	}

	if apiListenAddr == "" {
		apiListenAddr = ":" + apiPort
	} else {
		apiListenAddr = apiListenAddr + ":" + apiPort
	}

	// If startup messages are suppressed, skip all logging
	if noStartupMessage {
		return
	}

	// Configure the logger based on whether startup messages should be suppressed.
	startupLog := SetupStartupLogger(noStartupMessage, notifier)

	var apiVersion string
	if client != nil {
		apiVersion = client.GetVersion()
	}

	startupLog.Info("Watchtower ", watchtowerVersion, " using Docker API v", apiVersion)

	// Log details about configured notifiers or lack thereof.
	var notifierNames []string
	if notifier != nil {
		notifierNames = notifier.GetNames()
	}

	LogNotifierInfo(startupLog, notifierNames)

	// Log filtering information, using structured logging for scope when set
	if scope != "" {
		startupLog.WithField("scope", scope).Info("Only checking containers in scope")
	} else {
		startupLog.Debug(filtering)
	}

	// Log scheduling or run mode information based on configuration.
	LogScheduleInfo(startupLog, c, sched)

	// Report HTTP API status if enabled.
	if enableUpdateAPI {
		startupLog.Info(fmt.Sprintf("The HTTP API is enabled at %s.", apiListenAddr))
	}

	// Send batched notifications if not suppressed, ensuring startup info reaches users.
	if !noStartupMessage && notifier != nil {
		notifier.SendNotification(nil)
	}

	// Warn about trace-level logging if enabled, as it may expose sensitive data.
	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		startupLog.Warn(
			"Trace level enabled: log will include sensitive information as credentials and tokens",
		)
	}
}

// SetupStartupLogger configures the logger for startup messages based on message suppression settings.
//
// It uses a local log entry if messages are suppressed (--no-startup-message), otherwise batches messages
// via the notifier for consolidated delivery, ensuring flexibility in how startup info is presented.
//
// Parameters:
//   - noStartupMessage: A boolean indicating whether startup messages should be logged locally only.
//   - notifier: The notification system instance for batching messages.
//
// Returns:
//   - *logrus.Entry: A configured log entry for writing startup messages.
func SetupStartupLogger(noStartupMessage bool, notifier types.Notifier) *logrus.Entry {
	if noStartupMessage {
		return notifications.LocalLog
	}

	log := logrus.NewEntry(logrus.StandardLogger())

	if notifier != nil {
		notifier.StartNotification()
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

// LogScheduleInfo logs information about the scheduling or run mode configuration.
//
// It handles scheduled runs with timing details, one-time updates, or indicates no periodic runs,
// ensuring users understand when and how updates will occur.
//
// Parameters:
//   - log: The logrus.Entry used to write the schedule information.
//   - c: The cobra.Command instance, providing access to flags like --run-once.
//   - sched: The time.Time of the first scheduled run, or zero if no schedule is set.
func LogScheduleInfo(log *logrus.Entry, c *cobra.Command, sched time.Time) {
	switch {
	case !sched.IsZero(): // scheduled runs
		until := util.FormatDuration(time.Until(sched))
		log.Info("Scheduling next run: " + sched.Format("2006-01-02 15:04:05 -0700 MST"))
		log.Info("Note that the next check will be performed in " + until)

	case func() bool { // one-time updates
		v, _ := c.PersistentFlags().GetBool("run-once")

		return v
	}():
		log.Info("Running a one time update.")

	case func() bool { // update on start
		v, _ := c.PersistentFlags().GetBool("update-on-start")

		return v
	}():
		log.Info("Running update on start, then scheduling periodic updates.")

	case func() bool { // HTTP API without periodic polling
		a, _ := c.PersistentFlags().GetBool("http-api-update")
		b, _ := c.PersistentFlags().GetBool("http-api-periodic-polls")

		return a && !b
	}():
		log.Info("Updates via HTTP API enabled. Periodic updates are not enabled.")

	case func() bool { // HTTP API with periodic polling
		a, _ := c.PersistentFlags().GetBool("http-api-update")
		b, _ := c.PersistentFlags().GetBool("http-api-periodic-polls")

		return a && b
	}():
		log.Info("Updates via HTTP API enabled. Periodic updates are also enabled.")

	default: // default periodic
		log.Info("Periodic updates are enabled with default schedule.")
	}
}
