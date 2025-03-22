// Package cmd contains the watchtower (sub-)commands.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/robfig/cron"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/internal/meta"
	"github.com/nicholas-fedor/watchtower/pkg/api"
	metricsAPI "github.com/nicholas-fedor/watchtower/pkg/api/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/api/update"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/notifications"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// client is the Docker client used for interacting with container operations.
// It is initialized in PreRun based on command-line flags and environment settings.
var client container.Client

// scheduleSpec holds the cron schedule string for periodic updates.
// It is set via the --schedule flag or environment variable in PreRun.
var scheduleSpec string

// cleanup determines whether to remove old images after updates.
// It is enabled via the --cleanup flag or WATCHTOWER_CLEANUP environment variable in PreRun.
var cleanup bool

// noRestart prevents restarting containers after updates.
// It is set via the --no-restart flag or WATCHTOWER_NO_RESTART environment variable in PreRun.
var noRestart bool

// noPull skips pulling new images during updates.
// It is enabled via the --no-pull flag or WATCHTOWER_NO_PULL environment variable in PreRun.
var noPull bool

// monitorOnly enables monitoring mode without performing updates.
// It is set via the --monitor-only flag or WATCHTOWER_MONITOR_ONLY environment variable in PreRun.
var monitorOnly bool

// enableLabel restricts updates to containers with the watchtower.enable label.
// It is enabled via the --label-enable flag or WATCHTOWER_LABEL_ENABLE environment variable in PreRun.
var enableLabel bool

// disableContainers lists container names to exclude from updates.
// It is set via the --disable-containers flag or WATCHTOWER_DISABLE_CONTAINERS environment variable in PreRun.
var disableContainers []string

// notifier manages sending notifications about update status.
// It is initialized in PreRun with configured notification types.
var notifier types.Notifier

// timeout specifies the duration allowed for container stop operations.
// It defaults to a value set in flags and is overridden via --timeout or WATCHTOWER_TIMEOUT in PreRun.
var timeout time.Duration

// lifecycleHooks enables execution of pre- and post-update lifecycle hooks.
// It is set via the --enable-lifecycle-hooks flag or WATCHTOWER_LIFECYCLE_HOOKS environment variable in PreRun.
var lifecycleHooks bool

// rollingRestart enables rolling restarts instead of stopping all containers at once.
// It is set via the --rolling-restart flag or WATCHTOWER_ROLLING_RESTART environment variable in PreRun.
var rollingRestart bool

// scope defines a specific scope for Watchtower operations, limiting affected containers.
// It is set via the --scope flag or WATCHTOWER_SCOPE environment variable in PreRun.
var scope string

// labelPrecedence gives container label settings precedence over global flags.
// It is enabled via the --label-take-precedence flag or WATCHTOWER_LABEL_PRECEDENCE environment variable in PreRun.
var labelPrecedence bool

// rootCmd represents the root command for the Watchtower CLI.
// It serves as the parent for all subcommands and is initialized with default behavior.
var rootCmd = NewRootCommand()

// NewRootCommand creates and configures the root command for Watchtower.
// It defines the base usage, description, and lifecycle hooks for execution.
func NewRootCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "watchtower",
		Short:  "Automatically updates running Docker containers",
		Long:   "\nWatchtower automatically updates running Docker containers whenever a new image is released.\nMore information available at https://github.com/nicholas-fedor/watchtower/.",
		Run:    run,
		PreRun: preRun,
		Args:   cobra.ArbitraryArgs, // Allows any number of positional arguments, filtered later.
	}
}

// init registers flags for the root command.
// It sets up Docker, system, and notification flags, establishing the CLI’s configuration options.
func init() {
	flags.SetDefaults()
	flags.RegisterDockerFlags(rootCmd)
	flags.RegisterSystemFlags(rootCmd)
	flags.RegisterNotificationFlags(rootCmd)
}

// Execute runs the root command and handles any execution errors.
// It is called from main.go as the entry point for the Watchtower CLI.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		logrus.Fatal(err)
	}
}

