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
	"net"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	dockerContainer "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	"github.com/nicholas-fedor/watchtower/internal/api"
	"github.com/nicholas-fedor/watchtower/internal/config"
	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/internal/logging"
	"github.com/nicholas-fedor/watchtower/internal/meta"
	"github.com/nicholas-fedor/watchtower/internal/scheduling"
	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/notifications"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var (
	ErrContainerIDNotFound = errors.New(
		"container ID not found in /proc/self/cgroup and HOSTNAME is not set",
	)

	// client is the Docker client instance used to interact with container operations in Watchtower.
	//
	// It provides an interface for listing, stopping, starting, and managing containers, initialized during
	// the preRun phase with options derived from command-line flags and environment variables such as
	// DOCKER_HOST, DOCKER_TLS_VERIFY, and DOCKER_API_VERSION.
	client container.Client

	// scheduleSpec holds the cron-formatted schedule string that dictates when periodic container updates occur.
	//
	// It is populated during preRun from the --schedule flag or the WATCHTOWER_SCHEDULE environment variable,
	// supporting formats like "@every 1h" or standard cron syntax (e.g., "0 0 * * * *") for flexible scheduling.
	scheduleSpec string

	// cleanup is a boolean flag determining whether to remove old images after a container update.
	//
	// It is set during preRun via the --cleanup flag or the WATCHTOWER_CLEANUP environment variable,
	// enabling disk space management by deleting outdated images post-update.
	cleanup bool

	// noRestart is a boolean flag that prevents containers from being restarted after an update.
	//
	// It is configured in preRun via the --no-restart flag or the WATCHTOWER_NO_RESTART environment variable,
	// useful when users prefer manual restart control or want to minimize downtime during updates.
	noRestart bool

	// noPull is a boolean flag that skips pulling new images from the registry during updates.
	//
	// It is enabled in preRun via the --no-pull flag or the WATCHTOWER_NO_PULL environment variable,
	// allowing updates to proceed using only locally cached images, potentially reducing network usage.
	noPull bool

	// monitorOnly is a boolean flag enabling a mode where Watchtower monitors containers without updating them.
	//
	// It is set in preRun via the --monitor-only flag or the WATCHTOWER_MONITOR_ONLY environment variable,
	// ideal for observing image staleness without triggering automatic updates.
	monitorOnly bool

	// enableLabel is a boolean flag restricting updates to containers with the "com.centurylinklabs.watchtower.enable" label set to true.
	//
	// It is configured in preRun via the --label-enable flag or the WATCHTOWER_LABEL_ENABLE environment variable,
	// providing granular control over which containers are targeted for updates.
	enableLabel bool

	// disableContainers is a slice of container names explicitly excluded from updates.
	//
	// It is populated in preRun from the --disable-containers flag or the WATCHTOWER_DISABLE_CONTAINERS environment variable,
	// allowing users to blacklist specific containers from Watchtower's operations.
	disableContainers []string

	// notifier is the notification system instance responsible for sending update status messages to configured channels.
	//
	// It is initialized in preRun with notification types specified via flags (e.g., --notifications), supporting
	// multiple methods like email, Slack, or MSTeams to inform users about update successes, failures, or skips.
	notifier types.Notifier

	// timeout specifies the maximum duration allowed for container stop operations during updates.
	//
	// It defaults to a value defined in the flags package and can be overridden in preRun via the --timeout flag or
	// WATCHTOWER_TIMEOUT environment variable, ensuring containers are stopped gracefully within a specified time limit.
	timeout time.Duration

	// lifecycleHooks is a boolean flag enabling the execution of pre- and post-update lifecycle hook commands.
	//
	// It is set in preRun via the --enable-lifecycle-hooks flag or the WATCHTOWER_LIFECYCLE_HOOKS environment variable,
	// allowing custom scripts to run at specific update stages for additional validation or actions.
	lifecycleHooks bool

	// rollingRestart is a boolean flag enabling rolling restarts, updating containers sequentially rather than all at once.
	//
	// It is configured in preRun via the --rolling-restart flag or the WATCHTOWER_ROLLING_RESTART environment variable,
	// reducing downtime by restarting containers one-by-one during updates.
	rollingRestart bool

	// scope defines a specific operational scope for Watchtower, limiting updates to containers matching this scope.
	//
	// It is set in preRun via the --scope flag or the WATCHTOWER_SCOPE environment variable, useful for isolating
	// Watchtower's actions to a subset of containers (e.g., a project or environment).
	scope string

	// labelPrecedence is a boolean flag giving container label settings priority over global command-line flags.
	//
	// It is enabled in preRun via the --label-take-precedence flag or the WATCHTOWER_LABEL_PRECEDENCE environment variable,
	// allowing container-specific configurations to override broader settings for flexibility.
	labelPrecedence bool

	// lifecycleUID is the default UID to run lifecycle hooks as.
	//
	// It is set in preRun via the --lifecycle-uid flag or the WATCHTOWER_LIFECYCLE_UID environment variable,
	// providing a global default that can be overridden by container labels.
	lifecycleUID int

	// lifecycleGID is the default GID to run lifecycle hooks as.
	//
	// It is set in preRun via the --lifecycle-gid flag or the WATCHTOWER_LIFECYCLE_GID environment variable,
	// providing a global default that can be overridden by container labels.
	lifecycleGID int

	// notificationSplitByContainer is a boolean flag enabling separate notifications for each updated container.
	//
	// It is set in preRun via the --notification-split-by-container flag or the WATCHTOWER_NOTIFICATION_SPLIT_BY_CONTAINER environment variable,
	// allowing users to receive individual notifications instead of grouped ones.
	notificationSplitByContainer bool

	// notificationReport is a boolean flag enabling report-based notifications.
	//
	// It is set in preRun via the --notification-report flag or the WATCHTOWER_NOTIFICATION_REPORT environment variable,
	// controlling whether notifications include session reports or just log entries.
	notificationReport bool

	// cpuCopyMode specifies how CPU settings are handled when recreating containers.
	//
	// It is set during preRun via the --cpu-copy-mode flag or the WATCHTOWER_CPU_COPY_MODE environment variable,
	// controlling CPU limit copying behavior for compatibility with different container runtimes like Podman.
	cpuCopyMode string

	// rootCmd represents the root command for the Watchtower CLI, serving as the entry point for all subcommands.
	//
	// It defines the base usage string, short and long descriptions, and assigns lifecycle hooks (PreRun and Run)
	// to manage setup and execution, initialized with default behavior and configured via flags during runtime.
	rootCmd = NewRootCommand()

	// runUpdatesWithNotifications performs container updates and sends notifications about the results.
	//
	// It executes the update action with configured parameters, batches notifications, and returns a metric
	// summarizing the session for monitoring purposes, ensuring users are informed of update outcomes.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts.
	//   - filter: The types.Filter determining which containers are targeted for updates.
	//   - params: The types.UpdateParams struct containing update configuration parameters.
	//
	// Returns:
	//   - *metrics.Metric: A pointer to a metric object summarizing the update session (scanned, updated, failed counts).
	runUpdatesWithNotifications = func(ctx context.Context, filter types.Filter, params types.UpdateParams) *metrics.Metric {
		actionParams := actions.RunUpdatesWithNotificationsParams{
			Client:                       client,
			Notifier:                     notifier,
			NotificationSplitByContainer: notificationSplitByContainer,
			NotificationReport:           notificationReport,
			Filter:                       filter,
			Cleanup:                      params.Cleanup,
			NoRestart:                    noRestart,
			MonitorOnly:                  params.MonitorOnly,
			LifecycleHooks:               lifecycleHooks,
			RollingRestart:               rollingRestart,
			LabelPrecedence:              labelPrecedence,
			NoPull:                       noPull,
			Timeout:                      timeout,
			LifecycleUID:                 lifecycleUID,
			LifecycleGID:                 lifecycleGID,
			CPUCopyMode:                  cpuCopyMode,
			PullFailureDelay:             time.Duration(0),
			RunOnce:                      params.RunOnce,
			SkipSelfUpdate:               params.SkipSelfUpdate,
		}

		return actions.RunUpdatesWithNotifications(ctx, actionParams)
	}
)

