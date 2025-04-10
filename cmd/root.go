// Package cmd contains the command-line interface (CLI) definitions and execution logic for Watchtower.
// It provides the root command and its subcommands, orchestrating the application's core functionality,
// including container updates, Docker client interactions, notification handling, and scheduling. This package
// serves as the primary entry point for the Watchtower CLI, integrating various components to automate
// Docker container management based on user-specified configurations.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/robfig/cron"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/internal/meta"
	pkgApi "github.com/nicholas-fedor/watchtower/pkg/api"
	metricsAPI "github.com/nicholas-fedor/watchtower/pkg/api/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/api/update"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/notifications"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// client is the Docker client instance used to interact with container operations in Watchtower.
//
// It provides an interface for listing, stopping, starting, and managing containers, initialized during
// the preRun phase with options derived from command-line flags and environment variables such as
// DOCKER_HOST, DOCKER_TLS_VERIFY, and DOCKER_API_VERSION.
var client container.Client

// scheduleSpec holds the cron-formatted schedule string that dictates when periodic container updates occur.
//
// It is populated during preRun from the --schedule flag or the WATCHTOWER_SCHEDULE environment variable,
// supporting formats like "@every 1h" or standard cron syntax (e.g., "0 0 * * * *") for flexible scheduling.
var scheduleSpec string

// cleanup is a boolean flag determining whether to remove old images after a container update.
//
// It is set during preRun via the --cleanup flag or the WATCHTOWER_CLEANUP environment variable,
// enabling disk space management by deleting outdated images post-update.
var cleanup bool

// noRestart is a boolean flag that prevents containers from being restarted after an update.
//
// It is configured in preRun via the --no-restart flag or the WATCHTOWER_NO_RESTART environment variable,
// useful when users prefer manual restart control or want to minimize downtime during updates.
var noRestart bool

// noPull is a boolean flag that skips pulling new images from the registry during updates.
//
// It is enabled in preRun via the --no-pull flag or the WATCHTOWER_NO_PULL environment variable,
// allowing updates to proceed using only locally cached images, potentially reducing network usage.
var noPull bool

// monitorOnly is a boolean flag enabling a mode where Watchtower monitors containers without updating them.
//
// It is set in preRun via the --monitor-only flag or the WATCHTOWER_MONITOR_ONLY environment variable,
// ideal for observing image staleness without triggering automatic updates.
var monitorOnly bool

// enableLabel is a boolean flag restricting updates to containers with the "com.centurylinklabs.watchtower.enable" label set to true.
//
// It is configured in preRun via the --label-enable flag or the WATCHTOWER_LABEL_ENABLE environment variable,
// providing granular control over which containers are targeted for updates.
var enableLabel bool

// disableContainers is a slice of container names explicitly excluded from updates.
//
// It is populated in preRun from the --disable-containers flag or the WATCHTOWER_DISABLE_CONTAINERS environment variable,
// allowing users to blacklist specific containers from Watchtower’s operations.
var disableContainers []string

// notifier is the notification system instance responsible for sending update status messages to configured channels.
//
// It is initialized in preRun with notification types specified via flags (e.g., --notifications), supporting
// multiple methods like email, Slack, or MSTeams to inform users about update successes, failures, or skips.
var notifier types.Notifier

// timeout specifies the maximum duration allowed for container stop operations during updates.
//
// It defaults to a value defined in the flags package and can be overridden in preRun via the --timeout flag or
// WATCHTOWER_TIMEOUT environment variable, ensuring containers are stopped gracefully within a specified time limit.
var timeout time.Duration

// lifecycleHooks is a boolean flag enabling the execution of pre- and post-update lifecycle hook commands.
//
// It is set in preRun via the --enable-lifecycle-hooks flag or the WATCHTOWER_LIFECYCLE_HOOKS environment variable,
// allowing custom scripts to run at specific update stages for additional validation or actions.
var lifecycleHooks bool