// preRun prepares the environment before the main command execution.
// It configures logging, processes flags, initializes the Docker client and notifier,
// and performs validation on flag combinations.
func preRun(cmd *cobra.Command, _ []string) {
	flagsSet := cmd.PersistentFlags()
	flags.ProcessFlagAliases(flagsSet)

	// Set up logging based on flag settings (e.g., verbosity level).
	if err := flags.SetupLogging(flagsSet); err != nil {
		logrus.Fatalf("Failed to initialize logging: %s", err.Error())
	}

	// Retrieve and assign the schedule specification from flags.
	scheduleSpec, _ = flagsSet.GetString("schedule")

	// Process secrets and read core operational flags.
	flags.GetSecretsFromFiles(cmd)
	cleanup, noRestart, monitorOnly, timeout = flags.ReadFlags(cmd)

	// Validate timeout to ensure it’s non-negative.
	if timeout < 0 {
		logrus.Fatal("Please specify a positive value for timeout value.")
	}

	// Retrieve and assign additional configuration flags.
	enableLabel, _ = flagsSet.GetBool("label-enable")
	disableContainers, _ = flagsSet.GetStringSlice("disable-containers")
	lifecycleHooks, _ = flagsSet.GetBool("enable-lifecycle-hooks")
	rollingRestart, _ = flagsSet.GetBool("rolling-restart")
	scope, _ = flagsSet.GetString("scope")
	labelPrecedence, _ = flagsSet.GetBool("label-take-precedence")

	if scope != "" {
		logrus.Debugf(`Using scope %q`, scope)
	}

	// Configure environment variables for the Docker client.
	err := flags.EnvConfig(cmd)
	if err != nil {
		logrus.Fatal(err)
	}

	// Retrieve and assign flags controlling container behavior and image handling.
	noPull, _ = flagsSet.GetBool("no-pull")
	includeStopped, _ := flagsSet.GetBool("include-stopped")
	includeRestarting, _ := flagsSet.GetBool("include-restarting")
	reviveStopped, _ := flagsSet.GetBool("revive-stopped")
	removeVolumes, _ := flagsSet.GetBool("remove-volumes")
	warnOnHeadPullFailed, _ := flagsSet.GetString("warn-on-head-failure")

	// Warn about potentially conflicting flag combinations.
	if monitorOnly && noPull {
		logrus.Warn("Using `WATCHTOWER_NO_PULL` and `WATCHTOWER_MONITOR_ONLY` simultaneously might lead to no action being taken at all. If this is intentional, you may safely ignore this message.")
	}

	// Initialize and assign the Docker client with specified options.
	client = container.NewClient(container.ClientOptions{
		IncludeStopped:    includeStopped,
		ReviveStopped:     reviveStopped,
		RemoveVolumes:     removeVolumes,
		IncludeRestarting: includeRestarting,
		WarnOnHeadFailed:  container.WarningStrategy(warnOnHeadPullFailed),
	})

	// Initialize and assign the notifier for sending update status notifications.
	notifier = notifications.NewNotifier(cmd)
	notifier.AddLogHook()
}

// run executes the main Watchtower logic based on command-line flags.
// It handles one-time updates, HTTP API setup, and scheduled updates.
func run(c *cobra.Command, names []string) {
	// Build the container filter based on provided names and flags.
	filter, filterDesc := filters.BuildFilter(names, disableContainers, enableLabel, scope)

	// Retrieve operational mode flags.
	runOnce, _ := c.PersistentFlags().GetBool("run-once")
	enableUpdateAPI, _ := c.PersistentFlags().GetBool("http-api-update")
	enableMetricsAPI, _ := c.PersistentFlags().GetBool("http-api-metrics")
	unblockHTTPAPI, _ := c.PersistentFlags().GetBool("http-api-periodic-polls")
	apiToken, _ := c.PersistentFlags().GetString("http-api-token")
	healthCheck, _ := c.PersistentFlags().GetBool("health-check")

	// Retrieve the HTTP API port from flags.
	flagsSet := c.PersistentFlags()

	apiPort, err := flagsSet.GetString("http-api-port")
	if err != nil {
		logrus.Fatalf("Failed to get http-api-port flag: %v", err)
	}

	if apiPort == "" {
		apiPort = "8080" // Fallback to default if unset.
	}

	// Handle health check mode early and return if applicable.
	if healthCheck {
		if os.Getpid() == 1 {
			time.Sleep(1 * time.Second)
			logrus.Fatal("The health check flag should never be passed to the main watchtower container process")
		}

		return // Exit early without os.Exit to allow defer in caller if needed.
	}

	// Run the main logic and handle exit status.
	if exitCode := runMain(c, names, filter, filterDesc, runOnce, enableUpdateAPI, enableMetricsAPI, unblockHTTPAPI, apiToken, apiPort); exitCode != 0 {
		os.Exit(exitCode)
	}
}

