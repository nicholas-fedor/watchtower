package cmd

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	"github.com/nicholas-fedor/watchtower/internal/api"
	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/events"
	appConfig "github.com/nicholas-fedor/watchtower/internal/config"
	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/internal/logging"
	"github.com/nicholas-fedor/watchtower/internal/meta"
	"github.com/nicholas-fedor/watchtower/internal/metrics"
	"github.com/nicholas-fedor/watchtower/internal/scheduling"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/notifications"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const (
	// restartPolicyTimeout is the maximum duration allowed for restart-policy
	// update operations.
	//
	// It bounds the Docker API call that sets the current Watchtower
	// container's restart policy to "no", so a slow or unresponsive
	// daemon cannot delay shutdown paths.
	restartPolicyTimeout = 5 * time.Second

	// containerLookupTimeout is the maximum duration allowed for current
	// container ID lookups.
	//
	// It bounds the Docker API / hostname / mountinfo detection sequence,
	// so startup cannot hang indefinitely if the daemon or container runtime
	// metadata is unreachable.
	containerLookupTimeout = 5 * time.Second
)

var (
	// appCfg is the resolved process configuration from appconfig.Load.
	//
	// It is the single source of operational policy for run-once, schedule, and API paths.
	// Values originate from CLI flags and environment variables registered in internal/flags
	// and are resolved once at startup (and again in run when positional container names apply).
	appCfg appConfig.Config

	// client is the Docker client instance used to interact with container operations in Watchtower.
	//
	// It provides an interface for listing, stopping, starting, and managing containers, initialized during
	// the preRun phase with options derived from appCfg.ClientOptions() (DOCKER_HOST, TLS, API version, and
	// related client flags/environment variables).
	client container.Client

	// notifier is the notification system instance responsible for sending update status messages to configured channels.
	//
	// It is initialized in preRun from appCfg.Notify via notifications.NewNotifier, supporting
	// Shoutrrr URLs and (deprecated) legacy types such as email, Slack, or MSTeams.
	notifier types.Notifier

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
	flags.RegisterAll(rootCmd)
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
		Args:   cobra.ArbitraryArgs, // Permits any number of positional arguments, processed as container names later.
		PreRun: preRun,
		Run:    run,
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
// It processes command-line flag aliases, configures logging based on verbosity settings,
// expands secrets from files, maps Docker flags into the process environment, loads the
// immutable appconfig snapshot, initializes the Docker client and notification client, and
// handles early-exit paths (ephemeral self-update orchestrator and invalid old-container restarts).
//
// Parameters:
//   - cmd: The cobra.Command instance being executed, providing access to parsed flags.
//   - _: A slice of string arguments (unused here, as container names are applied in run when
//     reloading configuration for filtering).
func preRun(cmd *cobra.Command, _ []string) {
	flagsSet := cmd.PersistentFlags()

	// Bridge environment values onto unset flags so aliases and logging still see them.
	err := flags.ApplyEnvToFlags(flagsSet, flags.AllSpecs())
	if err != nil {
		logrus.WithError(err).Fatal("Failed to apply environment configuration")
	}

	// Apply porcelain, interval→schedule, and debug/trace log-level aliases.
	flags.ProcessFlagAliases(flagsSet)

	// Configure logging based on flags such as --debug, --trace, and --log-format.
	err = flags.SetupLogging(flagsSet)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to initialize logging")
	}

	// Expand secrets from files (for example notification URLs and API tokens).
	flags.GetSecretsFromFiles(cmd)

	// Map Docker connection flags into the process environment for the client stack.
	err = flags.EnvConfig(cmd)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to configure Docker environment")
	}

	// Load without positional names; run reloads with args for the final filter.
	appCfg, err = appConfig.Load(cmd, nil)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to load configuration")
	}

	logrus.WithField("scheduleSpec", appCfg.Schedule.Spec).
		Debug("Retrieved cron schedule specification from configuration")

	// Log the scope if specified, aiding debugging by confirming the operational boundary.
	if appCfg.Filter.Scope != "" {
		logrus.WithField("scope", appCfg.Filter.Scope).
			Debug("Configured operational scope")
	}

	// Initialize the Docker client from the resolved ClientOptions projection.
	client = container.NewClient(appCfg.ClientOptions())

	// Check for orchestrator mode early. This is an internal mode where Watchtower
	// runs as a one-shot orchestrator for self-update.
	if appCfg.Mode.SelfUpdateOrchestrator {
		logrus.Info("Running in ephemeral self-update orchestrator mode")

		actions.RunOrchestrator(context.Background(), client)

		currentWatchtowerContainer = resolveCurrentWatchtowerContainerForFallback(
			context.Background(),
			client,
		)

		setNoRestartPolicyCtx, cancel := context.WithTimeout(
			context.Background(),
			restartPolicyTimeout,
		)
		defer cancel()

		client.SetNoRestartPolicy(
			setNoRestartPolicyCtx,
			currentWatchtowerContainer,
		)

		logrus.WithField("flag", "self-update-orchestrator").
			Fatal("RunOrchestrator returned unexpectedly. Exiting to prevent unintended execution")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		containerLookupTimeout,
	)
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
	if scheduling.ShouldExitDueToInvalidRestart(
		currentWatchtowerContainer,
		appCfg.Mode.RunOnce,
	) {
		logrus.Info(
			"Detected invalid restart of old Watchtower container, stopping Watchtower container now",
		)

		exitCtx, exitCancel := context.WithTimeout(
			context.Background(),
			containerLookupTimeout,
		)
		defer exitCancel()

		// Update current Watchtower container's restart policy to "no" to prevent unwanted restarts
		client.SetNoRestartPolicy(exitCtx, currentWatchtowerContainer)

		logrus.Exit(0)
	}

	// Set up the notification client from loaded process config (appCfg.Notify).
	notifier = notifications.NewNotifier(appCfg.Notify)
	notifier.AddLogHook()

	// Log deprecated notification configuration options, if set.
	notifications.LogLegacyDeprecationWarnings(appCfg.Notify.LegacyTypes)
}

