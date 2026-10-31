package cmd

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/api"
	"github.com/nicholas-fedor/watchtower/internal/api/routes"
	"github.com/nicholas-fedor/watchtower/internal/container"
	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/internal/meta"
	"github.com/nicholas-fedor/watchtower/internal/observability/events"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	"github.com/nicholas-fedor/watchtower/internal/settings"
	"github.com/nicholas-fedor/watchtower/internal/update"
	"github.com/nicholas-fedor/watchtower/internal/update/cleanup"
	"github.com/nicholas-fedor/watchtower/internal/update/schedule"
	"github.com/nicholas-fedor/watchtower/internal/update/selfupdate"
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
	// metadata is unreachable. The inspect of the container afterwards bounds
	// its own retries. In a health check, this timeout bounds them as well.
	containerLookupTimeout = 5 * time.Second
)

// errSupersededInstance ends a Watchtower instance that runs in an old
// container, which a newer instance has replaced.
var errSupersededInstance = errors.New("superseded Watchtower instance")

var (
	// appCfg is the resolved process configuration from settings.Load.
	//
	// It is the single source of operational policy for run-once, schedule, and API paths.
	// Values originate from CLI flags and environment variables registered in internal/settings
	// and are resolved once at startup (and again in run when positional container names apply).
	appCfg settings.Config

	// client is the Docker client instance used to interact with container operations in Watchtower.
	//
	// It provides an interface for listing, stopping, starting, and managing containers, initialized during
	// the preRun phase with options derived from appCfg.ClientOptions() (DOCKER_HOST, TLS, API version, and
	// related client flags/environment variables).
	client containerClient.Client

	// rateLimiter paces every registry request, so image pulls, digest checks,
	// image age lookups, and release tag lookups share one budget per registry
	// host. It is created in preRun with client.
	rateLimiter *ratelimit.Limiter

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

	// currentWatchtowerContainerUnknown is true when Watchtower runs in a container
	// whose ID could not be determined. Self-updates are then disabled.
	currentWatchtowerContainerUnknown bool

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

	// newClient is a function variable for creating the Docker client, allowing it to be overridden in tests.
	//
	// It is initialized to container.NewClient by default. preRun calls it with the resolved client options,
	// so tests can substitute a mock client without a Docker daemon.
	newClient = containerClient.NewClient

	// processFs is the filesystem holding the container marker files and the
	// /proc files that self container ID detection reads.
	//
	// Tests replace it to control whether Watchtower appears to run in a
	// container and which container ID it detects.
	processFs fs.FS = os.DirFS("/")

	// runUpdatesWithNotifications is a function variable for performing container updates and sending notifications.
	//
	// It is initialized inside runMain with a closure that executes update.RunUpdatesWithNotifications,
	// allowing it to be overridden in tests to mock the update process. It takes a context, filter, and update params,
	// and returns a metric summarizing the update session.
	runUpdatesWithNotifications func(context.Context, types.Filter, types.UpdateParams) *metrics.Metric

	// rootCmd represents the root command for the Watchtower CLI, serving as the entry point for all subcommands.
	//
	// It defines the base usage string and the short and long descriptions. Execute assigns its lifecycle hooks
	// (PreRunE and RunE).
	rootCmd = NewRootCommand()
)

// process holds composition-root dependencies for a single CLI invocation.
//
// The logger is constructed in main and reconfigured in preRun after flags are
// parsed. It is never stored as a package-level global.
type process struct {
	log *zerolog.Logger
}

// exitError is an error that ends the process. Execute reports it and exits
// with its code.
type exitError struct {
	// err is the cause of the exit.
	err error
	// msg is the static message Execute logs at fatal level with the cause.
	// Empty means the failure was already reported, so Execute only exits.
	msg string
	// code is the process exit code for a failure that was already reported.
	code int
}

// Error returns the message followed by the cause. An empty message or a nil
// cause is left out.
//
// Returns:
//   - string: The message and the cause.
func (e *exitError) Error() string {
	switch {
	case e.msg == "":
		return e.err.Error()
	case e.err == nil:
		return e.msg
	default:
		return e.msg + ": " + e.err.Error()
	}
}

