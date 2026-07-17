package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	"github.com/nicholas-fedor/watchtower/internal/api"
	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/events"
	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/internal/logging"
	"github.com/nicholas-fedor/watchtower/internal/meta"
	"github.com/nicholas-fedor/watchtower/internal/metrics"
	"github.com/nicholas-fedor/watchtower/internal/scheduling"
	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
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

	// useComposeDependsOn is a flag that controls whether the Docker Compose depends_on label
	// is processed for container dependency ordering.
	//
	// It is set in preRun via the --use-compose-depends-on flag or the WATCHTOWER_USE_COMPOSE_DEPENDS_ON environment variable,
	// defaulting to true for backward compatibility.
	useComposeDependsOn bool

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

	// reviveStopped is a boolean flag that starts stopped containers after an update.
	//
	// It is set in preRun via the --revive-stopped flag or the WATCHTOWER_REVIVE_STOPPED environment variable,
	// allowing users to have Watchtower start containers that were originally stopped before the update.
	reviveStopped bool

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

	// monitoredImageNamePatterns is a slice of image name patterns that
	// restricts which containers are monitored.
	//
	// When set, only containers whose image matches one of these patterns are monitored.
	// It is populated in preRun from the --monitored-image-name-patterns flag or the
	// WATCHTOWER_MONITORED_IMAGE_NAME_PATTERNS environment variable, allowing users to
	// configure specific image patterns for Watchtower's monitoring scope.
	monitoredImageNamePatterns []string

	// skippedImageNamePatterns is a slice of image name patterns for
	// containers to exclude from monitoring.
	//
	// Matching containers are not monitored. It is populated in preRun from the
	// --skipped-image-name-patterns flag or the WATCHTOWER_SKIPPED_IMAGE_NAME_PATTERNS
	// environment variable, providing a way to blacklist specific image patterns.
	skippedImageNamePatterns []string

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

	// cooldownDelay specifies the minimum age a new image must have before Watchtower will update a container.
	//
	// It is set in preRun via the --cooldown-delay flag or the WATCHTOWER_COOLDOWN_DELAY environment variable,
	// providing a safeguard against supply chain attacks by deferring updates to newly pushed images.
	cooldownDelay time.Duration

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

	// includeStopped indicates whether stopped containers are included in updates.
	//
	// It is set in preRun via the --include-stopped flag or the WATCHTOWER_INCLUDE_STOPPED environment variable,
	// allowing Watchtower to manage containers that are not currently running.
	includeStopped bool

	// includeRestarting indicates whether restarting containers are included in updates.
	//
	// It is set in preRun via the --include-restarting flag or the WATCHTOWER_INCLUDE_RESTARTING environment variable,
	// allowing Watchtower to manage containers that are in the process of restarting.
	includeRestarting bool

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

	// ephemeralSelfUpdate is a boolean flag enabling the ephemeral container-based self-update mechanism.
	//
	// When true, Watchtower uses a short-lived orchestrator container to perform the self-update
	// transition atomically. When false (default), the existing rename-based approach is used.
	// It is set in preRun via the --ephemeral-self-update flag or WATCHTOWER_EPHEMERAL_SELF_UPDATE env var.
	ephemeralSelfUpdate bool

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

	// createSignalContext is a function variable for creating a signal-aware context.
	//
	// It wraps signal.NotifyContext to allow overriding in tests for testing signal handling behavior.
	// The function creates a context that is canceled when the specified signals (SIGINT, SIGTERM) are received.
	createSignalContext = signal.NotifyContext

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