var sleepFunc = time.Sleep

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
	logrus.WithField("scheduleSpec", scheduleSpec).
		Debug("Retrieved cron schedule specification from flags")

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
	for i := range disableContainers {
		disableContainers[i] = util.NormalizeContainerName(disableContainers[i])
	}

	lifecycleHooks, _ = flagsSet.GetBool("enable-lifecycle-hooks")
	rollingRestart, _ = flagsSet.GetBool("rolling-restart")
	scope, _ = flagsSet.GetString("scope")
	labelPrecedence, _ = flagsSet.GetBool("label-take-precedence")

	// Retrieve lifecycle UID and GID flags.
	lifecycleUID, _ = flagsSet.GetInt("lifecycle-uid")
	lifecycleGID, _ = flagsSet.GetInt("lifecycle-gid")

	// Retrieve notification split flag.
	notificationSplitByContainer, _ = flagsSet.GetBool("notification-split-by-container")

	// Retrieve notification report flag.
	notificationReport, _ = flagsSet.GetBool("notification-report")

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
	disableMemorySwappiness, _ := flagsSet.GetBool("disable-memory-swappiness")
	cpuCopyMode, _ = flagsSet.GetString("cpu-copy-mode")

	// Warn about potential redundancy in flag combinations that could result in no action.
	if monitorOnly && noPull {
		logrus.WithFields(logrus.Fields{
			"monitor_only": monitorOnly,
			"no_pull":      noPull,
		}).Warn("Combining monitor-only and no-pull might result in no updates")
	}

	// Initialize the Docker client with options reflecting the desired container handling behavior.
	client = container.NewClient(container.ClientOptions{
		IncludeStopped:          includeStopped,
		ReviveStopped:           reviveStopped,
		RemoveVolumes:           removeVolumes,
		IncludeRestarting:       includeRestarting,
		DisableMemorySwappiness: disableMemorySwappiness,
		CPUCopyMode:             cpuCopyMode,
		WarnOnHeadFailed:        container.WarningStrategy(warnOnHeadPullFailed),
	})

	// Set up the notification system with types specified via flags (e.g., email, Slack).
	notifier = notifications.NewNotifier(cmd)
	notifier.AddLogHook()
}