// rollingRestart is a boolean flag enabling rolling restarts, updating containers sequentially rather than all at once.
//
// It is configured in preRun via the --rolling-restart flag or the WATCHTOWER_ROLLING_RESTART environment variable,
// reducing downtime by restarting containers one-by-one during updates.
var rollingRestart bool

// scope defines a specific operational scope for Watchtower, limiting updates to containers matching this scope.
//
// It is set in preRun via the --scope flag or the WATCHTOWER_SCOPE environment variable, useful for isolating
// Watchtower’s actions to a subset of containers (e.g., a project or environment).
var scope string

// labelPrecedence is a boolean flag giving container label settings priority over global command-line flags.
//
// It is enabled in preRun via the --label-take-precedence flag or the WATCHTOWER_LABEL_PRECEDENCE environment variable,
// allowing container-specific configurations to override broader settings for flexibility.
var labelPrecedence bool

// rootCmd represents the root command for the Watchtower CLI, serving as the entry point for all subcommands.
//
// It defines the base usage string, short and long descriptions, and assigns lifecycle hooks (PreRun and Run)
// to manage setup and execution, initialized with default behavior and configured via flags during runtime.
var rootCmd = NewRootCommand()

// RunConfig encapsulates the configuration parameters for the runMain function.
//
// It aggregates command-line flags and derived settings into a single structure, providing a cohesive way
// to pass configuration data through the CLI execution flow, ensuring all necessary parameters are accessible
// for update operations, API setup, and scheduling.
type RunConfig struct {
	// Command is the cobra.Command instance representing the executed command, providing access to parsed flags.
	Command *cobra.Command
	// Names is a slice of container names explicitly provided as positional arguments, used for filtering.
	Names []string
	// Filter is the types.Filter function determining which containers are processed during updates.
	Filter types.Filter
	// FilterDesc is a human-readable description of the applied filter, used in logging and notifications.
	FilterDesc string
	// RunOnce indicates whether to perform a single update and exit, set via the --run-once flag.
	RunOnce bool
	// EnableUpdateAPI enables the HTTP update API endpoint, set via the --http-api-update flag.
	EnableUpdateAPI bool
	// EnableMetricsAPI enables the HTTP metrics API endpoint, set via the --http-api-metrics flag.
	EnableMetricsAPI bool
	// UnblockHTTPAPI allows periodic polling alongside the HTTP API, set via the --http-api-periodic-polls flag.
	UnblockHTTPAPI bool
	// APIToken is the authentication token for HTTP API access, set via the --http-api-token flag.
	APIToken string
	// APIPort is the port for the HTTP API server, set via the --http-api-port flag (defaults to "8080").
	APIPort string
}

// NewRootCommand creates and configures the root command for the Watchtower CLI.
//
// It establishes the base usage string ("watchtower"), a short description summarizing its purpose,
// and a long description with additional context and a project URL.
//
// It assigns the PreRun and Run functions to handle setup and execution, respectively, and allows arbitrary arguments for flexibility.
//
// Returns:
//   - *cobra.Command: A pointer to the fully configured root command, ready for flag registration and execution.
func NewRootCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "watchtower",
		Short:  "Automatically updates running Docker containers",
		Long:   "\nWatchtower automatically updates running Docker containers whenever a new image is released.\nMore information available at https://github.com/nicholas-fedor/watchtower/.",
		Run:    run,
		PreRun: preRun,
		Args:   cobra.ArbitraryArgs, // Permits any number of positional arguments, processed as container names later.
	}
}

// init registers command-line flags for the root command during package initialization.
//
// It invokes functions from the flags package to set default values and register flags for Docker configuration
// (e.g., --host), system behavior (e.g., --interval), and notifications (e.g., --notifications), establishing
// the CLI’s configurable parameters before execution begins.
func init() {
	flags.SetDefaults()
	flags.RegisterDockerFlags(rootCmd)
	flags.RegisterSystemFlags(rootCmd)
	flags.RegisterNotificationFlags(rootCmd)
}

