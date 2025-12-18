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
//   - updateOnStart: The actual update-on-start value, or nil to read from flags.
func WriteStartupMessage(
	c *cobra.Command,
	sched time.Time,
	filtering string,
	scope string,
	client types.Client,
	notifier types.Notifier,
	watchtowerVersion string,
	updateOnStart *bool,
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

	// Log comprehensive host information if enabled
	includeHostInfo, _ := c.PersistentFlags().GetBool("include-host-info")
	if includeHostInfo && client != nil {
		LogHostInfo(startupLog, client)
	}

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
	LogScheduleInfo(startupLog, c, sched, updateOnStart)

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
			"Trace-level logging enabled: Sensitive credentials and tokens may be included in logs",
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

// LogHostInfo logs comprehensive host information including system info, version info, and disk usage.
//
// It retrieves and displays key details like host OS, Docker version, kernel version, and disk usage summary
// to provide users with detailed information about the host environment.
//
// Parameters:
//   - log: The logrus.Entry used to write the host information.
//   - client: The Docker client instance used to retrieve host information.
func LogHostInfo(log *logrus.Entry, client types.Client) {
	// Retrieve system information
	if sysInfo, err := client.GetInfo(); err == nil {
		log.Info(fmt.Sprintf("Host OS: %s %s", sysInfo.OperatingSystem, sysInfo.OSType))
		log.Info("Docker Server Version: " + sysInfo.ServerVersion)

		// Log registry configuration if available
		if sysInfo.RegistryConfig != nil {
			if len(sysInfo.RegistryConfig.Mirrors) > 0 {
				log.Info("Registry Mirrors: " + strings.Join(sysInfo.RegistryConfig.Mirrors, ", "))
			}

			if len(sysInfo.RegistryConfig.InsecureRegistryCIDRs) > 0 {
				log.Info(
					"Insecure Registry CIDRs: " + strings.Join(
						sysInfo.RegistryConfig.InsecureRegistryCIDRs,
						", ",
					),
				)
			}
		}
	} else {
		log.Debug(fmt.Sprintf("Failed to retrieve system info: %v", err))
	}

	// Retrieve version information
	if versionInfo, err := client.GetServerVersion(); err == nil {
		log.Info("Kernel Version: " + versionInfo.KernelVersion)
		log.Info("Docker Version: " + versionInfo.Version)
		log.Info("Go Version: " + versionInfo.GoVersion)
		log.Info("Architecture: " + versionInfo.Arch)
	} else {
		log.Debug(fmt.Sprintf("Failed to retrieve version info: %v", err))
	}

	// Retrieve disk usage information
	if diskUsage, err := client.GetDiskUsage(); err == nil {
		totalSize := diskUsage.LayersSize
		imageCount := len(diskUsage.Images)
		containerCount := len(diskUsage.Containers)
		volumeCount := len(diskUsage.Volumes)
		log.Info(fmt.Sprintf("Disk Usage: %d bytes used by %d images, %d containers, %d volumes",
			totalSize, imageCount, containerCount, volumeCount))
	} else {
		log.Debug(fmt.Sprintf("Failed to retrieve disk usage: %v", err))
	}
}

// GetHostContextFields returns logrus fields with host context information for debugging.
//
// It retrieves OS type, Docker version, and architecture from the Docker client
// to provide valuable debugging information in error logs.
//
// Parameters:
//   - client: The Docker client instance used to retrieve host information (can be nil).
//
// Returns:
//   - logrus.Fields: Fields containing host_os, docker_version, and architecture.
func GetHostContextFields(client types.Client) logrus.Fields {
	fields := logrus.Fields{}

	if client == nil {
		return fields
	}

	if sysInfo, err := client.GetInfo(); err == nil {
		fields["host_os"] = fmt.Sprintf("%s %s", sysInfo.OperatingSystem, sysInfo.OSType)
		fields["docker_version"] = sysInfo.ServerVersion
	}

	if versionInfo, err := client.GetServerVersion(); err == nil {
		fields["architecture"] = versionInfo.Arch
	}

	return fields
}

// LogScheduleInfo logs information about the scheduling or run mode configuration.
//
// It handles scheduled runs with timing details, one-time updates, or indicates no periodic runs,
// ensuring users understand when and how updates will occur. It also warns about flag conflicts
// such as when both --run-once and --update-on-start are enabled.
//
// Parameters:
//   - log: The logrus.Entry used to write the schedule information.
//   - c: The cobra.Command instance, providing access to flags like --run-once.
//   - sched: The time.Time of the first scheduled run, or zero if no schedule is set.
//   - updateOnStart: The actual update-on-start value, or nil to read from flags.
func LogScheduleInfo(log *logrus.Entry, c *cobra.Command, sched time.Time, updateOnStart *bool) {
	// Obtain flag values for run-once.
	runOnce, _ := c.PersistentFlags().GetBool("run-once")

	// Use provided updateOnStart value if not nil, otherwise read from flags.
	var updateOnStartVal bool
	if updateOnStart != nil {
		updateOnStartVal = *updateOnStart
	} else {
		updateOnStartVal, _ = c.PersistentFlags().GetBool("update-on-start")
	}

	// Check if run-once is enabled.
	if runOnce {
		// Warn if disregarding update-on-start when already performing on-time update
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

	// Retrieve HTTP API related flags.
	httpAPI, _ := c.PersistentFlags().GetBool("http-api-update")
	periodicPolls, _ := c.PersistentFlags().GetBool("http-api-periodic-polls")

	// Handle HTTP API update configurations.
	if httpAPI {
		if periodicPolls {
			log.Info("HTTP API and periodic updates enabled")
		} else {
			log.Info("HTTP API enabled and periodic updates disabled")

			return
		}
	}

	// Log details of the next scheduled run if scheduling is active.
	if !sched.IsZero() {
		until := util.FormatDuration(time.Until(sched))
		// Example: Next scheduled run: 2025-10-22 00:31:25 MST in 24 hours.
		log.Info(
			"Next scheduled run: " + sched.Format(
				"2006-01-02 15:04:05 MST",
			) + " in " + until,
		)
	}

	// Default periodic updates are enabled.
	if !updateOnStartVal && !httpAPI && sched.IsZero() {
		log.Info("Periodic updates are enabled with default schedule")
	}
}