// run executes the main Watchtower logic based on parsed command-line flags.
//
// It determines the operational mode (one-time update, HTTP API, or scheduled updates),
// builds the container filter, and delegates to runMain for core execution,
// exiting with a status code based on the outcome (0 for success, non-zero for failure).
//
// This function bridges flag parsing and the application’s primary workflow.
//
// Parameters:
//   - c: The cobra.Command instance being executed, providing access to parsed flags.
//   - names: A slice of container names provided as positional arguments, used for filtering.
func run(c *cobra.Command, normalizedNames []string) {
	for i := range normalizedNames {
		normalizedNames[i] = util.NormalizeContainerName(normalizedNames[i])
	}

	logrus.WithField("positional_args", normalizedNames).
		Debug("Received positional arguments for container filtering")
	// Attempt to derive the operational scope from the current container's scope label
	// if not explicitly set. This ensures scope persistence during self-updates.
	if err := deriveScopeFromContainer(client); err != nil {
		logrus.WithError(err).Debug("Scope derivation failed, continuing with current scope")
	}

	// Build the filter and its description based on normalized names, exclusions, and label settings.
	filter, filterDesc := filters.BuildFilter(
		normalizedNames,
		disableContainers, // Normalized container names
		enableLabel,
		scope,
	)

	// Get flags controlling execution mode and HTTP API behavior.
	runOnce, _ := c.PersistentFlags().GetBool("run-once")
	updateOnStart, _ := c.PersistentFlags().GetBool("update-on-start")
	enableUpdateAPI, _ := c.PersistentFlags().GetBool("http-api-update")
	enableMetricsAPI, _ := c.PersistentFlags().GetBool("http-api-metrics")
	unblockHTTPAPI, _ := c.PersistentFlags().GetBool("http-api-periodic-polls")
	noStartupMessage, _ := c.PersistentFlags().GetBool("no-startup-message")
	apiToken, _ := c.PersistentFlags().GetString("http-api-token")
	healthCheck, _ := c.PersistentFlags().GetBool("health-check")

	// Get the HTTP API host and port, falling back to "8080" for port if not specified.
	flagsSet := c.PersistentFlags()

	apiHost, err := flagsSet.GetString("http-api-host")
	if err != nil {
		logrus.WithError(err).Fatal("Failed to get http-api-host flag")
	}

	// Validate APIHost: allow empty or valid IP
	if apiHost != "" && net.ParseIP(apiHost) == nil {
		logrus.Fatalf(
			"invalid http-api-host '%s': must be empty or a valid IP address (IPv4 or IPv6)",
			apiHost,
		)
	}

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
	cfg := config.RunConfig{
		Command:          c,
		Names:            normalizedNames,
		Filter:           filter,
		FilterDesc:       filterDesc,
		RunOnce:          runOnce,
		UpdateOnStart:    updateOnStart,
		EnableUpdateAPI:  enableUpdateAPI,
		EnableMetricsAPI: enableMetricsAPI,
		UnblockHTTPAPI:   unblockHTTPAPI,
		NoStartupMessage: noStartupMessage,
		APIToken:         apiToken,
		APIHost:          apiHost,
		APIPort:          apiPort,
	}

	// Execute core logic and exit with the returned status code (0 for success, 1 for failure).
	if exitCode := runMain(cfg); exitCode != 0 {
		logrus.WithField("exit_code", exitCode).Debug("Exiting with non-zero status")
		os.Exit(exitCode)
	}
}