// Execute runs the root command and manages any errors encountered during its execution.
//
// It serves as the primary entry point for the Watchtower CLI, called from main.go, and ensures that any
// fatal errors are logged and terminate the program with an appropriate exit status, providing a clean
// interface between the CLI and the operating system.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		logrus.WithError(err).Fatal("Failed to execute root command")
	}
}

// preRun prepares the environment and configuration before the main command execution begins.
//
// It processes command-line flags and their aliases, configures logging based on verbosity settings,
// initializes the Docker client and notification system, retrieves operational flags, and validates
// flag combinations to ensure Watchtower is correctly set up for its tasks.
//
// Parameters:
//   - cmd: The cobra.Command instance being executed, providing access to parsed flags.
//   - _: A slice of string arguments (unused here, as container names are handled in run).
func preRun(cmd *cobra.Command, _ []string) {
	flagsSet := cmd.PersistentFlags()
	flags.ProcessFlagAliases(flagsSet)

	// Setup logging based on flags such as --debug, --trace, and --log-format.
	if err := flags.SetupLogging(flagsSet); err != nil {
		logrus.WithError(err).Fatal("Failed to initialize logging")
	}

	// Get the cron schedule specification from flags or environment variables.
	scheduleSpec, _ = flagsSet.GetString("schedule")

	// Get secrets from files (e.g., for notifications) and read core operational flags.
	flags.GetSecretsFromFiles(cmd)
	cleanup, noRestart, monitorOnly, timeout = flags.ReadFlags(cmd)

	// Validate the timeout value to ensure it’s non-negative, preventing invalid stop durations.
	if timeout < 0 {
		logrus.Fatal("Please specify a positive value for timeout value.")
	}

	// Set additional configuration flags that control update behavior and scope.
	enableLabel, _ = flagsSet.GetBool("label-enable")
	disableContainers, _ = flagsSet.GetStringSlice("disable-containers")
	lifecycleHooks, _ = flagsSet.GetBool("enable-lifecycle-hooks")
	rollingRestart, _ = flagsSet.GetBool("rolling-restart")
	scope, _ = flagsSet.GetString("scope")
	labelPrecedence, _ = flagsSet.GetBool("label-take-precedence")

	// Log the scope if specified, aiding debugging by confirming the operational boundary.
	if scope != "" {
		logrus.WithField("scope", scope).Debug("Configured operational scope")
	}

	// Set Docker environment variables (e.g., DOCKER_HOST) based on flags for client initialization.
	err := flags.EnvConfig(cmd)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to configure Docker environment")
	}

	// Retrieve flags controlling container inclusion and image handling behavior.
	noPull, _ = flagsSet.GetBool("no-pull")
	includeStopped, _ := flagsSet.GetBool("include-stopped")
	includeRestarting, _ := flagsSet.GetBool("include-restarting")
	reviveStopped, _ := flagsSet.GetBool("revive-stopped")
	removeVolumes, _ := flagsSet.GetBool("remove-volumes")
	warnOnHeadPullFailed, _ := flagsSet.GetString("warn-on-head-failure")

	// Warn about potential redundancy in flag combinations that could result in no action.
	if monitorOnly && noPull {
		logrus.WithFields(logrus.Fields{
			"monitor_only": monitorOnly,
			"no_pull":      noPull,
		}).Warn("Combining monitor-only and no-pull might result in no updates")
	}

	// Initialize the Docker client with options reflecting the desired container handling behavior.
	client = container.NewClient(container.ClientOptions{
		IncludeStopped:    includeStopped,
		ReviveStopped:     reviveStopped,
		RemoveVolumes:     removeVolumes,
		IncludeRestarting: includeRestarting,
		WarnOnHeadFailed:  container.WarningStrategy(warnOnHeadPullFailed),
	})

	// Set up the notification system with types specified via flags (e.g., email, Slack).
	notifier = notifications.NewNotifier(cmd)
	notifier.AddLogHook()
}