// Unwrap returns the cause.
//
// Returns:
//   - error: The cause of the exit.
func (e *exitError) Unwrap() error {
	return e.err
}

// init registers command-line flags for the root command during package initialization.
//
// It registers every domain's flags, such as Docker configuration (e.g., --host), system behavior
// (e.g., --interval), and notifications (e.g., --notifications), establishing the CLI's configurable
// parameters before execution begins.
func init() {
	settings.RegisterAll(rootCmd)
}

// NewRootCommand creates and configures the root command for the Watchtower CLI.
//
// It establishes the base usage string ("watchtower"), a short description summarizing its purpose,
// and a long description with additional context and a project URL.
//
// PreRunE and RunE are wired in Execute with a logger-capturing process instance so the
// composition root can pass *zerolog.Logger without a package-level global.
//
// Returns:
//   - *cobra.Command: A pointer to the fully configured root command, ready for flag registration and execution.
func NewRootCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "watchtower",
		Short: "Automatically updates running Docker containers",
		Long:  "\nWatchtower automatically updates running Docker containers whenever a new image is released.\nMore information available at https://github.com/nicholas-fedor/watchtower/.",
		Args:  cobra.ArbitraryArgs, // Permits any number of positional arguments, processed as container names later.
	}
}

// Execute runs the root command and manages any errors encountered during its execution.
//
// It serves as the primary entry point for the Watchtower CLI, called from main.go, and ensures that any
// fatal errors are logged and terminate the program with an appropriate exit status, providing a clean
// interface between the CLI and the operating system.
//
// Parameters:
//   - log: Process logger from main (reconfigured in preRun from --log-format / --log-level).
func Execute(log *zerolog.Logger) {
	proc := &process{log: log}
	rootCmd.PreRunE = proc.preRun
	rootCmd.RunE = proc.run
	notifyUpgradeCmd.RunE = proc.runNotifyUpgrade

	proc.exit(rootCmd.Execute())
}