// getContainerID retrieves the actual container ID using Docker API by matching the HOSTNAME
// environment variable with container.Config.Hostname.
//
// Returns:
//   - types.ContainerID: The container ID if found.
//   - error: Non-nil if the container ID cannot be retrieved.
func getContainerID(client container.Client) (types.ContainerID, error) {
	hostname := os.Getenv("HOSTNAME")
	if hostname == "" {
		return "", ErrContainerIDNotFound
	}

	containers, err := client.ListAllContainers()
	if err != nil {
		return "", fmt.Errorf("failed to list all containers: %w", err)
	}

	for _, container := range containers {
		if container.ContainerInfo().Config.Hostname == hostname {
			return container.ID(), nil
		}
	}

	return "", ErrContainerIDNotFound
}

// deriveScopeFromContainer attempts to derive the operational scope from the current container's scope label.
// This is crucial for self-update scenarios where a new Watchtower instance needs to inherit
// the same scope as the instance being replaced to maintain proper isolation and prevent
// cross-scope interference during the update process.
//
// Parameters:
//   - client: Container client for Docker operations.
//
// Returns:
//   - error: Non-nil if container ID retrieval or scope derivation fails, nil on success or if derivation is skipped.
func deriveScopeFromContainer(client container.Client) error {
	// Skip derivation if scope is already explicitly set via flags or environment.
	if scope != "" {
		return nil
	}

	// Retrieve the actual container ID using Docker API by matching HOSTNAME.
	containerID, err := getContainerID(client)
	if err != nil {
		// Container ID retrieval failed, return the error for proper handling.
		return err
	}

	// Attempt to retrieve the container object using the retrieved container ID.
	// This lookup is necessary to access the container's labels and metadata.
	container, err := client.GetContainer(containerID)
	if err != nil {
		// Container lookup failed, but this is not a fatal error since
		// scope derivation is a best-effort operation.
		return fmt.Errorf("failed to retrieve current container for scope derivation: %w", err)
	}

	// Extract the scope label from the container. The Scope() method returns
	// the scope value and a boolean indicating whether the label is present.
	// Only set the scope if the label exists and contains a non-empty value.
	if derivedScope, ok := container.Scope(); ok && derivedScope != "" {
		scope = derivedScope
		logrus.WithFields(logrus.Fields{
			"derived_scope": scope,
			"container_id":  containerID,
		}).Debug("Derived operational scope from current container's scope label")
	}

	return nil
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
func runMain(cfg config.RunConfig) int {
	// Log the container names being processed for debugging visibility.
	logrus.WithField("names", cfg.Names).Debug("Processing specified containers")

	// Validate flag compatibility to prevent conflicting operational modes.
	if rollingRestart && monitorOnly {
		logrus.WithFields(logrus.Fields{
			"rolling_restart": rollingRestart,
			"monitor_only":    monitorOnly,
		}).Fatal("Incompatible flags: rolling restarts and monitor-only")
	}

	// Ensure the Docker client is fully initialized before proceeding.
	awaitDockerClient()

	// Perform sanity checks on the environment and container setup.
	if err := actions.CheckForSanity(client, cfg.Filter, rollingRestart); err != nil {
		logNotify("Sanity check failed", err)

		return 1 // Exit immediately after logging
	}

	// Initialize a lock channel to prevent concurrent updates.
	updateLock := make(chan bool, 1)
	updateLock <- true

	// Handle one-time update mode, executing updates and registering metrics.
	if cfg.RunOnce {
		logging.WriteStartupMessage(
			cfg.Command,
			time.Time{},
			cfg.FilterDesc,
			scope,
			client,
			notifier,
			meta.Version,
			nil, // read from flags
		)
		params := types.UpdateParams{
			Cleanup:       cleanup,
			RunOnce:       cfg.RunOnce,
			SkipSelfUpdate: false, // SkipSelfUpdate is not needed for run-once
		}
		metric := runUpdatesWithNotifications(context.Background(), cfg.Filter, params)
		metrics.Default().RegisterScan(metric)
		notifier.Close()

		// Update current Watchtower container's restart policy to "no" to prevent unwanted restarts
		if containerID, err := getContainerID(client); err != nil {
			logrus.WithError(err).
				Warn("Failed to get current container ID for restart policy update")
		} else if container, err := client.GetContainer(containerID); err != nil {
			logrus.WithError(err).Warn("Failed to get current container for restart policy update")
		} else {
			updateConfig := dockerContainer.UpdateConfig{
				RestartPolicy: dockerContainer.RestartPolicy{
					Name: "no",
				},
			}
			if err := client.UpdateContainer(container, updateConfig); err != nil {
				logrus.WithError(err).Warn("Failed to update restart policy to 'no' for current container")
			} else {
				logrus.Debug("Updated current container restart policy to 'no'")
			}
		}

		return 0
	}

	// Check for and resolve conflicts with multiple Watchtower instances.
	var cleanupImageInfos []types.CleanedImageInfo

	cleanupOccurred, err := actions.CheckForMultipleWatchtowerInstances(
		client,
		cleanup,
		scope,
		&cleanupImageInfos,
	)
	if err != nil {
		if strings.Contains(err.Error(), "failed to list containers") {
			logNotify("Failed to detect Watchtower instances", err)

			return 1
		}

		logNotify("Multiple Watchtower instances detected", err)

		return 1
	}

	// Disable update-on-start if cleanup occurred to prevent redundant updates after self-update
	if cleanupOccurred {
		cfg.UpdateOnStart = false

		logrus.Debug("Disabled update-on-start due to cleanup of excess Watchtower instances")
	}

	// Create a cancellable context for managing API and scheduler shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Configure and start the HTTP API, handling any startup errors.
	if err := api.SetupAndStartAPI(ctx, cfg.APIHost, cfg.APIPort, cfg.APIToken, cfg.EnableUpdateAPI, cfg.EnableMetricsAPI, cfg.UnblockHTTPAPI, cfg.NoStartupMessage, cfg.Filter, cfg.Command, cfg.FilterDesc, updateLock, cleanup, client, notifier, scope, meta.Version, runUpdatesWithNotifications, filters.FilterByImage, metrics.Default, logging.WriteStartupMessage); err != nil {
		return 1
	}

	// Schedule and execute periodic updates, handling errors or shutdown.
	if err := scheduling.RunUpgradesOnSchedule(ctx, cfg.Command, cfg.Filter, cfg.FilterDesc, updateLock, cleanup, scheduleSpec, logging.WriteStartupMessage, runUpdatesWithNotifications, client, scope, notifier, meta.Version, cfg.UpdateOnStart, cleanupOccurred); err != nil {
		logNotify("Scheduled upgrades failed", err)

		return 1
	}

	// Default to success if execution completes without errors.
	return 0
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
	notifier.StartNotification(false)
	notifier.SendNotification(nil)
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
	sleepFunc(1 * time.Second)
}