// run executes the main Watchtower logic based on parsed command-line flags.
//
// It determines the operational mode (one-time update, HTTP API, or scheduled updates), builds
// the container filter, and delegates to runMain for core execution, exiting with a status code
// based on the outcome (0 for success, non-zero for failure).
//
// This function bridges flag parsing and the application’s primary workflow.
//
// Parameters:
//   - c: The cobra.Command instance being executed, providing access to parsed flags.
//   - names: A slice of container names provided as positional arguments, used for filtering.
func run(c *cobra.Command, names []string) {
	// Build the filter and its description based on names, exclusions, and label settings.
	filter, filterDesc := filters.BuildFilter(names, disableContainers, enableLabel, scope)

	// Get flags controlling execution mode and HTTP API behavior.
	runOnce, _ := c.PersistentFlags().GetBool("run-once")
	enableUpdateAPI, _ := c.PersistentFlags().GetBool("http-api-update")
	enableMetricsAPI, _ := c.PersistentFlags().GetBool("http-api-metrics")
	unblockHTTPAPI, _ := c.PersistentFlags().GetBool("http-api-periodic-polls")
	apiToken, _ := c.PersistentFlags().GetString("http-api-token")
	healthCheck, _ := c.PersistentFlags().GetBool("health-check")

	// Get the HTTP API port, falling back to "8080" if not specified.
	flagsSet := c.PersistentFlags()

	apiPort, err := flagsSet.GetString("http-api-port")
	if err != nil {
		logrus.WithError(err).Fatal("Failed to get http-api-port flag")
	}

	if apiPort == "" {
		apiPort = "8080" // Default port if unset.
	}

	// Handle health check mode as an early exit, preventing updates or API setup.
	if healthCheck {
		if os.Getpid() == 1 {
			time.Sleep(1 * time.Second)
			logrus.Fatal(
				"The health check flag should never be passed to the main watchtower container process",
			)
		}

		return // Exit early without os.Exit to preserve defer in caller.
	}

	// Set configuration for core execution, encapsulating all operational parameters.
	cfg := RunConfig{
		Command:          c,
		Names:            names,
		Filter:           filter,
		FilterDesc:       filterDesc,
		RunOnce:          runOnce,
		EnableUpdateAPI:  enableUpdateAPI,
		EnableMetricsAPI: enableMetricsAPI,
		UnblockHTTPAPI:   unblockHTTPAPI,
		APIToken:         apiToken,
		APIPort:          apiPort,
	}

	// Execute core logic and exit with the returned status code (0 for success, 1 for failure).
	if exitCode := runMain(cfg); exitCode != 0 {
		logrus.WithField("exit_code", exitCode).Debug("Exiting with non-zero status")
		os.Exit(exitCode)
	}
}

// runMain contains the core Watchtower logic after early exits are handled.
//
// It validates the environment, performs one-time updates if specified, sets up the HTTP API,
// and schedules periodic updates, managing context and concurrency to ensure graceful operation.
//
// Parameters:
//   - cfg: The RunConfig struct containing all necessary configuration parameters for execution.
//
// Returns:
//   - int: An exit code (0 for success, 1 for failure) used to terminate the program.
func runMain(cfg RunConfig) int {
	// Log the container names being processed for debugging visibility.
	logrus.WithField("names", cfg.Names).Debug("Processing specified containers")

	// Validate flag compatibility to prevent conflicting operational modes (e.g., rolling restarts with monitor-only).
	if rollingRestart && monitorOnly {
		logrus.WithFields(logrus.Fields{
			"rolling_restart": rollingRestart,
			"monitor_only":    monitorOnly,
		}).Fatal("Incompatible flags: rolling restarts and monitor-only")
	}

	// Ensure the Docker client is fully initialized before proceeding.
	awaitDockerClient()

	// Perform sanity checks on the Docker environment and container setup to catch misconfigurations.
	if err := actions.CheckForSanity(client, cfg.Filter, rollingRestart); err != nil {
		logNotify("Sanity check failed", err)

		return 1
	}

	// Handle one-time update mode, executing updates immediately and exiting cleanly.
	if cfg.RunOnce {
		writeStartupMessage(cfg.Command, time.Time{}, cfg.FilterDesc)
		runUpdatesWithNotifications(cfg.Filter)
		notifier.Close()

		return 0
	}

	// Check for and resolve conflicts with multiple Watchtower instances running concurrently.
	if err := actions.CheckForMultipleWatchtowerInstances(client, cleanup, scope); err != nil {
		logNotify("Multiple Watchtower instances detected", err)

		return 1
	}

	// Setup API and scheduling by initializing a lock channel to prevent concurrent conflicting updates.
	updateLock := make(chan bool, 1)
	updateLock <- true

	// Create a cancellable context for managing API and scheduler shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Configure and start the HTTP API, handling any startup errors gracefully.
	if err := setupAndStartAPI(ctx, cfg, updateLock); err != nil {
		return 1 // Error logged in setupAndStartAPI
	}

	// Schedule and execute periodic updates, returning on error or shutdown.
	if err := runUpgradesOnSchedule(ctx, cfg.Command, cfg.Filter, cfg.FilterDesc, updateLock); err != nil {
		logNotify("Scheduled upgrades failed", err)

		return 1
	}

	// Default to failure if execution completes unexpectedly (e.g., no shutdown signal received).
	return 1
}