// init registers command-line flags for the root command during package initialization.
//
// It invokes functions from the flags package to set default values and register flags for Docker configuration
// (e.g., --host), system behavior (e.g., --interval), and notifications (e.g., --notifications), establishing
// the CLI's configurable parameters before execution begins.
func init() {
	flags.SetDefaults()
	flags.RegisterDockerFlags(rootCmd)
	flags.RegisterSystemFlags(rootCmd)
	flags.RegisterNotificationFlags(rootCmd)
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

	// Validate the timeout value to ensure it's non-negative, preventing invalid stop durations.
	if timeout < 0 {
		logrus.Fatal("Please specify a positive value for timeout value.")
	}

	// Warn if timeout is unreasonably small, which likely indicates a configuration
	// error (such as passing a raw value without a time duration unit).
	if timeout > 0 && timeout < time.Second {
		logrus.WithField("timeout", timeout).
			Warn("WATCHTOWER_TIMEOUT is less than 1 second")
	}

	// Set additional configuration flags that control update behavior and scope.
	enableLabel, _ = flagsSet.GetBool("label-enable")

	// Set containers that are excluded from Watchtower's handling.
	disableContainers, _ = flagsSet.GetStringSlice("disable-containers")
	for i := range disableContainers {
		disableContainers[i] = util.NormalizeContainerName(disableContainers[i])
	}

	// Set image name patterns to define which respective containers are monitored.
	monitoredImageNamePatterns, _ = flagsSet.GetStringSlice("monitor-image-names")
	for i := range monitoredImageNamePatterns {
		monitoredImageNamePatterns[i] = strings.TrimSpace(monitoredImageNamePatterns[i])
	}

	// Set image name patterns for respective containers to skip during monitoring.
	skippedImageNamePatterns, _ = flagsSet.GetStringSlice("skip-image-names")
	for i := range skippedImageNamePatterns {
		skippedImageNamePatterns[i] = strings.TrimSpace(skippedImageNamePatterns[i])
	}

	// Enable/disable execution of scripts before or after updates.
	lifecycleHooks, _ = flagsSet.GetBool("enable-lifecycle-hooks")

	// Enable/disable execution of container-by-container updates.
	rollingRestart, _ = flagsSet.GetBool("rolling-restart")

	// Define the operational scope of the Watchtower instance.
	scope, _ = flagsSet.GetString("scope")

	// Enable/disable operational precedence of labels.
	labelPrecedence, _ = flagsSet.GetBool("label-take-precedence")

	// Enable/disable Docker Compose depends_on label processing.
	useComposeDependsOn, _ = flagsSet.GetBool("use-compose-depends-on")

	// Retrieve lifecycle UID and GID flags.
	lifecycleUID, _ = flagsSet.GetInt("lifecycle-uid")
	lifecycleGID, _ = flagsSet.GetInt("lifecycle-gid")

	// Retrieve cooldown delay for minimum image age before updating.
	// Supports extended units: d (days), w (weeks), M (months).
	// Reset to zero to avoid persisting values from a previous preRun invocation.
	cooldownDelay = time.Duration(0)

	cooldownDelayStr, _ := flagsSet.GetString("cooldown-delay")

	if cooldownDelayStr != "" {
		parsed, err := util.ParseDuration(cooldownDelayStr)
		if err != nil {
			logrus.WithError(err).Fatal("Please specify a valid cooldown delay value (e.g., 24h, 3d, 1w, 1M).")
		}

		cooldownDelay = parsed
	}

	// Validate the cooldown delay value to ensure it's non-negative.
	if cooldownDelay < 0 {
		logrus.Fatal("Please specify a positive value for cooldown delay value.")
	}

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
	includeStopped, _ = flagsSet.GetBool("include-stopped")
	includeRestarting, _ = flagsSet.GetBool("include-restarting")
	reviveStopped, _ = flagsSet.GetBool("revive-stopped")
	removeVolumes, _ := flagsSet.GetBool("remove-volumes")
	warnOnHeadPullFailed, _ := flagsSet.GetString("warn-on-head-failure")
	disableMemorySwappiness, _ := flagsSet.GetBool("disable-memory-swappiness")
	cpuCopyMode, _ = flagsSet.GetString("cpu-copy-mode")
	ephemeralSelfUpdate, _ = flagsSet.GetBool("ephemeral-self-update")

	// Initialize the Docker client before the orchestrator check.
	// The orchestrator needs a valid client to perform container operations.
	client = container.NewClient(container.ClientOptions{
		IncludeStopped:          includeStopped,
		ReviveStopped:           reviveStopped,
		RemoveVolumes:           removeVolumes,
		IncludeRestarting:       includeRestarting,
		DisableMemorySwappiness: disableMemorySwappiness,
		CPUCopyMode:             cpuCopyMode,
		WarnOnHeadFailed:        container.WarningStrategy(warnOnHeadPullFailed),
	})

	// Check for orchestrator mode early — this is an internal mode where Watchtower
	// runs as a one-shot orchestrator for self-update. It reads environment variables
	// to determine the old container ID, new image, and original container name.
	if isOrchestrator, _ := flagsSet.GetBool("self-update-orchestrator"); isOrchestrator {
		logrus.Info("Running in ephemeral self-update orchestrator mode")

		actions.RunOrchestrator(context.Background(), client)

		// Defensive: RunOrchestrator should always call os.Exit, but if it ever
		// returns unexpectedly, ensure the process terminates to prevent the
		// preRun flow from continuing into the main Watchtower loop.
		logrus.WithField("flag", "self-update-orchestrator").
			Fatal("RunOrchestrator returned unexpectedly; exiting to prevent unintended execution")
	}

	// Warn about potential redundancy in flag combinations that could result in no action.
	if monitorOnly && noPull {
		logrus.WithFields(logrus.Fields{
			"monitor_only": monitorOnly,
			"no_pull":      noPull,
		}).Warn("Combining monitor-only and no-pull might result in no updates")
	}

	// Create a timeout-bound context for Docker API lookups to prevent hanging indefinitely.
	// This ensures the container ID lookup fails fast if the Docker API is unresponsive.
	const containerLookupTimeout = 5 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), containerLookupTimeout)
	defer cancel()

	// Retrieve and store the current container ID for use throughout the application.
	currentWatchtowerContainerID, err = container.GetCurrentContainerID(ctx, client)
	if err != nil {
		logrus.WithError(err).Debug("Failed to get current container ID")

		currentWatchtowerContainerID = ""
	}

	// Retrieve the current Watchtower container.
	if currentWatchtowerContainerID != "" {
		currentWatchtowerContainer, err = client.GetCurrentWatchtowerContainer(
			ctx,
			currentWatchtowerContainerID,
		)
		if err != nil {
			logrus.WithError(err).Debug("Failed to get the current Watchtower Container")

			// Handle context deadline exceeded or cancellation
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				currentWatchtowerContainerID = ""
			}

			currentWatchtowerContainer = nil
		}
	}

	// Check if this is an old Watchtower container that should not run continuously.
	if scheduling.ShouldExitDueToInvalidRestart(currentWatchtowerContainer, flagsSet) {
		logrus.Info(
			"Detected invalid restart of old Watchtower container, stopping Watchtower container now",
		)

		if currentWatchtowerContainer != nil {
			updateConfig := dockerContainer.UpdateConfig{
				RestartPolicy: dockerContainer.RestartPolicy{
					Name: "no",
				},
			}

			ctx, cancel := context.WithTimeout(
				context.Background(),
				containerLookupTimeout,
			)
			defer cancel()

			err := client.UpdateContainer(
				ctx,
				currentWatchtowerContainer,
				updateConfig,
			)
			if err != nil {
				logrus.WithError(err).
					Warn("Failed to update restart policy to 'no' for old Watchtower container")
			} else {
				logrus.Debug("Updated restart policy to 'no' for old Watchtower container")
			}
		}

		logrus.Exit(0)
	}

	// Set up the notification system with types specified via flags (e.g., email, Slack).
	notifier = notifications.NewNotifier(cmd)
	notifier.AddLogHook()

	// Log deprecated notification configuration options, if set.
	notificationTypes, _ := cmd.Flags().GetStringSlice("notifications")
	notifications.LogLegacyDeprecationWarnings(notificationTypes)
}

