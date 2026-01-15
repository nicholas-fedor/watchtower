package cmd

import (
	"context"
	"net"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	dockerContainer "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	"github.com/nicholas-fedor/watchtower/internal/api"
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

	// currentWatchtowerContainerID stores the current Watchtower container ID.
	//
	// It is initialized once in preRun after the client is set up, and used throughout the application
	// to avoid repeated calls to GetCurrentContainerID. If retrieval fails, it is set to an empty string.
	currentWatchtowerContainerID types.ContainerID

	// currentWatchtowerContainer holds the current Watchtower container instance.
	//
	// It is initialized in preRun by retrieving the container object using the currentWatchtowerContainerID,
	// remains nil if retrieval fails or yields an unexpected type, and is used for operations like updating
	// restart policy, validating restarts, and cleaning up excess instances.
	currentWatchtowerContainer types.Container

	// sleepFunc is a function variable for time.Sleep, allowing it to be overridden in tests.
	//
	// It is initialized to time.Sleep by default, providing a way to mock sleep behavior during testing
	// to avoid delays in unit tests or control timing in integration tests.
	sleepFunc = time.Sleep

	// runUpdatesWithNotifications is a function variable for performing container updates and sending notifications.
	//
	// It is initialized inside runMain with a closure that executes actions.RunUpdatesWithNotifications,
	// allowing it to be overridden in tests to mock the update process. It takes a context, filter, and update params,
	// and returns a metric summarizing the update session.
	runUpdatesWithNotifications func(context.Context, types.Filter, types.UpdateParams) *metrics.Metric

	// rootCmd represents the root command for the Watchtower CLI, serving as the entry point for all subcommands.
	//
	// It defines the base usage string, short and long descriptions, and assigns lifecycle hooks (PreRun and Run)
	// to manage setup and execution, initialized with default behavior and configured via flags during runtime.
	rootCmd = NewRootCommand()
)

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
	err := rootCmd.Execute()
	if err != nil {
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
	err := flags.SetupLogging(flagsSet)
	if err != nil {
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

	// Set containers that are excluded from Watchtower's handling.
	disableContainers, _ = flagsSet.GetStringSlice("disable-containers")
	for i := range disableContainers {
		disableContainers[i] = util.NormalizeContainerName(disableContainers[i])
	}

	// Enable/disable execution of scripts before or after updates.
	lifecycleHooks, _ = flagsSet.GetBool("enable-lifecycle-hooks")

	// Enable/disable execution of container-by-container updates.
	rollingRestart, _ = flagsSet.GetBool("rolling-restart")

	// Define the operational scope of the Watchtower instance.
	scope, _ = flagsSet.GetString("scope")

	// Enable/disable operational precedence of labels.
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
	err = flags.EnvConfig(cmd)
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

	// Retrieve and store the current container ID for use throughout the application.
	currentWatchtowerContainerID, err = container.GetCurrentContainerID(client)
	if err != nil {
		logrus.WithError(err).Debug("Failed to get current container ID")

		currentWatchtowerContainerID = ""
	}

	// Retrieve the current Watchtower container.
	if currentWatchtowerContainerID != "" {
		currentWatchtowerContainer, err = client.GetCurrentWatchtowerContainer(
			currentWatchtowerContainerID,
		)
		if err != nil {
			logrus.WithError(err).Debug("Failed to get the current Watchtower Container")
		}
	}

	// Check if this is an old Watchtower container that should not run continuously.
	if scheduling.ShouldExitDueToInvalidRestart(currentWatchtowerContainer, flagsSet) {
		logrus.Info(
			"Detected invalid restart of old Watchtower container, stopping Watchtower container now",
		)
		logrus.Exit(0)
	}

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
func run(command *cobra.Command, args []string) {
	logrus.WithField("positional_args", args).
		Debug("Received positional arguments for container filtering")

	// Strip forward slash from container names.
	normalizedContainerNames := make([]string, 0, len(args))
	for _, arg := range args {
		normalizedContainerNames = append(
			normalizedContainerNames,
			util.NormalizeContainerName(arg),
		)
	}

	// Determine the effective operational scope, prioritizing explicit scope over scope derived from the container's label.
	// This ensures scope persistence during self-updates.
	var err error

	scope, err = container.GetEffectiveScope(currentWatchtowerContainer, scope)
	if err != nil {
		logrus.WithError(err).Debug("Scope derivation failed, continuing with current scope")
	}

	// Build the filter and its description based on normalized names, exclusions, and label settings.
	filter, filterDesc := filters.BuildFilter(
		normalizedContainerNames,
		disableContainers, // Normalized container names
		enableLabel,
		scope,
	)

	// Get flags controlling execution mode and HTTP API behavior.
	runOnce, _ := command.PersistentFlags().GetBool("run-once")
	updateOnStart, _ := command.PersistentFlags().GetBool("update-on-start")
	enableUpdateAPI, _ := command.PersistentFlags().GetBool("http-api-update")
	enableMetricsAPI, _ := command.PersistentFlags().GetBool("http-api-metrics")
	unblockHTTPAPI, _ := command.PersistentFlags().GetBool("http-api-periodic-polls")
	noStartupMessage, _ := command.PersistentFlags().GetBool("no-startup-message")
	apiToken, _ := command.PersistentFlags().GetString("http-api-token")
	healthCheck, _ := command.PersistentFlags().GetBool("health-check")

	// Get the HTTP API host and port, falling back to "8080" for port if not specified.
	flagsSet := command.PersistentFlags()

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
	cfg := types.RunConfig{
		Command:          command,
		Names:            normalizedContainerNames,
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

// runMain contains the core Watchtower logic after early exits are handled.
//
// It validates the environment, performs one-time updates if specified,
// sets up the HTTP API, and schedules periodic updates while managing
// context and concurrency to ensure graceful operation.
//
// Parameters:
//   - cfg: The RunConfig struct containing all necessary configuration parameters for execution.
//
// Returns:
//   - int: An exit code (0 for success, 1 for failure) used to terminate the program.
func runMain(cfg types.RunConfig) int {
	// Log the container names being processed for debugging visibility.
	logrus.WithField("container_names", cfg.Names).Debug("Processing specified containers")

	// Validate flag compatibility to prevent conflicting operational modes.
	if rollingRestart && monitorOnly {
		logrus.WithFields(logrus.Fields{
			"rolling_restart": rollingRestart,
			"monitor_only":    monitorOnly,
		}).Fatal("Incompatible flags: rolling restarts and monitor-only")
	}

	// Ensure the Docker client is fully initialized before proceeding.
	awaitDockerClient()

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
		// Prepare parameters for the update action
		actionParams := actions.RunUpdatesWithNotificationsParams{
			Client:                       client,                       // Docker client for container operations
			Notifier:                     notifier,                     // Notification system for sending update status messages
			NotificationSplitByContainer: notificationSplitByContainer, // Enable separate notifications for each updated container
			NotificationReport:           notificationReport,           // Enable report-based notifications
			Filter:                       filter,                       // Container filter determining which containers are targeted
			Cleanup:                      params.Cleanup,               // Remove old images after container updates
			NoRestart:                    noRestart,                    // Prevent containers from being restarted after updates
			MonitorOnly:                  params.MonitorOnly,           // Monitor containers without performing updates
			LifecycleHooks:               lifecycleHooks,               // Enable pre- and post-update lifecycle hook commands
			RollingRestart:               rollingRestart,               // Update containers sequentially rather than all at once
			LabelPrecedence:              labelPrecedence,              // Give container label settings priority over global flags
			NoPull:                       noPull,                       // Skip pulling new images from registry during updates
			Timeout:                      timeout,                      // Maximum duration for container stop operations
			LifecycleUID:                 lifecycleUID,                 // Default UID to run lifecycle hooks as
			LifecycleGID:                 lifecycleGID,                 // Default GID to run lifecycle hooks as
			CPUCopyMode:                  cpuCopyMode,                  // CPU settings handling when recreating containers
			PullFailureDelay:             params.PullFailureDelay,      // Delay after failed Watchtower self-update pulls
			RunOnce:                      params.RunOnce,               // Perform one-time update and exit
			SkipSelfUpdate:               params.SkipSelfUpdate,        // Skip Watchtower self-update
			CurrentContainerID:           currentWatchtowerContainerID, // ID of the current Watchtower container for self-update logic
		}

		metric := actions.RunUpdatesWithNotifications(ctx, actionParams)

		return metric
	}

	// If rolling restarts are enabled, validate that the containers being monitored for
	// updates do not have linked dependencies.
	if rollingRestart {
		err := actions.ValidateRollingRestartDependencies(client, cfg.Filter)
		if err != nil {
			logNotify("Rolling restart compatibility validation failed", err)

			return 1 // Exit immediately after logging failure
		}
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
			Cleanup:        cleanup,
			RunOnce:        cfg.RunOnce,
			MonitorOnly:    monitorOnly,
			SkipSelfUpdate: false, // SkipSelfUpdate is dynamically set in RunUpgradesOnSchedule based on skipFirstRun
		}
		metric := runUpdatesWithNotifications(context.Background(), cfg.Filter, params)
		metrics.Default().RegisterScan(metric)
		notifier.Close()

		// Update current Watchtower container's restart policy to "no" to prevent unwanted restarts
		if currentWatchtowerContainer == nil {
			logrus.Warn("Current container not available for restart policy update")
		} else {
			updateConfig := dockerContainer.UpdateConfig{
				RestartPolicy: dockerContainer.RestartPolicy{
					Name: "no",
				},
			}

			err := client.UpdateContainer(currentWatchtowerContainer, updateConfig)
			if err != nil {
				logrus.WithError(err).
					Warn("Failed to update restart policy to 'no' for current container")
			} else {
				logrus.Debug("Updated current container restart policy to 'no'")
			}
		}

		return 0 // Exit after successful execution
	}

	// Retrieve the current Watchtower container for cleanup operations.
	if currentWatchtowerContainer == nil && currentWatchtowerContainerID != "" {
		logrus.Warn("Current container not cached for cleanup")
	}

	// Check for and cleanup excess Watchtower instances within scope.
	totalRemovedInstances, err := actions.RemoveExcessWatchtowerInstances(
		client,
		cleanup,
		scope,
		&[]types.RemovedImageInfo{},
		currentWatchtowerContainer,
	)
	if err != nil {
		if strings.Contains(err.Error(), "failed to list containers") {
			logNotify("Failed to detect Watchtower instances", err)

			return 1
		}

		logNotify("Multiple Watchtower instances detected", err)

		return 1 // Log failure and exit.
	}

	// Track if cleanup occurred to prevent redundant updates after self-update
	var cleanupOccurred bool
	if totalRemovedInstances > 0 {
		cleanupOccurred = true
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
	err = api.SetupAndStartAPI(
		ctx,
		cfg.APIHost,
		cfg.APIPort,
		cfg.APIToken,
		cfg.EnableUpdateAPI,
		cfg.EnableMetricsAPI,
		cfg.UnblockHTTPAPI,
		cfg.NoStartupMessage,
		cfg.Filter,
		cfg.Command,
		cfg.FilterDesc,
		updateLock,
		cleanup,
		monitorOnly,
		client,
		notifier,
		scope,
		meta.Version,
		runUpdatesWithNotifications,
		filters.FilterByImage,
		metrics.Default,
		logging.WriteStartupMessage,
	)
	if err != nil {
		logNotify("API setup failed", err)

		return 1 // Exit while indicating failure.
	}

	// Schedule and execute periodic updates, handling errors or shutdown.
	err = scheduling.RunUpgradesOnSchedule(
		ctx, cfg.Command,
		cfg.Filter,
		cfg.FilterDesc,
		updateLock,
		cleanup,
		scheduleSpec,
		logging.WriteStartupMessage,
		runUpdatesWithNotifications,
		client,
		scope,
		notifier,
		meta.Version,
		monitorOnly,
		cfg.UpdateOnStart,
		cleanupOccurred,
		currentWatchtowerContainer,
	)
	if err != nil {
		logNotify("Scheduled upgrades failed", err)

		return 1 // Exit while indicating failure.
	}

	return 0 // Default to success if execution completes without errors.
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