// setupAndStartAPI configures and launches the HTTP API if enabled by configuration flags.
//
// It sets up update and metrics endpoints, starts the API server in blocking or non-blocking mode,
// and handles startup errors, ensuring the API integrates seamlessly with Watchtower’s update workflow.
//
// Parameters:
//   - ctx: The context controlling the API’s lifecycle, enabling graceful shutdown on cancellation.
//   - cfg: The RunConfig struct with API-related settings (e.g., token, port, enable flags).
//   - updateLock: A channel ensuring only one update runs at a time, shared with the scheduler.
//
// Returns:
//   - error: An error if the API fails to start (excluding clean shutdown), nil otherwise.
func setupAndStartAPI(ctx context.Context, cfg RunConfig, updateLock chan bool) error {
	// Initialize the HTTP API with the configured authentication token and port.
	httpAPI := pkgApi.New(cfg.APIToken)
	httpAPI.Addr = ":" + cfg.APIPort

	// Register the update API endpoint if enabled, linking it to the update handler.
	if cfg.EnableUpdateAPI {
		updateHandler := update.New(func(images []string) {
			metric := runUpdatesWithNotifications(filters.FilterByImage(images, cfg.Filter))
			metrics.Default().RegisterScan(metric)
		}, updateLock)
		httpAPI.RegisterFunc(updateHandler.Path, updateHandler.Handle)

		if !cfg.UnblockHTTPAPI {
			writeStartupMessage(cfg.Command, time.Time{}, cfg.FilterDesc)
		}
	}

	// Register the metrics API endpoint if enabled, providing access to update metrics.
	if cfg.EnableMetricsAPI {
		metricsHandler := metricsAPI.New()
		httpAPI.RegisterHandler(metricsHandler.Path, metricsHandler.Handle)
	}

	// Start the API server, logging errors unless it’s a clean shutdown triggered by context.
	if err := httpAPI.Start(ctx, cfg.EnableUpdateAPI && !cfg.UnblockHTTPAPI); err != nil &&
		!errors.Is(err, http.ErrServerClosed) {
		logrus.WithError(err).Error("Failed to start API")

		return fmt.Errorf("failed to start HTTP API: %w", err)
	}

	return nil
}

// logNotify logs an error message and ensures notifications are sent before returning control.
//
// It uses a specific message if provided, falling back to a generic one, and includes the error in fields.
//
// Parameters:
//   - msg: A string specifying the error context (e.g., "Sanity check failed"), optional.
//   - err: The error to log and include in notifications.
func logNotify(msg string, err error) {
	if msg == "" {
		msg = "Operation failed"
	}

	logrus.WithError(err).Error(msg)
	notifier.Close()
}