// exit ends the process for an error that a command returned. It does
// nothing for nil.
//
// An [exitError] with a message is logged at fatal level with its cause. One
// without a message ends the process with its exit code. Any other error, such
// as a flag that does not parse, is logged as a failed command. Fatal logs exit
// with code 1. Logs use the logger the command configured from its flags.
//
// Parameters:
//   - err: The error the command returned.
func (p *process) exit(err error) {
	if err == nil {
		return
	}

	exitErr, ok := errors.AsType[*exitError](err)

	switch {
	case !ok:
		p.log.Fatal().Err(err).Msg("Failed to execute root command")
	case exitErr.msg != "":
		p.log.Fatal().Err(exitErr.err).Msg(exitErr.msg)
	default:
		if exitErr.code != 0 {
			p.log.Debug().
				Int("exit_code", exitErr.code).
				Msg("Exiting with non-zero status")
		}

		os.Exit(exitErr.code)
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
//
// Returns:
//   - error: An [exitError] when Watchtower must not continue.
func (p *process) preRun(cmd *cobra.Command, _ []string) error {
	// Execute reports the errors returned from here on. Flag and argument
	// errors come earlier, so Cobra still prints them with the usage text.
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	flagsSet := cmd.PersistentFlags()

	// Bridge environment values onto unset flags so aliases and logging still see them.
	err := settings.ApplyEnvToFlags(flagsSet, settings.AllSpecs())
	if err != nil {
		return &exitError{err: err, msg: "Failed to apply environment configuration", code: 1}
	}

	// Apply format (and pre-alias level) before ProcessFlagAliases so its errors,
	// such as an interval set with a schedule, use the user-selected --log-format
	// rather than zerolog's default JSON encoding on stderr.
	p.log, err = settings.SetupLogging(p.log, flagsSet)
	if err != nil {
		return &exitError{err: err, msg: "Failed to initialize logging", code: 1}
	}

	// Apply porcelain, interval→schedule, and debug/trace log-level aliases.
	err = settings.ProcessFlagAliases(p.log, flagsSet)
	if err != nil {
		return &exitError{err: err, msg: "Invalid configuration", code: 1}
	}

	// Re-apply after aliases so debug/trace forced log-level values take effect.
	p.log, err = settings.SetupLogging(p.log, flagsSet)
	if err != nil {
		return &exitError{err: err, msg: "Failed to initialize logging", code: 1}
	}

	// Expand secrets from files (for example notification URLs and API tokens).
	err = settings.GetSecretsFromFiles(p.log, cmd)
	if err != nil {
		return &exitError{err: err, msg: "Failed to load secret from file", code: 1}
	}

	// Map Docker connection flags into the process environment for the client stack.
	err = settings.EnvConfig(p.log, cmd)
	if err != nil {
		return &exitError{err: err, msg: "Failed to configure Docker environment", code: 1}
	}

	// Load without positional names. run reloads with args for the final filter.
	appCfg, err = settings.Load(p.log, cmd, nil)
	if err != nil {
		return &exitError{err: err, msg: "Failed to load configuration", code: 1}
	}

	p.log.Debug().
		Str("scheduleSpec", appCfg.Schedule.Spec).
		Msg("Retrieved cron schedule specification from configuration")

	// Log the scope if specified, aiding debugging by confirming the operational boundary.
	if appCfg.Filter.Scope != "" {
		p.log.Debug().
			Str("scope", appCfg.Filter.Scope).
			Msg("Configured operational scope")
	}

	// Initialize the Docker client from the resolved ClientOptions projection,
	// with the rate limiter that update sessions also use for release tag lookups.
	rateLimiter = ratelimit.New()

	clientOpts := appCfg.ClientOptions()
	clientOpts.RateLimiter = rateLimiter

	client, err = newClient(p.log, clientOpts)
	if err != nil {
		return &exitError{err: err, msg: "Failed to initialize Docker client", code: 1}
	}

	// Check for orchestrator mode early. This is an internal mode where Watchtower
	// runs as a one-shot orchestrator for self-update.
	if appCfg.Mode.SelfUpdateOrchestrator {
		p.log.Info().Msg("Running in ephemeral self-update orchestrator mode")

		selfupdate.RunOrchestrator(p.log, context.Background(), client)

		lookupCtx, lookupCancel := context.WithTimeout(context.Background(), containerLookupTimeout)
		currentWatchtowerContainerID, _ = containerClient.GetCurrentContainerID(p.log, lookupCtx, processFs, client)

		lookupCancel()
		p.disableRestarts()

		p.log.Fatal().
			Str("flag", "self-update-orchestrator").
			Msg("RunOrchestrator returned unexpectedly. Exiting to prevent unintended execution")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		containerLookupTimeout,
	)
	defer cancel()

	// Retrieve and store the current container ID for use throughout the application.
	var detectErr, inspectErr error

	currentWatchtowerContainerID, detectErr = containerClient.GetCurrentContainerID(p.log, ctx, processFs, client)
	if detectErr != nil {
		p.log.Debug().Err(detectErr).Msg("Failed to get current container ID")

		currentWatchtowerContainerID = ""
	}

	// Retrieve the current Watchtower container.
	//
	// When the daemon has no container with the detected ID, Watchtower has no
	// container of its own on that daemon.
	// Detection may have picked the wrong ID, or Watchtower may manage a
	// different Docker host than the one it runs on. Either way it counts as a
	// failed detection.
	//
	// Another failed inspect keeps the detected ID, so the checks that tell
	// Watchtower apart from other instances still work.
	if currentWatchtowerContainerID != "" {
		// A health check uses none of the container's data, so its inspect
		// shares the lookup's timeout instead of waiting for every retry.
		inspectCtx := context.Background()
		if appCfg.Mode.HealthCheck {
			inspectCtx = ctx
		}

		currentWatchtowerContainer, inspectErr = client.GetCurrentWatchtowerContainer(
			inspectCtx,
			currentWatchtowerContainerID,
		)
		if cerrdefs.IsNotFound(inspectErr) {
			detectErr, inspectErr = inspectErr, nil
			currentWatchtowerContainerID = ""
		}
	}

	// Without its own ID, Watchtower cannot tell itself apart from other instances.
	// A host binary has no container ID, so only a containerized run is unknown.
	currentWatchtowerContainerUnknown = currentWatchtowerContainerID == "" &&
		containerClient.InContainer(processFs)

	// Check if this is an old Watchtower container that should not run continuously.
	// A run-once instance ends after its run, so only a continuous one exits here.
	if !appCfg.Mode.RunOnce && container.IsSuperseded(currentWatchtowerContainer) {
		p.stopInvalidWatchtowerRestart()

		return &exitError{err: errSupersededInstance, msg: "", code: 0}
	}

	// Set up the notification client from loaded process config (appCfg.Notify).
	// RegisterHook attaches the notifier to p.log and updates p.log in place to the
	// hooked logger so subsequent application logging is captured for notifications.
	notifier = notifications.NewNotifier(p.log, appCfg.Notify)
	notifier.RegisterHook(p.log)

	// Without the container's labels, a scope set only by its scope label is
	// unknown. Running without a scope would update containers that belong to
	// another Watchtower instance, so Watchtower exits instead. A health check
	// runs no updates, so it continues.
	if inspectErr != nil && appCfg.Filter.Scope == "" && !appCfg.Mode.HealthCheck {
		// Disable restarts first, so the runtime does not start Watchtower again.
		p.disableRestarts()
		p.logNotify("Could not inspect Watchtower's own container to read its scope label. "+
			"Exiting rather than running without a scope", inspectErr)

		return &exitError{err: inspectErr, msg: "", code: 1}
	}

	if currentWatchtowerContainerUnknown {
		p.log.Warn().Err(detectErr).Msg(
			"Could not identify Watchtower's own container. " +
				"Self-updates are disabled and old instances will not be removed",
		)
	} else if inspectErr != nil {
		p.log.Warn().Err(inspectErr).Msg(
			"Could not inspect Watchtower's own container. Self-updates are disabled",
		)
	}

	// Log deprecated notification configuration options, if set.
	notifications.LogLegacyDeprecationWarnings(p.log, appCfg.Notify.LegacyTypes)

	return nil
}

// stopInvalidWatchtowerRestart recovers any orphaned instance and sets the
// restart policy of the current Watchtower container to "no", so the old
// instance can exit. The invalid restart check that leads here requires a
// current container.
func (p *process) stopInvalidWatchtowerRestart() {
	p.log.Info().
		Msg("Detected invalid restart of old Watchtower container, stopping Watchtower container now")

	// Attempt to recover an orphaned Watchtower container that is stuck
	// in the created state before exiting. If recovery succeeds, the
	// old container still exits with restart policy set to "no".
	recoverCtx, recoverCancel := context.WithTimeout(
		context.Background(),
		restartPolicyTimeout,
	)

	recoveredContainer, recovered := selfupdate.TryRecoverOrphanedContainer(
		p.log,
		recoverCtx,
		client,
		currentWatchtowerContainer,
	)
	if recovered {
		p.log.Info().
			Str("container", recoveredContainer.Name()).
			Msg("Recovered orphaned Watchtower container, exiting old instance")
	}

	recoverCancel()
	p.disableRestarts()
}

// run executes the main Watchtower logic based on parsed command-line flags.
//
// It reloads process configuration with positional container names, derives the effective
// operational scope (including scope persistence across self-updates), handles health-check
// early exit, builds the HTTP API RunConfig from appCfg, and delegates to runMain for core
// execution. It bridges configuration loading and the application's primary workflow.
//
// Parameters:
//   - command: The cobra.Command instance being executed, providing access to parsed flags.
//   - args: A slice of container names provided as positional arguments, used for filtering.
//
// Returns:
//   - error: An [exitError] when Watchtower cannot continue or its run failed.
func (p *process) run(command *cobra.Command, args []string) error {
	p.log.Debug().
		Strs("positional_args", args).
		Msg("Received positional arguments for container filtering")

	// Reload configuration with positional names so the filter includes them.
	loaded, err := settings.Load(p.log, command, args)
	if err != nil {
		p.disableRestarts()

		return &exitError{err: err, msg: "Failed to load configuration", code: 1}
	}

	appCfg = loaded

	normalizedContainerNames := append([]string(nil), appCfg.Filter.Names...)

	// Prefer explicit scope, then scope derived from the container label (self-update persistence).
	effectiveScope, scopeErr := container.GetEffectiveScope(
		currentWatchtowerContainer,
		appCfg.Filter.Scope,
	)
	if scopeErr != nil {
		p.log.Debug().
			Err(scopeErr).
			Msg("Scope derivation failed, continuing with current scope")
	} else if effectiveScope != appCfg.Filter.Scope {
		appCfg.Filter.Scope = effectiveScope

		// Rebuild the filter predicate with the effective scope.
		predicate, desc, filterErr := filters.BuildFilter(
			p.log,
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
			p.disableRestarts()

			return &exitError{err: filterErr, msg: "Failed to build container filter", code: 1}
		}

		appCfg.Filter.Predicate = predicate
		appCfg.Filter.Desc = desc
	}

	if appCfg.Mode.HealthCheck {
		if os.Getpid() == 1 {
			time.Sleep(1 * time.Second)

			return &exitError{
				err:  nil,
				msg:  "The health check flag should never be passed to the main watchtower container process",
				code: 1,
			}
		}

		return nil
	}

	cfg, err := appCfg.BuildRunConfig(settings.RunConfigInput{
		Command: command,
		Names:   normalizedContainerNames,
	})
	if err != nil {
		return &exitError{err: err, msg: "Failed to build run configuration", code: 1}
	}

	// Warn if HTTP API configuration options are set without an endpoint enabled.
	if !settings.HTTPAPIEndpointsEnabled(cfg) && settings.AnyHTTPAPIConfig(cfg) {
		p.log.Warn().
			Msg("HTTP API configuration options are set, but no endpoints are enabled.")
	}

	return p.runMain(cfg)
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
//   - error: An [exitError] for a failure that was already logged and notified.
func (p *process) runMain(cfg types.RunConfig) error {
	// Log the container names being processed for debugging visibility.
	p.log.Debug().
		Strs("container_names", cfg.Names).
		Msg("Processing specified containers")

	// Give a self-updated instance a moment before it lists its predecessors.
	awaitDockerClient(p.log)

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
	gitClient := git.New(p.log, git.Options{
		Token:           appCfg.Git.Token,
		Username:        appCfg.Git.Username,
		Password:        appCfg.Git.Password,
		SSHKeyPath:      appCfg.Git.SSHKeyPath,
		SSHKnownHosts:   appCfg.Git.SSHKnownHosts,
		Timeout:         appCfg.Git.Timeout,
		CABundle:        appCfg.Git.CABundle,
		InsecureSkipTLS: appCfg.Git.InsecureSkipTLS,
	})

	ctx, stop := createSignalContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	runUpdatesWithNotifications = func(ctx context.Context, filter types.Filter, params types.UpdateParams) *metrics.Metric {
		runParams := params
		if filter != nil {
			runParams.Filter = filter
		}

		if runParams.CurrentContainerID == "" {
			runParams.CurrentContainerID = currentWatchtowerContainerID
		}

		return update.RunUpdatesWithNotifications(ctx, update.RunUpdatesWithNotificationsParams{
			Logger:                       p.log,
			Client:                       client,
			GitClient:                    gitClient,
			RateLimiter:                  rateLimiter,
			Notifier:                     notifier,
			NotificationSplitByContainer: appCfg.Notify.SplitByContainer,
			NotificationReport:           appCfg.Notify.Report,
			EventBroadcaster:             eventsBroadcaster,
			Update:                       runParams,
			// Stop the process, as a signal would, when this is an old instance.
			OnOldSelfDetected: stop,
		})
	}

	// If rolling restarts are enabled, validate that the containers being monitored for
	// updates do not have linked dependencies.
	if appCfg.Update.RollingRestart {
		err := update.ValidateRollingRestartDependencies(
			p.log,
			ctx,
			client,
			cfg.Filter,
			appCfg.Update.UseComposeDependsOn,
		)
		if err != nil {
			p.disableRestarts()
			p.logNotify("Rolling restart compatibility validation failed", err)

			return &exitError{err: err, msg: "", code: 1}
		}
	}

	// Share one update lock between the scheduler and the HTTP API, so their
	// updates never run at the same time.
	updateLock := schedule.NewUpdateLock()

	baseParams := appCfg.UpdateParams(settings.RunOverrides{
		Filter:             cfg.Filter,
		SkipSelfUpdate:     currentWatchtowerContainerUnknown,
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
		startup.Logger = p.log
		logging.WriteStartupMessage(startup)

		params := baseParams
		params.RunOnce = true

		metric := runUpdatesWithNotifications(ctx, cfg.Filter, params)
		metrics.Default().RegisterScan(metric)
		p.disableRestarts()
		notifier.Close()

		return nil
	}

	// Check for and cleanup old Watchtower containers within scope.
	totalRemovedInstances, err := cleanup.RemoveExcessWatchtowerInstances(
		p.log,
		ctx,
		client,
		appCfg.Update.Cleanup,
		appCfg.Filter.Scope,
		nil, // Remove the images of stopped instances right away.
		currentWatchtowerContainer,
	)
	if err != nil {
		// Cleanup failure is non-fatal — log a warning and continue.
		// The old container may still be stopping. Forcing exit would leave
		// no Watchtower running. Continuing ensures the new instance operates
		// even if the old container could not be fully cleaned up.
		p.log.Warn().
			Err(err).
			Msg("Failed to clean up old Watchtower containers, continuing anyway")
	}

	// Check for and cleanup orphaned ephemeral orchestrator containers.
	// These may persist if the orchestrator crashed or was killed unexpectedly.
	// With AutoRemove: true, this is a safety net for edge cases.
	removedOrchestratorCount, orchestratorErr := cleanup.RemoveOrphanedOrchestrators(p.log, ctx, client)
	if orchestratorErr != nil {
		p.log.Warn().
			Err(orchestratorErr).
			Int("removed_orchestrators", removedOrchestratorCount).
			Msg("Failed to clean up orphaned orchestrator containers, continuing anyway")
	} else if removedOrchestratorCount > 0 {
		p.log.Debug().
			Int("removed_orchestrators", removedOrchestratorCount).
			Msg("Cleaned up orphaned orchestrator containers")
	}

	// Track whether cleanup occurred to prevent redundant updates after self-update.
	cleanupOccurred := totalRemovedInstances > 0
	// Disable update-on-start if cleanup occurred to prevent redundant updates after self-update.
	if cleanupOccurred {
		cfg.UpdateOnStart = false

		p.log.Debug().Msg("Disabled update-on-start due to cleanup of old Watchtower containers")
	}

	// Skip self-updates when the running container cannot be replaced safely.
	// A container that could not be inspected was already reported at startup.
	//
	// Perform this check here rather than inside SetupAndStartAPI so the
	// published port warning always appears, even when no HTTP API endpoints
	// are enabled and SetupAndStartAPI returns early.
	selfUpdateBlock := selfupdate.Blocked(currentWatchtowerContainer,
		currentWatchtowerContainerID,
		appCfg.Update.EphemeralSelfUpdate,
	)
	if selfUpdateBlock == selfupdate.BlockedByPublishedPorts {
		p.log.Warn().Msg("Published port detected - self-updates disabled.")
	}

	// One UpdateParams snapshot for HTTP API and schedule paths.
	sharedBase := baseParams
	if selfUpdateBlock != selfupdate.NotBlocked {
		sharedBase.SkipSelfUpdate = true
	}

	// Startup messaging snapshot. Sched and UpdateOnStart are filled by schedule or API
	// callers. Populate the rest here so scheduling does not re-derive them from scalar deps.
	startupBase := appCfg.StartupParams(cfg)
	startupBase.Filtering = cfg.FilterDesc
	startupBase.Scope = appCfg.Filter.Scope
	startupBase.Client = client
	startupBase.Notifier = notifier
	startupBase.Version = meta.Version
	startupBase.Logger = p.log

	// API request/auth logs must not fan into notification hooks.
	apiLog := p.log.With().Str("notify", "no").Logger()

	apiStopped, err := api.SetupAndStartAPI(
		ctx,
		routes.Options{
			Logger:                       &apiLog,
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
			GitClient:                    gitClient,
			Notifier:                     notifier,
			NotificationSplitByContainer: appCfg.Notify.SplitByContainer,
			Scope:                        appCfg.Filter.Scope,
			Version:                      meta.Version,
			Startup:                      startupBase,
			RunUpdatesWithNotifications:  runUpdatesWithNotifications,
			FilterByImage: func(images []string, base types.Filter) types.Filter {
				return filters.FilterByImage(p.log, images, base)
			},
			DefaultMetrics:      metrics.Default,
			WriteStartupMessage: logging.WriteStartupMessage,
			EventBroadcaster:    eventsBroadcaster,
			OnUnexpectedServerStop: func(listenErr error) {
				p.log.Error().
					Err(listenErr).
					Msg("Canceling process context after unexpected HTTP server stop")
				stop()
			},
		},
	)
	if err != nil {
		p.disableRestarts()
		p.logNotify("API setup failed", err)

		return &exitError{err: err, msg: "", code: 1}
	}

	// Schedule and execute periodic updates, handling errors or shutdown.
	// The startup message is skipped here if it was already sent by the HTTP API in blocking mode.
	startupMessageSent := cfg.EnableUpdateAPI && !cfg.UnblockHTTPAPI

	err = schedule.RunUpgradesOnSchedule(ctx, schedule.ScheduleDeps{
		Logger:              p.log,
		Filter:              cfg.Filter,
		Lock:                updateLock,
		ScheduleSpec:        appCfg.Schedule.Spec,
		Startup:             startupBase,
		WriteStartupMessage: logging.WriteStartupMessage,
		RunUpdate:           runUpdatesWithNotifications,
		Notifier:            notifier,
		UpdateOnStart:       cfg.UpdateOnStart,
		SkipFirstRun:        cleanupOccurred,
		StartupMessageSent:  startupMessageSent,
		BaseParams:          sharedBase,
	})

	// Stop the HTTP API and give it its shutdown grace period to finish the
	// requests it is serving before the process exits.
	stop()
	<-apiStopped

	if err != nil {
		p.disableRestarts()
		p.logNotify("Scheduled upgrades failed", err)

		return &exitError{err: err, msg: "", code: 1}
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
func (p *process) logNotify(msg string, err error) {
	if msg == "" {
		msg = "Operation failed"
	}

	p.log.Error().Err(err).Msg(msg)
	notifier.StartNotification(false)
	notifier.SendNotification(nil)
	notifier.Close()
}

// disableRestarts sets the restart policy of Watchtower's own container to
// "no", so the runtime does not start Watchtower again after the process ends.
// It does nothing when Watchtower does not run in a container, or when the
// policy of its inspected container is already "no".
//
// A failure is logged as an error, because the runtime may then start
// Watchtower again. Exit paths call it before they report the exit, so a
// notification that is still collecting entries for the exit includes it.
func (p *process) disableRestarts() {
	if currentWatchtowerContainer != nil && !currentWatchtowerContainer.RestartsAutomatically() {
		return
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		restartPolicyTimeout,
	)
	defer cancel()

	err := client.SetRestartPolicy(
		ctx,
		currentWatchtowerContainerID,
		dockerContainer.RestartPolicy{Name: dockerContainer.RestartPolicyDisabled},
	)
	if err != nil {
		p.log.Error().Err(err).
			Msg("Could not disable Watchtower's restart policy. The runtime may start it again")
	}
}

// awaitDockerClient pauses for one second before Watchtower first lists containers.
//
// It guards against a self-update race in which the new instance's first listing of Watchtower
// containers can come back empty, which leaves the old instance running. Waiting for a daemon
// that is not serving its API yet happens earlier, when the client is created.
func awaitDockerClient(log *zerolog.Logger) {
	log.Debug().
		Msg("Sleeping for a second to ensure the docker api client has been properly initialized.")
	sleepFunc(1 * time.Second)
}
