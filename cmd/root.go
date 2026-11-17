package cmd

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/api"
	"github.com/nicholas-fedor/watchtower/internal/api/routes"
	composeApply "github.com/nicholas-fedor/watchtower/internal/compose/apply"
	"github.com/nicholas-fedor/watchtower/internal/container"
	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/internal/meta"
	"github.com/nicholas-fedor/watchtower/internal/observability/events"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	"github.com/nicholas-fedor/watchtower/internal/settings"
	dockerConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/docker"
	"github.com/nicholas-fedor/watchtower/internal/update"
	"github.com/nicholas-fedor/watchtower/internal/update/cleanup"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/schedule"
	"github.com/nicholas-fedor/watchtower/internal/update/selfupdate"
)

const (
	// restartPolicyTimeout is the maximum duration allowed for restart-policy
	// update operations.
	//
	// It bounds the Docker API call that sets the current Watchtower
	// container's restart policy to "no" and each step of retiring an old
	// instance. A slow or unresponsive daemon then cannot delay shutdown paths.
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

// Errors for ending Watchtower.
var (
	// errSupersededInstance ends a Watchtower instance that runs in an old
	// container, which a newer instance has replaced.
	errSupersededInstance = errors.New("superseded Watchtower instance")
	// errUnknownContainerID indicates that Watchtower does not know the ID of
	// its own container, so it cannot change the container's restart policy.
	errUnknownContainerID = errors.New("own container ID is not known")
)

var (
	// appCfg is the resolved process configuration from settings.Load.
	//
	// It is the single source of operational policy for run-once, schedule, and API paths.
	// Values originate from CLI flags and environment variables registered in internal/settings
	// and are loaded once at startup, in preRun. preRun then takes the scope from Watchtower's
	// own scope label when no scope is set.
	appCfg settings.Config

	// client is the Docker client instance used to interact with container operations in Watchtower.
	//
	// It provides an interface for listing, stopping, starting, and managing containers, initialized during
	// the preRun phase with options derived from appCfg.ClientOptions(), which include the Docker
	// connection settings.
	client DockerClient

	// rateLimiter paces every registry request, so image pulls, digest checks,
	// image age lookups, and release tag lookups share one budget per registry
	// host. It is created in preRun with client.
	rateLimiter *ratelimit.Limiter

	// notifier is the notification system instance responsible for sending update status messages to configured channels.
	//
	// It is initialized in preRun from appCfg.Notify via notify.NewNotifier, supporting
	// Shoutrrr URLs and (deprecated) legacy types such as email, Slack, or MSTeams.
	// A health check and a subcommand leave it unset. Execute closes it before
	// the process ends.
	notifier *notify.Notifier

	// currentWatchtowerContainerID stores the current Watchtower container ID.
	//
	// It is initialized once in preRun after the client is set up, and used throughout the application
	// to avoid repeated calls to GetCurrentContainerID. If retrieval fails, it is set to an empty string.
	currentWatchtowerContainerID container.ContainerID

	// currentWatchtowerContainerUnknown is true when Watchtower runs in a container
	// whose ID could not be determined. Self-updates are then disabled.
	currentWatchtowerContainerUnknown bool

	// currentWatchtowerContainer holds the current Watchtower container instance.
	//
	// It is initialized in preRun by retrieving the container object using the currentWatchtowerContainerID,
	// remains nil if retrieval fails or yields an unexpected type, and is used for operations like updating
	// restart policy, validating restarts, and cleaning up excess instances.
	currentWatchtowerContainer *container.Container

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

	// newClient creates the Docker client, allowing it to be overridden in tests.
	//
	// It creates the client with containerClient.NewClient. preRun calls it with
	// the resolved client options, so tests can substitute a mock client without
	// a Docker daemon.
	newClient = func(ctx context.Context, log *zerolog.Logger, opts containerClient.ClientOptions) (DockerClient, error) {
		dockerClient, err := containerClient.NewClient(ctx, log, opts)
		if err != nil {
			// Return a nil interface, which callers compare with nil, rather
			// than one holding a nil pointer.
			return nil, err //nolint:wrapcheck // NewClient's error already names the failed step.
		}

		return dockerClient, nil
	}

	// processFs is the filesystem holding the container marker files and the
	// /proc files that self container ID detection and main process detection
	// read.
	//
	// Tests replace it to control whether Watchtower appears to run in a
	// container and which container ID it detects.
	processFs fs.FS = os.DirFS("/")

	// processID returns the ID of the Watchtower process. Tests replace it to
	// run as the main process of a container.
	processID = os.Getpid

	// parentProcessID returns the ID of the Watchtower process's parent. Tests
	// replace it to run under a container's init process.
	parentProcessID = os.Getppid

	// runUpdatesWithNotifications is a function variable for performing container updates and sending notifications.
	//
	// It is initialized inside runMain with a closure that executes update.RunUpdatesWithNotifications,
	// allowing it to be overridden in tests to mock the update process. It takes a context, filter, and update params,
	// and returns a metric summarizing the update session.
	runUpdatesWithNotifications func(context.Context, filters.Filter, updateParams.Params) *metrics.Metric

	// rootCmd represents the root command for the Watchtower CLI, serving as the entry point for all subcommands.
	//
	// It defines the base usage string and the short and long descriptions. Execute assigns its lifecycle hooks
	// (PreRunE and RunE).
	rootCmd = NewRootCommand()
)

// DockerClient is what the commands need from the Docker client. It covers what
// the update session, the HTTP API and the self-update handoff use. It also
// looks up Watchtower's own container.
type DockerClient interface {
	update.Client
	routes.Client
	selfupdate.HandoffClient

	// GetCurrentWatchtowerContainer returns the container with the given ID
	// without inspecting its image.
	//
	// A transient inspect failure is tried again, up to three attempts 2
	// seconds apart, or until ctx ends.
	GetCurrentWatchtowerContainer(ctx context.Context, containerID container.ContainerID) (*container.Container, error)
}

// process holds composition-root dependencies for a single CLI invocation.
//
// The logger is constructed in main and reconfigured in preRun after flags are
// parsed. It is never stored as a package-level global.
type process struct {
	// log is the process logger.
	log *zerolog.Logger
	// clientErr is the error from creating the Docker client at startup. An
	// exit reports it instead of waiting for the daemon again.
	clientErr error
}

// exitError is an error that ends the process. Execute reports it and exits
// with its code.
type exitError struct {
	// err is the cause of the exit.
	err error
	// msg is the static message Execute logs at fatal level with the cause.
	// Execute logs nothing when it is empty, since the failure was already
	// logged.
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

	proc.exit(rootCmd.ExecuteC())
}

// exit closes the notifier and ends the process when a command returns an
// error.
//
// For an [exitError] without a message, which was already reported, the
// process only exits with its code. Any other error is logged at fatal level
// with the logger the command configured before the process exits with code 1.
// Because a root command failure would happen again on every start,
// Watchtower's restarts are disabled before that line is logged.
//
// Closing the notifier sends every line it still holds, the fatal line
// included. Lines logged after the close are dropped, such as those of an HTTP
// API request that outlasts its 5-second shutdown grace period. A startup that
// fails before the notifier is set up sends no notification. That covers a
// configuration that does not load and a Docker daemon that cannot be reached.
// A command that succeeds returns after the close.
//
// Parameters:
//   - cmd: The command that ran.
//   - err: The error the command returned.
func (p *process) exit(cmd *cobra.Command, err error) {
	code := 0

	exitErr, ok := errors.AsType[*exitError](err)

	switch {
	case err == nil:
	case ok && exitErr.msg == "":
		code = exitErr.code
		if code != 0 {
			p.log.Debug().
				Int("exit_code", code).
				Msg("Exiting with non-zero status")
		}
	default:
		msg, cause := "Failed to execute root command", err
		if ok {
			msg, cause = exitErr.msg, exitErr.err
		}

		p.disableRestartsBeforeExit(cmd)

		// Fatal would exit inside Msg, before the notifier could send the line.
		p.log.WithLevel(zerolog.FatalLevel).Err(cause).Msg(msg)

		code = 1
	}

	if notifier != nil {
		notifier.Close()
	}

	if err != nil {
		os.Exit(code)
	}
}

// preRun prepares the environment and configuration before the main command execution begins.
//
// It runs these steps in order:
//  1. Loads the configuration with [settings.Load], which also sets up logging.
//  2. Maps the Docker flags into the process environment.
//  3. Creates the Docker client and finds Watchtower's own container.
//  4. Ends the process for the ephemeral self-update orchestrator and for an old container
//     restarted after a self-update replaced it.
//  5. Sets up notifications.
//  6. Takes the scope from Watchtower's own scope label when no scope is set. Exits when
//     that label is needed but cannot be read.
//  7. Warns when Watchtower cannot identify or inspect its own container.
//  8. Warns about deprecated notification settings.
//
// Parameters:
//   - cmd: The cobra.Command instance being executed, providing access to parsed flags.
//   - args: Container names given as positional arguments, which limit the containers
//     Watchtower monitors.
//
// Returns:
//   - error: An [exitError] when Watchtower must not continue.
func (p *process) preRun(cmd *cobra.Command, args []string) error {
	// Execute reports the errors returned from here on. Flag and argument
	// errors come earlier, so Cobra still prints them with the usage text.
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	var err error

	appCfg, p.log, err = settings.Load(p.log, cmd, args)
	if err != nil {
		return &exitError{err: err, msg: "Failed to load configuration", code: 1}
	}

	// The build tools that Compose rebuilds start read the Docker connection from the process
	// environment.
	err = composeApply.ExportConnection(appCfg.Docker)
	if err != nil {
		return &exitError{err: err, msg: "Failed to configure Docker environment", code: 1}
	}

	p.log.Debug().
		Str("scheduleSpec", appCfg.Schedule.Spec).
		Msg("Retrieved cron schedule specification from configuration")

	// Initialize the Docker client from the resolved ClientOptions projection,
	// with the rate limiter that update sessions also use for release tag lookups.
	rateLimiter = ratelimit.New()

	clientOpts := appCfg.ClientOptions()
	clientOpts.RateLimiter = rateLimiter

	client, err = newClient(context.Background(), p.log, clientOpts)
	if err != nil {
		p.clientErr = err

		return &exitError{err: err, msg: "Failed to initialize Docker client", code: 1}
	}

	// Check for orchestrator mode early. This is an internal mode where Watchtower
	// runs as a one-shot orchestrator for self-update.
	if appCfg.Mode.SelfUpdateOrchestrator {
		p.log.Info().Msg("Running in ephemeral self-update orchestrator mode")

		selfupdate.RunOrchestrator(p.log, context.Background(), client)

		_ = p.detectCurrentContainerID()
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

	if !appCfg.Mode.HealthCheck && container.IsSuperseded(currentWatchtowerContainer) {
		return p.retire()
	}

	err = p.setUpNotifications()
	if err != nil {
		return err
	}

	err = p.resolveScope(inspectErr)
	if err != nil {
		return err
	}

	if currentWatchtowerContainerUnknown {
		p.log.Warn().Err(detectErr).Msg(logging.SelfUnidentified)
	} else if inspectErr != nil {
		p.log.Warn().Err(inspectErr).Msg(logging.SelfUninspected)
	}

	// Log deprecated notification configuration options, if set.
	notify.LogLegacyDeprecationWarnings(p.log, appCfg.Notify.LegacyTypes)

	return nil
}

// retire retires Watchtower's container after a self-update replaced it.
//
// Returns:
//   - error: An [exitError] with exit code 0.
func (p *process) retire() error {
	// Notifications are set up first, so the recovery is notified.
	err := p.setUpNotifications()
	if err != nil {
		p.log.Debug().Err(err).Msg("Could not set up notifications for the retirement of an old Watchtower container")
	}

	// Startup retires before it settles the scope, so the scope is worked out
	// here from the configuration and the old container's scope label.
	label, _ := currentWatchtowerContainer.Scope()
	scope := appCfg.Filter.ScopeWithLabel(label)

	selfupdate.RetireOldInstance(p.log, context.Background(), client, currentWatchtowerContainer, scope, restartPolicyTimeout)

	return &exitError{err: errSupersededInstance, msg: "", code: 0}
}

// setUpNotifications creates the notifier from the notification settings and
// hooks it to the process logger, so the lines logged after it reach
// notifications.
//
// A health check probe runs often, so it sets up no notifier, and its warnings
// stay in the log.
//
// Returns:
//   - error: An [exitError] when the notifier cannot be created.
func (p *process) setUpNotifications() error {
	if appCfg.Mode.HealthCheck {
		return nil
	}

	var err error

	notifier, err = notify.NewNotifier(p.log, appCfg.Notify)
	if err != nil {
		return &exitError{err: err, msg: "Failed to initialize notifications", code: 1}
	}

	notifier.RegisterHook(p.log)

	return nil
}

// resolveScope settles the scope Watchtower runs in, taking it from
// Watchtower's own scope label when the configuration sets none.
//
// Without its own container, Watchtower keeps the configured scope. When its
// container could not be inspected and no scope is set, the scope label is
// unknown. Running without a scope would update containers that belong to
// another Watchtower instance, so Watchtower exits instead. A health check
// runs no updates, so it continues.
//
// Parameters:
//   - inspectErr: The error from inspecting Watchtower's own container. It is
//     nil when the inspect succeeded.
//
// Returns:
//   - error: An [exitError] when the scope label cannot be read or the filter
//     cannot be built.
func (p *process) resolveScope(inspectErr error) error {
	if inspectErr != nil && appCfg.Filter.Scope == "" && !appCfg.Mode.HealthCheck {
		// Disable restarts first, so the runtime does not start Watchtower again.
		p.disableRestarts()
		p.log.Error().Err(inspectErr).Msg(logging.ScopeLabelUnreadable)

		return &exitError{err: inspectErr, msg: "", code: 1}
	}

	var label string
	if currentWatchtowerContainer != nil {
		label, _ = currentWatchtowerContainer.Scope()
	}

	scoped, err := appCfg.Filter.WithLabelScope(p.log, label)
	if err != nil {
		return &exitError{err: err, msg: "Failed to build container filter", code: 1}
	}

	appCfg.Filter = scoped

	if appCfg.Filter.Scope != "" {
		p.log.Debug().
			Str("scope", appCfg.Filter.Scope).
			Msg("Resolved operational scope")
	}

	return nil
}

// run executes the main Watchtower logic based on parsed command-line flags.
//
// It uses the configuration preRun prepared. A health check exits early. Otherwise run builds
// the RunConfig from appCfg and hands it to runMain for core execution.
//
// Parameters:
//   - _: The cobra.Command instance being executed, whose settings preRun already loaded.
//   - _: Positional arguments, which preRun already loaded as container names.
//
// Returns:
//   - error: An [exitError] when Watchtower cannot continue or its run failed.
func (p *process) run(_ *cobra.Command, _ []string) error {
	if appCfg.Mode.HealthCheck {
		if isContainerMainProcess() {
			sleepFunc(1 * time.Second)

			return &exitError{
				err:  nil,
				msg:  "The health check flag should never be passed to the main watchtower container process",
				code: 1,
			}
		}

		return nil
	}

	cfg, err := appCfg.BuildRunConfig()
	if err != nil {
		return &exitError{err: err, msg: logging.RunConfigurationFailed, code: 1}
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
// a complete [updateParams.Params] snapshot.
//
// Parameters:
//   - cfg: The RunConfig struct containing filter, API, and mode parameters for this execution.
//
// Returns:
//   - error: An [exitError] for a failure that was already logged.
func (p *process) runMain(cfg settings.RunConfig) error {
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
	//   - filter: The filters.Filter determining which containers are targeted for updates.
	//   - params: The updateParams.Params struct containing update configuration parameters.
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

	runUpdatesWithNotifications = func(ctx context.Context, filter filters.Filter, params updateParams.Params) *metrics.Metric {
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
			p.log.Error().Err(err).Msg(logging.RollingRestartValidationFailed)

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
		logging.WriteStartupMessage(ctx, startup)

		params := baseParams
		params.RunOnce = true

		metric := runUpdatesWithNotifications(ctx, cfg.Filter, params)
		metrics.Default().RegisterScan(metric)
		p.disableRestarts()

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

	// Remove ephemeral orchestrators that a crash or failed start left behind.
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

	// One updateParams.Params snapshot for HTTP API and schedule paths.
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
			Logger:                      &apiLog,
			Host:                        cfg.APIHost,
			Port:                        cfg.APIPort,
			Token:                       cfg.APIToken,
			EventsToken:                 cfg.APIEventsToken,
			RateLimit:                   cfg.APIRateLimit,
			EnableCheckAPI:              cfg.EnableCheckAPI,
			EnableConfigAPI:             cfg.EnableConfigAPI,
			EnableContainersAPI:         cfg.EnableContainersAPI,
			EnableEventsAPI:             cfg.EnableEventsAPI,
			EnableHealthAPI:             cfg.EnableHealthAPI,
			EnableHistoryAPI:            cfg.EnableHistoryAPI,
			EnableImagesAPI:             cfg.EnableImagesAPI,
			EnableMetricsAPI:            cfg.EnableMetricsAPI,
			EnableSwaggerAPI:            cfg.EnableSwaggerAPI,
			EnableUpdateAPI:             cfg.EnableUpdateAPI,
			CheckTimeout:                cfg.CheckAPITimeout,
			UpdateTimeout:               cfg.UpdateAPITimeout,
			TLSCertPath:                 cfg.TLSCertPath,
			TLSKeyPath:                  cfg.TLSKeyPath,
			CORSAllowedOrigins:          cfg.CORSAllowedOrigins,
			TrustedProxies:              cfg.TrustedProxies,
			ProxyHeader:                 cfg.ProxyHeader,
			UnblockHTTPAPI:              cfg.UnblockHTTPAPI,
			NoStartupMessage:            cfg.NoStartupMessage,
			Filter:                      cfg.Filter,
			FilterDesc:                  cfg.FilterDesc,
			UpdateLock:                  updateLock,
			BaseParams:                  sharedBase,
			IncludeStopped:              appCfg.Client.IncludeStopped,
			IncludeRestarting:           appCfg.Client.IncludeRestarting,
			LabelEnable:                 appCfg.Filter.LabelEnable,
			Client:                      client,
			GitClient:                   gitClient,
			Notifier:                    notifier,
			Scope:                       appCfg.Filter.Scope,
			Version:                     meta.Version,
			Startup:                     startupBase,
			RunUpdatesWithNotifications: runUpdatesWithNotifications,
			FilterByImage: func(images []string, base filters.Filter) filters.Filter {
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
		p.log.Error().Err(err).Msg(logging.APISetupFailed)

		return &exitError{err: err, msg: "", code: 1}
	}

	// Schedule and execute periodic updates, handling errors or shutdown. In
	// API-only mode the HTTP API returns only at shutdown. The scheduler then
	// only waits for an update the API started and announces nothing.
	err = schedule.RunUpgradesOnSchedule(ctx, schedule.ScheduleDeps{
		Logger:                p.log,
		Filter:                cfg.Filter,
		Lock:                  updateLock,
		ScheduleSpec:          appCfg.Schedule.Spec,
		Startup:               startupBase,
		WriteStartupMessage:   logging.WriteStartupMessage,
		RunUpdate:             runUpdatesWithNotifications,
		UpdateOnStart:         cfg.UpdateOnStart,
		ReplacedOlderInstance: totalRemovedInstances > 0,
		BaseParams:            sharedBase,
	})

	// Stop the HTTP API and give it its shutdown grace period to finish the
	// requests it is serving before the process exits.
	stop()
	<-apiStopped

	if err != nil {
		p.disableRestarts()
		p.log.Error().Err(err).Msg(logging.SchedulingFailed)

		return &exitError{err: err, msg: "", code: 1}
	}

	return nil
}

// disableRestarts sets the restart policy of Watchtower's container to "no".
//
// Only the container's main process changes the policy. A host binary and a
// command started with docker exec, such as a health check probe, leave it
// alone. A policy that is already "no" is not updated. A failure, including a
// container ID that is not known, is logged as an error, because the runtime
// may then start Watchtower again. Exit paths call it before they report the
// exit, so the exit's notification includes that error.
func (p *process) disableRestarts() {
	if !isContainerMainProcess() {
		return
	}

	if currentWatchtowerContainerID == "" {
		p.log.Error().Err(errUnknownContainerID).Msg(logging.RestartsKept)

		return
	}

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
		p.log.Error().Err(err).Msg(logging.RestartsKept)
	}
}

// disableRestartsBeforeExit disables Watchtower's restarts when the root command fails.
//
// A subcommand's failure leaves them alone, since a subcommand such as
// notify-upgrade can run inside the main container. Without a Docker client
// from startup, the container's main process connects with default options,
// using the connection settings its flags and environment hold. When it
// cannot connect or find its container, it logs that its restarts stay
// enabled.
//
// Parameters:
//   - cmd: The command that failed, whose flags hold the Docker connection
//     settings.
func (p *process) disableRestartsBeforeExit(cmd *cobra.Command) {
	if cmd.HasParent() {
		return
	}

	if client == nil {
		// Only the main process changes the restart policy, so only it
		// connects for that.
		if !isContainerMainProcess() {
			return
		}

		var docker dockerConfig.Docker

		err := p.clientErr
		if err == nil {
			// Startup reports the TLS warnings when it loads the settings
			// itself, so this read logs nothing.
			docker, err = settings.LoadDocker(new(zerolog.Nop()), cmd)
		}

		if err == nil {
			// The other client options only change how updates run. The
			// process is exiting, so it waits only briefly for a daemon that
			// does not answer.
			ctx, cancel := context.WithTimeout(context.Background(), restartPolicyTimeout)
			client, err = newClient(ctx, p.log, containerClient.ClientOptions{Docker: docker})

			cancel()
		}

		if err == nil {
			err = p.detectCurrentContainerID()
		}

		if err != nil {
			p.log.Error().Err(err).Msg(logging.RestartsKept)

			return
		}
	}

	p.disableRestarts()
}

// isContainerMainProcess reports whether Watchtower is the main process of its
// container, whose exit stops the container.
//
// The main process is PID 1. When the container is started with an init,
// such as docker run --init, the init runs as PID 1 and the main process is
// its child. A command started with docker exec, such as a health check
// probe, usually has parent PID 0. Some kernels, such as OpenVZ's, report
// parent PID 1 for it although PID 1 did not start it. The parent PID
// therefore counts only when PID 1 lists this process among its children.
// A kernel without that list never counts it. A host binary is never PID 1.
// Its parent PID is 1 when an init system such as systemd starts it, so the
// parent PID counts only in a container.
//
// Returns:
//   - bool: True for the main process of a container.
func isContainerMainProcess() bool {
	pid := processID()
	if pid == 1 {
		return true
	}

	if parentProcessID() != 1 || !containerClient.InContainer(processFs) {
		return false
	}

	// The kernel provides this list only when built with CONFIG_PROC_CHILDREN.
	children, err := fs.ReadFile(processFs, "proc/1/task/1/children")
	if err != nil {
		return false
	}

	return slices.Contains(strings.Fields(string(children)), strconv.Itoa(pid))
}

// detectCurrentContainerID detects the ID of Watchtower's own container for an
// exit path, waiting at most 5 seconds. A failed detection leaves the ID
// empty.
//
// Returns:
//   - error: Non-nil when the ID cannot be detected.
func (p *process) detectCurrentContainerID() error {
	ctx, cancel := context.WithTimeout(context.Background(), containerLookupTimeout)
	defer cancel()

	var err error

	currentWatchtowerContainerID, err = containerClient.GetCurrentContainerID(p.log, ctx, processFs, client)

	return err //nolint:wrapcheck // The error already says that the container ID could not be detected.
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