// awaitDockerClient introduces a brief delay to ensure the Docker client is fully initialized.
//
// It pauses execution for one second to mitigate potential race conditions during startup,
// giving the Docker API time to stabilize before Watchtower begins interacting with containers.
func awaitDockerClient() {
	logrus.Debug(
		"Sleeping for a second to ensure the docker api client has been properly initialized.",
	)
	time.Sleep(1 * time.Second)
}

// formatDuration converts a time.Duration into a human-readable string representation.
//
// It breaks down the duration into hours, minutes, and seconds, formatting each unit with appropriate
// grammar (singular or plural) and returning a string like "1 hour, 2 minutes, 3 seconds" or "0 seconds"
// if the duration is zero, ensuring a user-friendly output for logs and notifications.
//
// Parameters:
//   - duration: The time.Duration to convert into a readable string.
//
// Returns:
//   - string: A formatted string representing the duration, always including at least "0 seconds".
func formatDuration(duration time.Duration) string {
	const (
		minutesPerHour   = 60 // Number of minutes in an hour for duration breakdown
		secondsPerMinute = 60 // Number of seconds in a minute for duration breakdown
		timeUnitCount    = 3  // Number of time units (hours, minutes, seconds) for pre-allocation
	)

	// timeUnit represents a single unit of time (hours, minutes, or seconds) with its value and labels.
	type timeUnit struct {
		value    int64  // The numeric value of the unit (e.g., 2 for 2 hours)
		singular string // The singular form of the unit (e.g., "hour")
		plural   string // The plural form of the unit (e.g., "hours")
	}

	// Define units with calculated values from the duration, preserving order for display.
	units := []timeUnit{
		{int64(duration.Hours()), "hour", "hours"},
		{int64(math.Mod(duration.Minutes(), minutesPerHour)), "minute", "minutes"},
		{int64(math.Mod(duration.Seconds(), secondsPerMinute)), "second", "seconds"},
	}

	parts := make([]string, 0, timeUnitCount)
	// Format each unit, forcing inclusion of seconds if no other parts exist to avoid empty output.
	for i, unit := range units {
		parts = append(parts, formatTimeUnit(unit, i == len(units)-1 && len(parts) == 0))
	}

	// Join non-empty parts, ensuring a readable output with proper separators.
	joined := strings.Join(filterEmpty(parts), ", ")
	if joined == "" {
		return "0 seconds" // Default output when duration is zero or all units are skipped.
	}

	return joined
}

// formatTimeUnit formats a single time unit into a string based on its value and context.
//
// It applies singular or plural grammar, skipping leading zeros unless forced (e.g., for seconds as the last unit),
// returning an empty string for skippable zeros to maintain a concise output.
//
// Parameters:
//   - unit: The timeUnit struct containing the value and labels (singular/plural) to format.
//   - forceInclude: A boolean indicating whether to include the unit even if zero (e.g., for seconds as fallback).
//
// Returns:
//   - string: The formatted unit (e.g., "1 hour", "2 minutes") or empty string if skipped.
func formatTimeUnit(unit struct {
	value    int64
	singular string
	plural   string
}, forceInclude bool,
) string {
	switch {
	case unit.value == 1:
		return "1 " + unit.singular
	case unit.value > 1 || forceInclude:
		return fmt.Sprintf("%d %s", unit.value, unit.plural)
	default:
		return "" // Skip zero values unless forced.
	}
}

// filterEmpty removes empty strings from a slice, returning only non-empty elements.
//
// It ensures the final formatted duration string excludes unnecessary parts, maintaining readability
// by filtering out zero-value units that were not explicitly included.
//
// Parameters:
//   - parts: A slice of strings representing formatted time units (e.g., "1 hour", "").
//
// Returns:
//   - []string: A new slice containing only the non-empty strings from the input.
func filterEmpty(parts []string) []string {
	var filtered []string

	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}

	return filtered
}