// runMain contains the core Watchtower logic after early exits are handled.
// It sets up the HTTP API and scheduled updates with proper context management,
// returning an exit code (0 for success, 1 for failure).
func runMain(c *cobra.Command, names []string, filter types.Filter, filterDesc string, runOnce, enableUpdateAPI, enableMetricsAPI, unblockHTTPAPI bool, apiToken, apiPort string) int {
	// Log the container names for debugging, ensuring 'names' is used.
	logrus.Debugf("Processing containers: %v", names)

	// Validate flag compatibility.
	if rollingRestart && monitorOnly {
		logrus.Fatal("Rolling restarts is not compatible with the global monitor only flag")
	}

	awaitDockerClient()

	// Perform sanity checks on the container environment.
	if err := actions.CheckForSanity(client, filter, rollingRestart); err != nil {
		logNotify(err)

		return 1
	}

	// Execute a one-time update and exit if specified.
	if runOnce {
		writeStartupMessage(c, time.Time{}, filterDesc)
		runUpdatesWithNotifications(filter)
		notifier.Close()

		return 0
	}

	// Check for and clean up multiple Watchtower instances.
	if err := actions.CheckForMultipleWatchtowerInstances(client, cleanup, scope); err != nil {
		logNotify(err)

		return 1
	}

	// updateLock ensures only one update runs at a time, shared between scheduler and API.
	updateLock := make(chan bool, 1)
	updateLock <- true

	// Initialize the HTTP API with the configured port.
	httpAPI := api.New(apiToken)
	httpAPI.Addr = ":" + apiPort

	// Set up the update API endpoint if enabled.
	if enableUpdateAPI {
		updateHandler := update.New(func(images []string) {
			metric := runUpdatesWithNotifications(filters.FilterByImage(images, filter))
			metrics.RegisterScan(metric)
		}, updateLock)
		httpAPI.RegisterFunc(updateHandler.Path, updateHandler.Handle)

		if !unblockHTTPAPI {
			writeStartupMessage(c, time.Time{}, filterDesc)
		}
	}

	// Set up the metrics API endpoint if enabled.
	if enableMetricsAPI {
		metricsHandler := metricsAPI.New()
		httpAPI.RegisterHandler(metricsHandler.Path, metricsHandler.Handle)
	}

	// Create context for HTTP API and scheduling, ensuring cleanup on exit.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the HTTP API, logging errors unless it’s a clean shutdown.
	if err := httpAPI.Start(ctx, enableUpdateAPI && !unblockHTTPAPI); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logrus.Error("failed to start API", err)
	}

	// Run updates on the specified schedule, handling any errors.
	if err := runUpgradesOnSchedule(ctx, c, filter, filterDesc, updateLock); err != nil {
		logrus.Error(err)

		return 1
	}

	return 1 // Default failure exit if execution reaches here unexpectedly.
}

// logNotify logs an error and closes the notifier without exiting.
// It ensures notifications are sent before returning control.
func logNotify(err error) {
	logrus.Error(err)
	notifier.Close()
}

// awaitDockerClient pauses briefly to ensure the Docker client is ready.
// This avoids race conditions during initialization.
func awaitDockerClient() {
	logrus.Debug("Sleeping for a second to ensure the docker api client has been properly initialized.")
	time.Sleep(1 * time.Second)
}

// formatDuration converts a time.Duration to a human-readable string.
// It handles hours, minutes, and seconds, ensuring proper grammar and formatting.
func formatDuration(duration time.Duration) string {
	durationBuilder := strings.Builder{}

	const (
		minutesPerHour = 60
		secondsPerHour = 60 * minutesPerHour
	)

	hours := int64(duration.Hours())
	minutes := int64(math.Mod(duration.Minutes(), minutesPerHour))
	seconds := int64(math.Mod(duration.Seconds(), secondsPerHour))

	if hours == 1 {
		durationBuilder.WriteString("1 hour")
	} else if hours != 0 {
		durationBuilder.WriteString(strconv.FormatInt(hours, 10))
		durationBuilder.WriteString(" hours")
	}

	if hours != 0 && (seconds != 0 || minutes != 0) {
		durationBuilder.WriteString(", ")
	}

	if minutes == 1 {
		durationBuilder.WriteString("1 minute")
	} else if minutes != 0 {
		durationBuilder.WriteString(strconv.FormatInt(minutes, 10))
		durationBuilder.WriteString(" minutes")
	}

	if minutes != 0 && seconds != 0 {
		durationBuilder.WriteString(", ")
	}

	if seconds == 1 {
		durationBuilder.WriteString("1 second")
	} else if seconds != 0 || (hours == 0 && minutes == 0) {
		durationBuilder.WriteString(strconv.FormatInt(seconds, 10))
		durationBuilder.WriteString(" seconds")
	}

	return durationBuilder.String()
}