// run executes the main Watchtower logic based on parsed command-line flags.
//
// It reloads process configuration with positional container names, derives the effective
// operational scope (including scope persistence across self-updates), handles health-check
// early exit, builds the HTTP API RunConfig from appCfg, and delegates to runMain for core
// execution, exiting with a status code based on the outcome (0 for success, non-zero for failure).
//
// This function bridges configuration loading and the application's primary workflow.
//
// Parameters:
//   - command: The cobra.Command instance being executed, providing access to parsed flags.
//   - args: A slice of container names provided as positional arguments, used for filtering.
func run(command *cobra.Command, args []string) {
	logrus.WithField("positional_args", args).
		Debug("Received positional arguments for container filtering")

	// Reload configuration with positional names so the filter includes them.
	loaded, err := appConfig.Load(command, args)
	if err != nil {
		if currentWatchtowerContainer != nil {
			setNoRestartPolicyCtx, cancel := context.WithTimeout(
				context.Background(),
				restartPolicyTimeout,
			)
			defer cancel()

			client.SetNoRestartPolicy(setNoRestartPolicyCtx, currentWatchtowerContainer)
		}

		logrus.WithError(err).Fatal("Failed to load configuration")
	}

	appCfg = loaded

	normalizedContainerNames := append([]string(nil), appCfg.Filter.Names...)

	// Prefer explicit scope, then scope derived from the container label (self-update persistence).
	effectiveScope, scopeErr := container.GetEffectiveScope(
		currentWatchtowerContainer,
		appCfg.Filter.Scope,
	)
	if scopeErr != nil {
		logrus.WithError(scopeErr).Debug("Scope derivation failed, continuing with current scope")
	} else if effectiveScope != appCfg.Filter.Scope {
		appCfg.Filter.Scope = effectiveScope

		// Rebuild the filter predicate with the effective scope.
		predicate, desc, filterErr := filters.BuildFilter(
			appCfg.Filter.Names,
			appCfg.Filter.DisableContainers,
			appCfg.Filter.MonitorImageNames,
			appCfg.Filter.SkipImageNames,
			appCfg.Filter.EnableContainersByLabel,
			appCfg.Filter.DisableContainersByLabel,
			appCfg.Filter.LabelEnable,
			appCfg.Filter.Scope,
		)
		if filterErr != nil {
			if currentWatchtowerContainer != nil {
				setNoRestartPolicyCtx, cancel := context.WithTimeout(
					context.Background(),
					restartPolicyTimeout,
				)
				defer cancel()

				client.SetNoRestartPolicy(setNoRestartPolicyCtx, currentWatchtowerContainer)
			}

			logrus.WithError(filterErr).Fatal("Failed to build container filter")
		}

		appCfg.Filter.Predicate = predicate
		appCfg.Filter.Desc = desc
	}

	if appCfg.Mode.HealthCheck {
		if os.Getpid() == 1 {
			time.Sleep(1 * time.Second)
			logrus.Fatal(
				"The health check flag should never be passed to the main watchtower container process",
			)
		}

		return
	}

	cfg, err := appCfg.BuildRunConfig(appConfig.RunConfigInput{
		Command: command,
		Names:   normalizedContainerNames,
	})
	if err != nil {
		logrus.WithError(err).Fatal("Failed to build run configuration")
	}

	// Warn if HTTP API configuration options are set without an endpoint enabled.
	if !appConfig.HTTPAPIEndpointsEnabled(cfg) && appConfig.AnyHTTPAPIConfig(cfg) {
		logrus.Warn(
			"HTTP API configuration options are set, but no endpoints are enabled.",
		)
	}

	// Execute core logic and exit with the returned status code (0 for success, 1 for failure).
	if exitCode := runMain(cfg); exitCode != 0 {
		logrus.WithField("exit_code", exitCode).Debug("Exiting with non-zero status")
		logrus.Exit(exitCode)
	}
}