// writeStartupMessage logs or notifies startup information based on configuration flags.
//
// It reports Watchtower’s version, notification setup, container filtering details, scheduling information,
// and HTTP API status, providing users with a comprehensive overview of the application’s initial state.
//
// Parameters:
//   - c: The cobra.Command instance, providing access to flags like --no-startup-message.
//   - sched: The time.Time of the first scheduled run, or zero if no schedule is set.
//   - filtering: A string describing the container filter applied (e.g., "Watching all containers").
func writeStartupMessage(c *cobra.Command, sched time.Time, filtering string) {
	// Retrieve flags controlling startup message behavior and API setup.
	noStartupMessage, _ := c.PersistentFlags().GetBool("no-startup-message")
	enableUpdateAPI, _ := c.PersistentFlags().GetBool("http-api-update")

	apiPort, _ := c.PersistentFlags().GetString("http-api-port")
	if apiPort == "" {
		apiPort = "8080" // Default port if not specified via --http-api-port.
	}

	// Configure the logger based on whether startup messages should be suppressed.
	startupLog := setupStartupLogger(noStartupMessage)
	startupLog.Info("Watchtower ", meta.Version, " using Docker API v", client.GetVersion())

	// Log details about configured notifiers or lack thereof.
	logNotifierInfo(startupLog, notifier.GetNames())
	startupLog.Info(filtering)

	// Log scheduling or run mode information based on configuration.
	logScheduleInfo(startupLog, c, sched)

	// Report HTTP API status if enabled.
	if enableUpdateAPI {
		startupLog.Info(fmt.Sprintf("The HTTP API is enabled at :%s.", apiPort))
	}

	// Send batched notifications if not suppressed, ensuring startup info reaches users.
	if !noStartupMessage {
		notifier.SendNotification(nil)
	}

	// Warn about trace-level logging if enabled, as it may expose sensitive data.
	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		startupLog.Warn(
			"Trace level enabled: log will include sensitive information as credentials and tokens",
		)
	}
}

// setupStartupLogger configures the logger for startup messages based on message suppression settings.
//
// It uses a local log entry if messages are suppressed (--no-startup-message), otherwise batches messages
// via the notifier for consolidated delivery, ensuring flexibility in how startup info is presented.
//
// Parameters:
//   - noStartupMessage: A boolean indicating whether startup messages should be logged locally only.
//
// Returns:
//   - *logrus.Entry: A configured log entry for writing startup messages.
func setupStartupLogger(noStartupMessage bool) *logrus.Entry {
	if noStartupMessage {
		return notifications.LocalLog
	}

	log := logrus.NewEntry(logrus.StandardLogger())

	notifier.StartNotification()

	return log
}

// logNotifierInfo logs details about the notification setup for Watchtower.
//
// It reports the list of configured notifier names (e.g., "email, slack") or indicates no notifications
// are set up, providing visibility into how update statuses will be communicated.
//
// Parameters:
//   - log: The logrus.Entry used to write the notification information.
//   - notifierNames: A slice of strings representing the names of configured notifiers.
func logNotifierInfo(log *logrus.Entry, notifierNames []string) {
	if len(notifierNames) > 0 {
		log.Info("Using notifications: " + strings.Join(notifierNames, ", "))
	} else {
		log.Info("Using no notifications")
	}
}

// logScheduleInfo logs information about the scheduling or run mode configuration.
//
// It handles scheduled runs with timing details, one-time updates, or indicates no periodic runs,
// ensuring users understand when and how updates will occur.
//
// Parameters:
//   - log: The logrus.Entry used to write the schedule information.
//   - c: The cobra.Command instance, providing access to flags like --run-once.
//   - sched: The time.Time of the first scheduled run, or zero if no schedule is set.
func logScheduleInfo(log *logrus.Entry, c *cobra.Command, sched time.Time) {
	if !sched.IsZero() {
		until := formatDuration(time.Until(sched))
		log.Info("Scheduling first run: " + sched.Format("2006-01-02 15:04:05 -0700 MST"))
		log.Info("Note that the first check will be performed in " + until)

		return
	}

	if runOnce, _ := c.PersistentFlags().GetBool("run-once"); runOnce {
		log.Info("Running a one time update.")
	} else {
		log.Info("Periodic runs are not enabled.")
	}
}