// writeStartupMessage logs or notifies startup information based on configuration.
// It includes version, notification setup, filtering, scheduling details, and HTTP API status.
func writeStartupMessage(c *cobra.Command, sched time.Time, filtering string) {
	noStartupMessage, _ := c.PersistentFlags().GetBool("no-startup-message")
	enableUpdateAPI, _ := c.PersistentFlags().GetBool("http-api-update")
	// Retrieve the HTTP API port for the startup message.
	flagsSet := c.PersistentFlags()

	apiPort, err := flagsSet.GetString("http-api-port")
	if err != nil {
		logrus.Fatalf("Failed to get http-api-port flag: %v", err)
	}

	if apiPort == "" {
		apiPort = "8080" // Fallback to default if unset.
	}

	var startupLog *logrus.Entry
	if noStartupMessage {
		startupLog = notifications.LocalLog
	} else {
		startupLog = logrus.NewEntry(logrus.StandardLogger())
		// Batch startup messages for a single notification.
		notifier.StartNotification()
	}

	startupLog.Info("Watchtower ", meta.Version)

	notifierNames := notifier.GetNames()
	if len(notifierNames) > 0 {
		startupLog.Info("Using notifications: " + strings.Join(notifierNames, ", "))
	} else {
		startupLog.Info("Using no notifications")
	}

	startupLog.Info(filtering)

	if !sched.IsZero() {
		until := formatDuration(time.Until(sched))
		startupLog.Info("Scheduling first run: " + sched.Format("2006-01-02 15:04:05 -0700 MST"))
		startupLog.Info("Note that the first check will be performed in " + until)
	} else if runOnce, _ := c.PersistentFlags().GetBool("run-once"); runOnce {
		startupLog.Info("Running a one time update.")
	} else {
		startupLog.Info("Periodic runs are not enabled.")
	}

	if enableUpdateAPI {
		startupLog.Info(fmt.Sprintf("The HTTP API is enabled at :%s.", apiPort))
	}

	if !noStartupMessage {
		// Send batched startup messages, excluding trace warning.
		notifier.SendNotification(nil)
	}

	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		startupLog.Warn("Trace level enabled: log will include sensitive information as credentials and tokens")
	}
}

// runUpgradesOnSchedule schedules and executes periodic updates.
// It uses a cron scheduler and ensures graceful shutdown on interrupt signals.
func runUpgradesOnSchedule(ctx context.Context, c *cobra.Command, filter types.Filter, filtering string, lock chan bool) error {
	if lock == nil {
		lock = make(chan bool, 1)
		lock <- true
	}

	scheduler := cron.New()

	// Add the update function to the cron schedule, wrapping errors for proper handling.
	if err := scheduler.AddFunc(
		scheduleSpec,
		func() {
			select {
			case v := <-lock:
				defer func() { lock <- v }()

				metric := runUpdatesWithNotifications(filter)
				metrics.RegisterScan(metric)
			default:
				// Skip if another update is running.
				metrics.RegisterScan(nil)
				logrus.Debug("Skipped another update already running.")
			}

			nextRuns := scheduler.Entries()
			if len(nextRuns) > 0 {
				logrus.Debug("Scheduled next run: " + nextRuns[0].Next.String())
			}
		}); err != nil {
		return fmt.Errorf("failed to schedule updates: %w", err)
	}

	// Log startup message with the first scheduled run.
	writeStartupMessage(c, scheduler.Entries()[0].Schedule.Next(time.Now()), filtering)

	scheduler.Start()

	// Handle graceful shutdown on context cancellation or SIGINT/SIGTERM.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

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

// runUpdatesWithNotifications performs container updates and sends notifications.
// It returns a metric summarizing the update session for monitoring.
func runUpdatesWithNotifications(filter types.Filter) *metrics.Metric {
	// Start batching notifications for the update session.
	notifier.StartNotification()

	// Configure update parameters from global flags.
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

	// Execute the update and handle any errors.
	result, err := actions.Update(client, updateParams)
	if err != nil {
		logrus.Error(err)
	}

	// Send the update results as a notification.
	notifier.SendNotification(result)
	metricResults := metrics.NewMetric(result)
	notifications.LocalLog.WithFields(logrus.Fields{
		"Scanned": metricResults.Scanned,
		"Updated": metricResults.Updated,
		"Failed":  metricResults.Failed,
	}).Info("Session done")

	return metricResults
}