// run executes the main Watchtower logic based on parsed command-line flags.
//
// It determines the operational mode (one-time update, HTTP API, or scheduled updates),
// builds the container filter, and delegates to runMain for core execution,
// exiting with a status code based on the outcome (0 for success, non-zero for failure).
//
// This function bridges flag parsing and the application's primary workflow.
//
// Parameters:
//   - command: The cobra.Command instance being executed, providing access to parsed flags.
//   - args: A slice of container names provided as positional arguments, used for filtering.
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
		disableContainers,
		monitoredImageNamePatterns,
		skippedImageNamePatterns,
		enableLabel,
		scope,
	)

	// Get flags controlling execution mode.
	runOnce, _ := command.PersistentFlags().GetBool("run-once")
	updateOnStart, _ := command.PersistentFlags().GetBool("update-on-start")
	noStartupMessage, _ := command.PersistentFlags().GetBool("no-startup-message")
	healthCheck, _ := command.PersistentFlags().GetBool("health-check")

	// Get flags controlling HTTP API behavior.
	apiEndpoints, _ := command.PersistentFlags().GetStringSlice("http-api-endpoints")
	// TODO: Remove legacy HTTP API enable flags when dropping them in v2.
	//nolint:godox
	legacyUpdateAPI, _ := command.PersistentFlags().GetBool("http-api-update")
	legacyMetricsAPI, _ := command.PersistentFlags().GetBool("http-api-metrics")
	legacyContainersAPI, _ := command.PersistentFlags().GetBool("http-api-containers")
	tlsCertPath, _ := command.PersistentFlags().GetString("http-api-tls-cert")
	tlsKeyPath, _ := command.PersistentFlags().GetString("http-api-tls-key")
	trustedProxies, _ := command.PersistentFlags().GetStringSlice("http-api-trusted-proxies")
	proxyHeader, _ := command.PersistentFlags().GetString("http-api-proxy-header")
	corsOrigins, _ := command.PersistentFlags().GetStringSlice("http-api-cors-origins")
	unblockHTTPAPI, _ := command.PersistentFlags().GetBool("http-api-periodic-polls")
	apiToken, _ := command.PersistentFlags().GetString("http-api-token")
	apiEventsToken, _ := command.PersistentFlags().GetString("http-api-events-token")

	endpointSet, err := config.ResolveEndpoints(
		apiEndpoints,
		legacyUpdateAPI,
		legacyMetricsAPI,
		legacyContainersAPI,
	)
	if err != nil {
		logrus.WithError(err).Fatal("Invalid HTTP API endpoint configuration")
	}

	// Get the HTTP API host and port, falling back to "8080" for port if not specified.
	flagsSet := command.PersistentFlags()

	apiHost, err := flagsSet.GetString("http-api-host")
	if err != nil {
		logrus.WithError(err).Fatal("Failed to get http-api-host flag")
	}

	// Validate if the configuration option has been changed from the default value.
	apiHostChanged := flagsSet.Lookup("http-api-host").Changed

	err = validateAPIHost(apiHost)
	if err != nil {
		logrus.Fatal(err)
	}

	apiPort, err := flagsSet.GetString("http-api-port")
	if err != nil {
		logrus.WithError(err).Fatal("Failed to get http-api-port flag")
	}

	// Validate if the configuration option has been changed from the default value.
	apiPortChanged := flagsSet.Lookup("http-api-port").Changed

	if apiPort == "" {
		apiPort = "8080" // Default port if unset.
	}

	// Get the HTTP API rate limit, defaulting to 60 requests per minute.
	apiRateLimit, err := flagsSet.GetInt("http-api-rate-limit")
	if err != nil {
		logrus.WithError(err).Fatal("Failed to get http-api-rate-limit flag")
	}

	// Validate if the configuration option has been changed from the default value.
	apiRateLimitChanged := flagsSet.Lookup("http-api-rate-limit").Changed

	// Set the API rate limit to the default value (60) if set to an invalid value.
	if apiRateLimit <= 0 {
		apiRateLimit = 60
	}

	checkAPITimeout, err := flagsSet.GetDuration("http-api-check-timeout")
	if err != nil {
		logrus.WithError(err).Fatal("Failed to get http-api-check-timeout flag")
	}

	checkAPITimeoutChanged := flagsSet.Lookup("http-api-check-timeout").Changed

	updateAPITimeout, err := flagsSet.GetDuration("http-api-update-timeout")
	if err != nil {
		logrus.WithError(err).Fatal("Failed to get http-api-update-timeout flag")
	}

	updateAPITimeoutChanged := flagsSet.Lookup("http-api-update-timeout").Changed

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
		Command:                 command,
		Names:                   normalizedContainerNames,
		Filter:                  filter,
		FilterDesc:              filterDesc,
		RunOnce:                 runOnce,
		UpdateOnStart:           updateOnStart,
		TLSCertPath:             tlsCertPath,
		TLSKeyPath:              tlsKeyPath,
		CORSAllowedOrigins:      corsOrigins,
		TrustedProxies:          trustedProxies,
		ProxyHeader:             proxyHeader,
		UnblockHTTPAPI:          unblockHTTPAPI,
		NoStartupMessage:        noStartupMessage,
		APIToken:                apiToken,
		APIEventsToken:          apiEventsToken,
		APIHost:                 apiHost,
		APIHostChanged:          apiHostChanged,
		APIPort:                 apiPort,
		APIPortChanged:          apiPortChanged,
		APIRateLimit:            apiRateLimit,
		APIRateLimitChanged:     apiRateLimitChanged,
		CheckAPITimeout:         checkAPITimeout,
		CheckAPITimeoutChanged:  checkAPITimeoutChanged,
		UpdateAPITimeout:        updateAPITimeout,
		UpdateAPITimeoutChanged: updateAPITimeoutChanged,
	}

	// Set the HTTP API Endpoint configuration.
	config.SetEndpointConfig(endpointSet, &cfg)

	// Warn if HTTP API configuration options are set without an endpoint enabled.
	if !httpAPIEndpointsEnabled(cfg) && anyHTTPAPIConfig(cfg) {
		logrus.Warn(
			"HTTP API configuration options are set, but no endpoints are enabled.",
		)
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

	// Initialize the event broadcaster for SSE subscribers.
	// Declared before runUpdatesWithNotifications so the closure can capture it.
	eventsBroadcaster := events.NewBroadcaster()

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
			ReviveStopped:                params.ReviveStopped,         // Start stopped containers after update if true
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
			CurrentContainerID:           currentWatchtowerContainerID, // ID of the current Watchtower container for self-update logic
			UseComposeDependsOn:          params.UseComposeDependsOn,   // Enable Docker Compose depends_on label processing
			SkipSelfUpdate:               params.SkipSelfUpdate,        // Skip Watchtower self-update
			EphemeralSelfUpdate:          ephemeralSelfUpdate,          // Use ephemeral container for self-update
			CooldownDelay:                cooldownDelay,                // Minimum time since image creation before allowing updates
			EventBroadcaster:             eventsBroadcaster,            // Broadcaster for SSE event streaming
		}

		metric := actions.RunUpdatesWithNotifications(ctx, actionParams)

		return metric
	}

	// Create a context that is automatically canceled on SIGINT/SIGTERM signals,
	// enabling graceful shutdown of the API, scheduler, and validation operations.
	// The stop function is returned but not needed as the context automatically
	// handles cleanup when the program exits.
	ctx, stop := createSignalContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// If rolling restarts are enabled, validate that the containers being monitored for
	// updates do not have linked dependencies.
	if rollingRestart {
		err := actions.ValidateRollingRestartDependencies(ctx, client, cfg.Filter, useComposeDependsOn)
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
			Cleanup:             cleanup,
			RunOnce:             cfg.RunOnce,
			MonitorOnly:         monitorOnly,
			UseComposeDependsOn: useComposeDependsOn,
			SkipSelfUpdate:      false, // SkipSelfUpdate is dynamically set in RunUpgradesOnSchedule based on skipFirstRun
			CooldownDelay:       cooldownDelay,
			ReviveStopped:       reviveStopped,
		}
		metric := runUpdatesWithNotifications(ctx, cfg.Filter, params)
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

			err := client.UpdateContainer(ctx, currentWatchtowerContainer, updateConfig)
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

	// Check for and cleanup old Watchtower containers within scope.
	totalRemovedInstances, err := actions.RemoveExcessWatchtowerInstances(
		ctx,
		client,
		cleanup,
		scope,
		&[]types.RemovedImageInfo{},
		currentWatchtowerContainer,
	)
	if err != nil {
		// Cleanup failure is non-fatal — log a warning and continue.
		// The old container may still be stopping; forcing exit would leave
		// no Watchtower running. Continuing ensures the new instance operates
		// even if the old container couldn't be fully cleaned up.
		logrus.WithError(err).Warn("Failed to clean up old Watchtower containers, continuing anyway")
	}

	// Check for and cleanup orphaned ephemeral orchestrator containers.
	// These may persist if the orchestrator crashed or was killed unexpectedly.
	// With AutoRemove: true, this is a safety net for edge cases.
	removedOrchestratorCount, orchestratorErr := container.RemoveOrphanedOrchestrators(ctx, client)
	if orchestratorErr != nil {
		logrus.WithError(orchestratorErr).
			WithField("removed_orchestrators", removedOrchestratorCount).
			Warn("Failed to clean up orphaned orchestrator containers, continuing anyway")
	} else if removedOrchestratorCount > 0 {
		logrus.WithField("removed_orchestrators", removedOrchestratorCount).
			Debug("Cleaned up orphaned orchestrator containers")
	}

	// Track if cleanup occurred to prevent redundant updates after self-update
	var cleanupOccurred bool
	if totalRemovedInstances > 0 {
		cleanupOccurred = true
	}

	// Disable update-on-start if cleanup occurred to prevent redundant updates after self-update
	if cleanupOccurred {
		cfg.UpdateOnStart = false

		logrus.Debug("Disabled update-on-start due to cleanup of old Watchtower containers")
	}

	// Determine whether self-update should be skipped because the running
	// Watchtower container has published host ports. Docker cannot rebind
	// an occupied port during container replacement. Ephemeral self-updates
	// are exempt, because they remove the old container before creating the new
	// one, so no port conflict occurs.
	//
	// Perform this check here rather than inside SetupAndStartAPI so the
	// warning always appears, even when no HTTP API endpoints are enabled
	// and SetupAndStartAPI returns early.
	skipSelfUpdate := currentWatchtowerContainer != nil &&
		currentWatchtowerContainer.HasExposedPorts() &&
		!ephemeralSelfUpdate
	if skipSelfUpdate {
		logrus.Warn("Published port detected - self-updates disabled.")
	}

	err = api.SetupAndStartAPI(
		ctx,
		config.Options{
			Host:                         cfg.APIHost,
			Port:                         cfg.APIPort,
			Token:                        cfg.APIToken,
			EventsToken:                  cfg.APIEventsToken,
			RateLimit:                    cfg.APIRateLimit,
			EnableCheckAPI:               cfg.EnableCheckAPI,
			EnableConfigAPI:              cfg.EnableConfigAPI,
			EnableContainersAPI:          cfg.EnableContainersAPI,
			EnableEventsAPI:              cfg.EnableEventsAPI,
			EnableHealthAPI:              cfg.EnableHealthAPI,
			EnableHistoryAPI:             cfg.EnableHistoryAPI,
			EnableImagesAPI:              cfg.EnableImagesAPI,
			EnableMetricsAPI:             cfg.EnableMetricsAPI,
			EnableSwaggerAPI:             cfg.EnableSwaggerAPI,
			EnableUpdateAPI:              cfg.EnableUpdateAPI,
			CheckTimeout:                 cfg.CheckAPITimeout,
			UpdateTimeout:                cfg.UpdateAPITimeout,
			TLSCertPath:                  cfg.TLSCertPath,
			TLSKeyPath:                   cfg.TLSKeyPath,
			CORSAllowedOrigins:           cfg.CORSAllowedOrigins,
			TrustedProxies:               cfg.TrustedProxies,
			ProxyHeader:                  cfg.ProxyHeader,
			UnblockHTTPAPI:               cfg.UnblockHTTPAPI,
			NoStartupMessage:             cfg.NoStartupMessage,
			Filter:                       cfg.Filter,
			Command:                      cfg.Command,
			FilterDesc:                   cfg.FilterDesc,
			UpdateLock:                   updateLock,
			Cleanup:                      cleanup,
			MonitorOnly:                  monitorOnly,
			NoPull:                       noPull,
			NoRestart:                    noRestart,
			RollingRestart:               rollingRestart,
			IncludeStopped:               includeStopped,
			IncludeRestarting:            includeRestarting,
			LifecycleHooks:               lifecycleHooks,
			LabelEnable:                  enableLabel,
			LabelPrecedence:              labelPrecedence,
			CooldownDelay:                cooldownDelay,
			SkipSelfUpdate:               skipSelfUpdate,
			ReviveStopped:                reviveStopped,
			UseComposeDependsOn:          useComposeDependsOn,
			Client:                       client,
			Notifier:                     notifier,
			NotificationSplitByContainer: notificationSplitByContainer,
			Scope:                        scope,
			Version:                      meta.Version,
			RunUpdatesWithNotifications:  runUpdatesWithNotifications,
			FilterByImage:                filters.FilterByImage,
			DefaultMetrics:               metrics.Default,
			WriteStartupMessage:          logging.WriteStartupMessage,
			EventBroadcaster:             eventsBroadcaster,
			OnUnexpectedServerStop: func(listenErr error) {
				logrus.WithError(listenErr).Error(
					"Canceling process context after unexpected HTTP server stop",
				)
				stop()
			},
		},
	)
	if err != nil {
		logNotify("API setup failed", err)

		return 1 // Exit while indicating failure.
	}

	// Schedule and execute periodic updates, handling errors or shutdown.
	// The startup message is skipped here if it was already sent by the HTTP API in blocking mode.
	startupMessageSent := cfg.EnableUpdateAPI && !cfg.UnblockHTTPAPI

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
		startupMessageSent,
		ephemeralSelfUpdate,
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