// runUpgradesOnSchedule schedules and executes periodic container updates according to the cron specification.
//
// It sets up a cron scheduler, runs updates at specified intervals, and ensures graceful shutdown on interrupt
// signals (SIGINT, SIGTERM) or context cancellation, handling concurrency with a lock channel.
//
// Parameters:
//   - ctx: The context controlling the scheduler’s lifecycle, enabling shutdown on cancellation.
//   - c: The cobra.Command instance, providing access to flags for startup messaging.
//   - filter: The types.Filter determining which containers are updated.
//   - filtering: A string describing the filter, used in startup messaging.
//   - lock: A channel ensuring only one update runs at a time, or nil to create a new one.
//
// Returns:
//   - error: An error if scheduling fails (e.g., invalid cron spec), nil on successful shutdown.
func runUpgradesOnSchedule(
	ctx context.Context,
	c *cobra.Command,
	filter types.Filter,
	filtering string,
	lock chan bool,
) error {
	// Initialize lock if not provided, ensuring single-update concurrency.
	if lock == nil {
		lock = make(chan bool, 1)
		lock <- true
	}

	// Create a new cron scheduler for managing periodic updates.
	scheduler := cron.New()

	// Add the update function to the cron schedule, handling concurrency and metrics.
	if err := scheduler.AddFunc(
		scheduleSpec,
		func() {
			select {
			case v := <-lock:
				defer func() { lock <- v }()
				metric := runUpdatesWithNotifications(filter)
				metrics.Default().RegisterScan(metric)
			default:
				// Skip if another update is running, logging and registering a nil metric.
				metrics.Default().RegisterScan(nil)
				logrus.Debug("Skipped another update already running.")
			}

			// Log the next scheduled run for visibility if entries exist.
			nextRuns := scheduler.Entries()
			if len(nextRuns) > 0 {
				logrus.Debug("Scheduled next run: " + nextRuns[0].Next.String())
			}
		}); err != nil {
		return fmt.Errorf("failed to schedule updates: %w", err)
	}

	// Log startup message with the first scheduled run time.
	writeStartupMessage(c, scheduler.Entries()[0].Schedule.Next(time.Now()), filtering)

	// Start the scheduler to begin periodic execution.
	scheduler.Start()

	// Set up signal handling for graceful shutdown on interrupt or termination signals.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	// Wait for shutdown signal or context cancellation, stopping the scheduler cleanly.
	select {
	case <-ctx.Done():
		logrus.Info("Context canceled, stopping scheduler...")
	case <-interrupt:
		logrus.Info("Received interrupt signal, stopping scheduler...")
	}

	scheduler.Stop()
	logrus.Info("Waiting for running update to be finished...")
	<-lock
	logrus.Info("Scheduler stopped and update completed.")

	return nil
}

// runUpdatesWithNotifications performs container updates and sends notifications about the results.
//
// It executes the update action with configured parameters, batches notifications, and returns a metric
// summarizing the session for monitoring purposes, ensuring users are informed of update outcomes.
//
// Parameters:
//   - filter: The types.Filter determining which containers are targeted for updates.
//
// Returns:
//   - *metrics.Metric: A pointer to a metric object summarizing the update session (scanned, updated, failed counts).
func runUpdatesWithNotifications(filter types.Filter) *metrics.Metric {
	// Start batching notifications to group update messages into a single alert.
	notifier.StartNotification()

	// Configure update parameters based on global flags and settings.
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
	}

	// Execute the update action, capturing results and handling any errors.
	result, err := actions.Update(client, updateParams)
	if err != nil {
		logrus.WithError(err).Error("Update execution failed")
	}

	// Debug report
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

	// Send the batched notification with update results (successes, failures, etc.).
	notifier.SendNotification(result)

	// Generate and log a metric summarizing the update session.
	metricResults := metrics.NewMetric(result)
	notifications.LocalLog.WithFields(logrus.Fields{
		"scanned": metricResults.Scanned,
		"updated": metricResults.Updated,
		"failed":  metricResults.Failed,
	}).Info("Update session completed")

	return metricResults
}