// runMain contains the core Watchtower logic after early exits are handled.
//
// It validates rolling-restart compatibility, performs one-time updates when run-once is set,
// cleans up excess Watchtower instances, sets up the HTTP API when endpoints are enabled,
// and schedules periodic updates while managing context and concurrency for graceful shutdown.
// Update policy is taken from appCfg.UpdateParams so run-once, schedule, and API paths share
// a complete types.UpdateParams snapshot.
//
// Parameters:
//   - cfg: The RunConfig struct containing filter, API, and mode parameters for this execution.
//
// Returns:
//   - int: An exit code (0 for success, 1 for failure) used to terminate the program.
func runMain(cfg types.RunConfig) int {
	// Log the container names being processed for debugging visibility.
	logrus.WithField("container_names", cfg.Names).Debug("Processing specified containers")

	// Validate flag compatibility to prevent conflicting operational modes.
	if appCfg.Update.RollingRestart && appCfg.Update.MonitorOnly {
		setNoRestartPolicyCtx, cancel := context.WithTimeout(
			context.Background(),
			restartPolicyTimeout,
		)
		defer cancel()

		client.SetNoRestartPolicy(
			setNoRestartPolicyCtx,
			currentWatchtowerContainer,
		)

		logrus.WithFields(logrus.Fields{
			"rolling_restart": appCfg.Update.RollingRestart,
			"monitor_only":    appCfg.Update.MonitorOnly,
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
		update := params
		if filter != nil {
			update.Filter = filter
		}

		if update.CurrentContainerID == "" {
			update.CurrentContainerID = currentWatchtowerContainerID
		}

		return actions.RunUpdatesWithNotifications(ctx, actions.RunUpdatesWithNotificationsParams{
			Client:                       client,
			Notifier:                     notifier,
			NotificationSplitByContainer: appCfg.Notify.SplitByContainer,
			NotificationReport:           appCfg.Notify.Report,
			EventBroadcaster:             eventsBroadcaster,
			Update:                       update,
		})
	}

	// Create a context that is automatically canceled on SIGINT/SIGTERM signals,
	// enabling graceful shutdown of the API, scheduler, and validation operations.
	// The stop function is returned but not needed as the context automatically
	// handles cleanup when the program exits.
	ctx, stop := createSignalContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	// If rolling restarts are enabled, validate that the containers being monitored for
	// updates do not have linked dependencies.
	if appCfg.Update.RollingRestart {
		err := actions.ValidateRollingRestartDependencies(
			ctx,
			client,
			cfg.Filter,
			appCfg.Update.UseComposeDependsOn,
		)
		if err != nil {
			logNotify("Rolling restart compatibility validation failed", err)

			// Update current Watchtower container's restart policy to "no" to prevent unwanted restarts
			setNoRestartPolicyCtx, cancel := context.WithTimeout(
				context.Background(),
				restartPolicyTimeout,
			)
			defer cancel()

			client.SetNoRestartPolicy(setNoRestartPolicyCtx, currentWatchtowerContainer)

			return 1 // Exit immediately after logging failure
		}
	}

	// Initialize a lock channel to prevent concurrent updates.
	updateLock := make(chan bool, 1)
	updateLock <- true

	baseParams := appCfg.UpdateParams(appConfig.RunOverrides{
		Filter:             cfg.Filter,
		CurrentContainerID: currentWatchtowerContainerID,
	})

	// Handle one-time update mode, executing updates and registering metrics.
	if cfg.RunOnce {
		// Write startup message from resolved config (no CLI flag reads).
		startup := appCfg.StartupParams(cfg)
		startup.Sched = time.Time{}
		startup.Filtering = cfg.FilterDesc
		startup.Scope = appCfg.Filter.Scope
		startup.Client = client
		startup.Notifier = notifier
		startup.Version = meta.Version
		logging.WriteStartupMessage(startup)

		params := baseParams
		params.RunOnce = true

		metric := runUpdatesWithNotifications(ctx, cfg.Filter, params)
		metrics.Default().RegisterScan(metric)
		notifier.Close()

		// Update current Watchtower container's restart policy to "no" to prevent unwanted restarts.
		setNoRestartPolicyCtx, cancel := context.WithTimeout(
			context.Background(),
			restartPolicyTimeout,
		)
		defer cancel()

		client.SetNoRestartPolicy(setNoRestartPolicyCtx, currentWatchtowerContainer)

		return 0 // Exit after successful execution.
	}

	// Retrieve the current Watchtower container for cleanup operations.
	if currentWatchtowerContainer == nil && currentWatchtowerContainerID != "" {
		logrus.Warn("Current container not cached for cleanup")
	}

	// Check for and cleanup old Watchtower containers within scope.
	totalRemovedInstances, err := actions.RemoveExcessWatchtowerInstances(
		ctx,
		client,
		appCfg.Update.Cleanup,
		appCfg.Filter.Scope,
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

	// Track whether cleanup occurred to prevent redundant updates after self-update.
	cleanupOccurred := totalRemovedInstances > 0
	// Disable update-on-start if cleanup occurred to prevent redundant updates after self-update.
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
		!appCfg.Update.EphemeralSelfUpdate
	if skipSelfUpdate {
		logrus.Warn("Published port detected - self-updates disabled.")
	}

	// One UpdateParams snapshot for HTTP API and schedule paths.
	sharedBase := baseParams
	if skipSelfUpdate {
		sharedBase.SkipSelfUpdate = true
	}

	// Startup messaging snapshot. Sched/UpdateOnStart are filled by schedule/API callers;
	// populate the rest here so scheduling does not re-derive them from scalar deps.
	startupBase := appCfg.StartupParams(cfg)
	startupBase.Filtering = cfg.FilterDesc
	startupBase.Scope = appCfg.Filter.Scope
	startupBase.Client = client
	startupBase.Notifier = notifier
	startupBase.Version = meta.Version

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
			FilterDesc:                   cfg.FilterDesc,
			UpdateLock:                   updateLock,
			BaseParams:                   sharedBase,
			IncludeStopped:               appCfg.Client.IncludeStopped,
			IncludeRestarting:            appCfg.Client.IncludeRestarting,
			LabelEnable:                  appCfg.Filter.LabelEnable,
			Client:                       client,
			Notifier:                     notifier,
			NotificationSplitByContainer: appCfg.Notify.SplitByContainer,
			Scope:                        appCfg.Filter.Scope,
			Version:                      meta.Version,
			Startup:                      startupBase,
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

		// Update current Watchtower container's restart policy to "no" to prevent unwanted restarts
		setNoRestartPolicyCtx, cancel := context.WithTimeout(
			context.Background(),
			restartPolicyTimeout,
		)
		defer cancel()

		client.SetNoRestartPolicy(setNoRestartPolicyCtx, currentWatchtowerContainer)

		return 1 // Exit while indicating failure.
	}

	// Schedule and execute periodic updates, handling errors or shutdown.
	// The startup message is skipped here if it was already sent by the HTTP API in blocking mode.
	startupMessageSent := cfg.EnableUpdateAPI && !cfg.UnblockHTTPAPI

	err = scheduling.RunUpgradesOnSchedule(ctx, scheduling.ScheduleDeps{
		Filter:                     cfg.Filter,
		FilterDesc:                 cfg.FilterDesc,
		Lock:                       updateLock,
		ScheduleSpec:               appCfg.Schedule.Spec,
		Startup:                    startupBase,
		WriteStartupMessage:        logging.WriteStartupMessage,
		RunUpdate:                  runUpdatesWithNotifications,
		Client:                     client,
		Scope:                      appCfg.Filter.Scope,
		Notifier:                   notifier,
		MetaVersion:                meta.Version,
		UpdateOnStart:              cfg.UpdateOnStart,
		SkipFirstRun:               cleanupOccurred,
		CurrentWatchtowerContainer: currentWatchtowerContainer,
		StartupMessageSent:         startupMessageSent,
		BaseParams:                 sharedBase,
	})
	if err != nil {
		logNotify("Scheduled upgrades failed", err)

		// Update current Watchtower container's restart policy to "no" to prevent unwanted restarts
		setNoRestartPolicyCtx, cancel := context.WithTimeout(
			context.Background(),
			restartPolicyTimeout,
		)
		defer cancel()

		client.SetNoRestartPolicy(setNoRestartPolicyCtx, currentWatchtowerContainer)

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

// resolveCurrentWatchtowerContainerForFallback resolves the current Watchtower container
// for use in the orchestrator fallback path.
//
// It attempts to detect the current container ID and retrieve the container object,
// returning nil if any step fails.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - c: Container client for Docker API operations.
//
// Returns:
//   - types.Container: The resolved Watchtower container, or nil if detection fails.
func resolveCurrentWatchtowerContainerForFallback(ctx context.Context, c container.Client) types.Container {
	lookupCtx, cancel := context.WithTimeout(ctx, containerLookupTimeout)
	defer cancel()

	containerID, err := container.GetCurrentContainerID(lookupCtx, c)
	if err == nil && containerID != "" {
		resolvedContainer, _ := c.GetCurrentWatchtowerContainer(lookupCtx, containerID)

		return resolvedContainer
	}

	return nil
}