// errInvalidAPIHost indicates http-api-host is neither empty nor a valid IP.
var errInvalidAPIHost = errors.New(
	"invalid http-api-host: must be empty or a valid IP address (IPv4 or IPv6)",
)

// validateAPIHost ensures http-api-host is empty (all interfaces) or a valid IP.
//
// Parameters:
//   - host: Value of the http-api-host flag.
//
// Returns:
//   - error: Non-nil when host is a non-empty non-IP string (e.g. a hostname).
func validateAPIHost(host string) error {
	if host == "" {
		return nil
	}

	if net.ParseIP(host) == nil {
		return fmt.Errorf("%w: %q", errInvalidAPIHost, host)
	}

	return nil
}

// anyHTTPAPIConfig reports whether any HTTP API-related settings are present
// without enabled endpoints, so operators can be warned about a no-op config.
func anyHTTPAPIConfig(cfg types.RunConfig) bool {
	return cfg.APIToken != "" ||
		cfg.APIEventsToken != "" ||
		cfg.TLSCertPath != "" ||
		cfg.TLSKeyPath != "" ||
		len(cfg.CORSAllowedOrigins) > 0 ||
		len(cfg.TrustedProxies) > 0 ||
		cfg.ProxyHeader != "" ||
		cfg.APIHostChanged ||
		cfg.APIPortChanged ||
		cfg.APIRateLimitChanged ||
		cfg.CheckAPITimeoutChanged ||
		cfg.UpdateAPITimeoutChanged
}

// httpAPIEndpointsEnabled reports whether any HTTP API endpoint is enabled.
func httpAPIEndpointsEnabled(cfg types.RunConfig) bool {
	return cfg.EnableUpdateAPI ||
		cfg.EnableMetricsAPI ||
		cfg.EnableContainersAPI ||
		cfg.EnableCheckAPI ||
		cfg.EnableSwaggerAPI ||
		cfg.EnableHealthAPI ||
		cfg.EnableHistoryAPI ||
		cfg.EnableImagesAPI ||
		cfg.EnableConfigAPI ||
		cfg.EnableEventsAPI
}
