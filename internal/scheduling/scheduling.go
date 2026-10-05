// Package scheduling provides functionality for scheduling and executing container updates in Watchtower.
// It handles periodic scheduling using cron specifications, manages update concurrency, and ensures
// graceful shutdown of scheduled operations.
package scheduling

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/logging"
	"github.com/nicholas-fedor/watchtower/internal/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// updateWaitTimeout bounds how long shutdown waits for an in-flight update.
const updateWaitTimeout = 60 * time.Second

// scheduleRescanSpec is the internal cron spec used to periodically rescan
// container labels for per-container schedule overrides, so containers created
// after startup get their override registered without a restart.
const scheduleRescanSpec = "@every 60s"

// normalizeScheduleSpec trims whitespace and surrounding quotes from a cron spec.
//
// Parameters:
//   - spec: Raw schedule specification from a flag or container label.
//
// Returns:
//   - string: Normalized schedule specification.
func normalizeScheduleSpec(spec string) string {
	return strings.Trim(strings.TrimSpace(spec), `"'`)
}

// containerScheduleSpec returns the normalized per-container schedule override
// from the com.centurylinklabs.watchtower.schedule label.
//
// Parameters:
//   - c: Container to inspect.
//
// Returns:
//   - string: The schedule override spec, or an empty string when unset.
func containerScheduleSpec(c types.FilterableContainer) string {
	raw, ok := c.GetLabel(container.ScheduleLabel)
	if !ok {
		return ""
	}

	return normalizeScheduleSpec(raw)
}

// WaitForRunningUpdate waits for any currently running update to complete before proceeding with shutdown.
// It checks the lock channel status and blocks with a timeout if an update is in progress.
//
// Parameters:
//   - log: Process logger. Required and must be non-nil. A nil logger panics on the first log call.
//   - ctx: The context for cancellation, allowing early shutdown on context timeout.
//   - lock: The channel used to synchronize updates, ensuring only one runs at a time.
func WaitForRunningUpdate(log *zerolog.Logger, ctx context.Context, lock chan bool) {
	log.Debug().Msg("Checking lock status before shutdown.")

	if len(lock) == 0 {
		select {
		case v := <-lock:
			log.Debug().Msg("Lock acquired, update finished.")

			lock <- v
		case <-time.After(updateWaitTimeout):
			log.Warn().Msg("Timeout waiting for running update to finish, proceeding with shutdown.")
		case <-ctx.Done():
			log.Warn().Msg("Context canceled while waiting for running update.")
		}
	} else {
		log.Debug().Msg("No update running, lock available.")
	}

	log.Debug().Msg("Lock check completed.")
}

// ScheduleDeps holds dependencies for scheduled update runs.
//
// BaseParams must be a complete types.UpdateParams snapshot from config.UpdateParams
// (or an equivalent full construction).
// Each tick copies BaseParams and applies only per-run fields such as SkipSelfUpdate.
type ScheduleDeps struct {
	// Logger is the process logger for scheduled runs. Required and must be non-nil.
	Logger *zerolog.Logger
	// Filter determines which containers are updated.
	Filter types.Filter
	// FilterDesc is a human-readable description of the filter for startup messaging.
	FilterDesc string
	// Lock ensures only one update runs at a time, or nil to create a new one.
	Lock chan bool
	// ScheduleSpec is the cron-formatted schedule string for periodic updates.
	ScheduleSpec string
	// Startup holds resolved values for startup messaging (no flag reads).
	// Callers must set Filtering, Scope, Client, Notifier, and Version on Startup.
	// RunUpgradesOnSchedule only applies Sched and UpdateOnStart at send time.
	Startup logging.StartupParams
	// WriteStartupMessage writes the startup message with scheduling information.
	WriteStartupMessage func(logging.StartupParams)
	// RunUpdate performs container updates and sends notifications.
	RunUpdate func(context.Context, types.Filter, types.UpdateParams) *metrics.Metric
	// Client is retained for callers. Prefer Startup.Client for messaging.
	Client container.Client
	// Scope is retained for callers. Prefer Startup.Scope for messaging.
	Scope string
	// Notifier is closed on schedule shutdown. Prefer Startup.Notifier for messaging.
	Notifier types.Notifier
	// MetaVersion is retained for callers. Prefer Startup.Version for messaging.
	MetaVersion string
	// UpdateOnStart triggers an immediate update before the scheduler starts.
	UpdateOnStart bool
	// SkipFirstRun skips Watchtower self-update on the first update run of any
	// kind (useful after self-update cleanup of old instances).
	SkipFirstRun bool
	// CurrentWatchtowerContainer is the running Watchtower container for parent checking.
	CurrentWatchtowerContainer types.Container
	// StartupMessageSent is true when the startup message was already sent
	// (for example by the HTTP API in blocking mode).
	StartupMessageSent bool
	// BaseParams is the complete update policy snapshot for every scheduled tick.
	// Must include Cleanup, MonitorOnly, UseComposeDependsOn, ReviveStopped, and
	// all other process-wide UpdateParams fields.
	BaseParams types.UpdateParams
}

// RunUpgradesOnSchedule schedules and executes periodic container updates according to the cron specification.
//
// It sets up a cron scheduler, runs updates at specified intervals, and ensures graceful shutdown on interrupt
// signals (SIGINT, SIGTERM) or context cancellation, handling concurrency with a lock channel.
// If update-on-start is enabled, it triggers the first update immediately before starting the scheduler.
// If SkipFirstRun is true, it skips Watchtower self-update on the first update run (useful after self-update cleanup).
//
// Parameters:
//   - ctx: The context controlling the scheduler's lifecycle, enabling shutdown on cancellation.
//   - deps: Schedule dependencies including a complete BaseParams policy snapshot.
//     deps.Logger is required and must be non-nil (nil panics on first log call).
//
// Returns:
//   - error: An error if scheduling fails (e.g., invalid cron spec), nil on successful shutdown.
func RunUpgradesOnSchedule(ctx context.Context, deps ScheduleDeps) error {
	log := deps.Logger
	// Initialize lock if not provided, ensuring single-update concurrency.
	lock := deps.Lock
	if lock == nil {
		lock = make(chan bool, 1)
		lock <- true
	}

	// Create a new cron scheduler for managing periodic updates.
	// Configured with optional seconds, skip overlapping runs, and panic recovery.
	// The parser is retained to compute next update run times across all jobs.
	parser := cron.NewParser(
		cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	scheduler := cron.New(
		cron.WithParser(parser),
		cron.WithChain(
			cron.SkipIfStillRunning(cron.DefaultLogger),
			cron.Recover(cron.DefaultLogger),
		),
	)

	// The default schedule spec, applied to containers without a schedule override label.
	scheduleSpec := normalizeScheduleSpec(deps.ScheduleSpec)

	// Determine if self-update should be skipped due to exposed port conflicts.
	// When a port is configured (e.g., HTTP API), the old container holds the port
	// while the new container tries to bind it, resulting in both containers being stopped.
	// Ephemeral self-updates are exempt from this restriction because they remove
	// the old container before creating the new one, avoiding port conflicts.
	skipSelfUpdateForPorts := deps.CurrentWatchtowerContainer != nil &&
		deps.CurrentWatchtowerContainer.HasExposedPorts() &&
		!deps.BaseParams.EphemeralSelfUpdate

	// The base container filter, identical to what updateFunc derives per tick.
	baseFilter := deps.Filter
	if baseFilter == nil {
		baseFilter = deps.BaseParams.Filter
	}

	// Per-container schedule overrides (com.centurylinklabs.watchtower.schedule).
	// customSpecs holds override specs registered with the scheduler; specsMu
	// guards it because cron jobs and label rescans run concurrently.
	var specsMu sync.Mutex

	// scanMu serializes label scans so a stale scan cannot remove jobs
	// registered by a newer one.
	var scanMu sync.Mutex

	customSpecs := make(map[string]cron.EntryID)
	invalidSpecs := make(map[string]struct{})

	// hasCustomSchedule reports whether the container's schedule label names a
	// registered override spec, meaning it is excluded from the default schedule.
	hasCustomSchedule := func(c types.FilterableContainer) bool {
		spec := containerScheduleSpec(c)
		if spec == "" {
			return false
		}

		specsMu.Lock()
		defer specsMu.Unlock()

		_, ok := customSpecs[spec]

		return ok
	}

	// defaultScheduleMatch admits containers that follow the default schedule:
	// everything except containers with a registered schedule override.
	defaultScheduleMatch := func(c types.FilterableContainer) bool {
		return !hasCustomSchedule(c)
	}

	// firstRun marks whether the first update after a self-update cleanup has
	// run yet; whichever scheduled job fires first skips the Watchtower
	// self-update, including per-container override jobs.
	var firstRun atomic.Uint32

	// syncContainerSchedules refreshes schedule override registrations. It is
	// assigned after updateFunc because each job closure calls updateFunc.
	var syncContainerSchedules func()

	// nextUpdateRun returns the soonest next activation across the default
	// schedule and all registered per-container overrides.
	nextUpdateRun := func() time.Time {
		var specs []string
		if scheduleSpec != "" {
			specs = append(specs, scheduleSpec)
		}

		specsMu.Lock()

		for spec := range customSpecs {
			specs = append(specs, spec)
		}

		specsMu.Unlock()

		var next time.Time
		for _, spec := range specs {
			sched, err := parser.Parse(spec)
			if err != nil {
				continue
			}

			if t := sched.Next(time.Now()); next.IsZero() || t.Before(next) {
				next = t
			}
		}

		return next
	}

	// resolveSkipSelfUpdate applies the self-update skip conditions. If
	// Watchtower has performed a self-cleanup, the first update of any kind
	// skips the self-update; the check lives here rather than in a job wrapper
	// so it applies to whichever schedule fires first, including per-container
	// override jobs. Exposed ports also force the skip to prevent port
	// conflicts during container replacement.
	resolveSkipSelfUpdate := func(skipWatchtowerSelfUpdate bool) bool {
		if deps.SkipFirstRun && firstRun.CompareAndSwap(0, 1) {
			log.Debug().Msg(
				"Skipping Watchtower self-update on first scheduled run due to cleanup",
			)

			return true
		}

		if skipSelfUpdateForPorts {
			log.Debug().Msg("Published ports detected - self-update skipped.")

			return true
		}

		return skipWatchtowerSelfUpdate
	}

	// isWatchtowerParentUpdate reports whether the run should be skipped
	// because the current container is an old Watchtower parent left over
	// from a self-update chain.
	isWatchtowerParentUpdate := func() bool {
		if deps.CurrentWatchtowerContainer == nil {
			return false
		}

		chain, _ := deps.CurrentWatchtowerContainer.GetContainerChain()
		if !container.IsWatchtowerParent(deps.CurrentWatchtowerContainer.ID(), chain) {
			return false
		}

		log.Debug().Msg("Skipping scheduled update for Watchtower parent container")

		if nextRun := nextUpdateRun(); !nextRun.IsZero() {
			log.Debug().Msg("Scheduled next run: " + nextRun.String())
		}

		return true
	}

	// acquireUpdateLock takes the update lock, waiting indefinitely when
	// blocking and returning nil immediately otherwise if an update is already
	// running. The returned function releases the lock.
	acquireUpdateLock := func(blocking bool) func() {
		if blocking {
			v := <-lock

			return func() { lock <- v }
		}

		select {
		case v := <-lock:
			return func() { lock <- v }
		default:
			log.Debug().Msg("Update skipped: another update is currently running")

			return nil
		}
	}

	// buildUpdateFilter narrows the base filter to the containers assigned to
	// this schedule. A nil scheduleMatch leaves the base filter untouched.
	buildUpdateFilter := func(scheduleMatch types.Filter) types.Filter {
		if scheduleMatch == nil {
			return baseFilter
		}

		return func(c types.FilterableContainer) bool {
			if !scheduleMatch(c) {
				return false
			}

			if baseFilter == nil {
				return true
			}

			return baseFilter(c)
		}
	}

	// Define the update function to be used both for scheduled runs and immediate execution.
	// skipWatchtowerSelfUpdate: whether to skip updating the Watchtower container itself
	// blocking: whether to wait for the lock (true for scheduled runs, false for immediate runs)
	// scheduleMatch: predicate limiting the run to containers assigned to this schedule
	updateFunc := func(skipWatchtowerSelfUpdate, blocking bool, scheduleMatch types.Filter) {
		if isWatchtowerParentUpdate() {
			return
		}

		release := acquireUpdateLock(blocking)
		if release == nil {
			return
		}

		defer release()

		// Refresh override registrations so a container labeled since the last
		// scan follows its own schedule instead of being picked up by this one.
		if syncContainerSchedules != nil {
			syncContainerSchedules()
		}

		params := deps.BaseParams
		params.RunOnce = false

		// Keep params.Filter and the positional filter argument identical so
		// runUpdatesWithNotifications cannot prefer a divergent source.
		params.Filter = buildUpdateFilter(scheduleMatch)

		if deps.RunUpdate == nil {
			log.Debug().Msg("Update skipped: RunUpdate hook is not configured")

			return
		}

		// Claim the first-run skip only once the run is actually executing so
		// invocations that abort above do not consume it.
		params.SkipSelfUpdate = resolveSkipSelfUpdate(skipWatchtowerSelfUpdate)

		metric := deps.RunUpdate(ctx, params.Filter, params)
		if metric != nil {
			metrics.Default().RegisterScan(metric)
		}

		log.Debug().Msg("Update operation completed")

		if nextRun := nextUpdateRun(); !nextRun.IsZero() {
			log.Debug().Msg("Scheduled next run: " + nextRun.String())
		}
	}

	// Add the update function to the cron schedule, handling concurrency and metrics.
	if scheduleSpec != "" {
		_, err := scheduler.AddFunc(
			scheduleSpec,
			func() { updateFunc(false, true, defaultScheduleMatch) },
		)
		if err != nil {
			return fmt.Errorf("failed to schedule updates: %w", err)
		}
	}

	// registerScheduleSpec registers a cron job for one distinct override spec
	// unless it is already known (registered or remembered as invalid).
	registerScheduleSpec := func(spec string) {
		specsMu.Lock()
		defer specsMu.Unlock()

		if _, ok := customSpecs[spec]; ok {
			return
		}

		if _, ok := invalidSpecs[spec]; ok {
			return
		}

		scheduleMatch := func(fc types.FilterableContainer) bool {
			return containerScheduleSpec(fc) == spec
		}

		entryID, err := scheduler.AddFunc(spec, func() {
			updateFunc(false, true, scheduleMatch)
		})
		if err != nil {
			invalidSpecs[spec] = struct{}{}

			log.Warn().
				Err(err).
				Str("schedule", spec).
				Msg("Invalid per-container schedule label, using default schedule")

			return
		}

		customSpecs[spec] = entryID

		log.Info().
			Str("schedule", spec).
			Msg("Registered per-container schedule override")
	}

	// syncContainerSchedules scans containers for schedule override labels and
	// registers a cron job per distinct spec. Overrides already registered or
	// equal to the default schedule are skipped; specs that fail to parse are
	// remembered so each is only warned about once, and the affected containers
	// keep following the default schedule.
	syncContainerSchedules = func() {
		if deps.Client == nil {
			return
		}

		scanMu.Lock()
		defer scanMu.Unlock()

		var listFilters []types.Filter
		if baseFilter != nil {
			listFilters = append(listFilters, baseFilter)
		}

		containers, err := deps.Client.ListContainers(ctx, listFilters...)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to list containers for schedule overrides")

			return
		}

		seen := make(map[string]struct{}, len(containers))

		for _, c := range containers {
			spec := containerScheduleSpec(c)
			if spec == "" || spec == scheduleSpec {
				continue
			}

			seen[spec] = struct{}{}
			registerScheduleSpec(spec)
		}

		// Remove jobs whose spec no longer appears on any container, so removed
		// labels stop firing.
		specsMu.Lock()
		defer specsMu.Unlock()

		for spec, entryID := range customSpecs {
			if _, ok := seen[spec]; ok {
				continue
			}

			scheduler.Remove(entryID)
			delete(customSpecs, spec)

			log.Info().
				Str("schedule", spec).
				Msg("Removed per-container schedule override")
		}
	}

	syncContainerSchedules()

	// Periodically rescan container labels so containers created after startup
	// pick up their schedule overrides without waiting for a restart.
	if deps.Client != nil {
		if _, err := scheduler.AddFunc(scheduleRescanSpec, syncContainerSchedules); err != nil {
			log.Debug().
				Err(err).
				Str("schedule", scheduleRescanSpec).
				Msg("Failed to register schedule rescan job")
		}
	}

	// Log startup message with the first scheduled run time.
	// Skip if the startup message was already sent (e.g., by the HTTP API in blocking mode).
	nextRun := nextUpdateRun()

	// Log startup message with the first scheduled run time.
	// Skip if the startup message was already sent (for example by the HTTP API in blocking mode).
	// Startup is the single source of truth for messaging fields (Filtering, Scope, Client,
	// Notifier, Version). Only apply scheduler-owned runtime values here.
	if !deps.StartupMessageSent && deps.WriteStartupMessage != nil {
		startup := deps.Startup
		startup.Sched = nextRun
		startup.UpdateOnStart = &deps.UpdateOnStart
		deps.WriteStartupMessage(startup)
	}

	// Check if update-on-start is enabled and trigger immediate update if so.
	// The immediate run follows the default schedule's container selection.
	if deps.UpdateOnStart {
		updateFunc(false, false, defaultScheduleMatch)
	}

	// Start the scheduler to begin periodic execution if any jobs were
	// registered (default schedule, per-container overrides, or label rescan).
	if len(scheduler.Entries()) > 0 {
		scheduler.Start()
	}

	// Set up signal handling for graceful shutdown.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	// Wait for shutdown signal or context cancellation.
	select {
	case <-ctx.Done():
		log.Debug().Msg("Context canceled, stopping scheduler...")
	case <-interrupt:
		log.Debug().Msg("Received interrupt signal, stopping scheduler...")
	}

	// Stop the scheduler and wait for any running update to complete.
	scheduler.Stop()
	log.Debug().Msg("Waiting for running update to be finished...")

	// Original ctx is often already canceled at shutdown. Detach cancel and
	// apply a bound so an active RunUpdate can finish before Notifier.Close().
	waitCtx, waitCancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		updateWaitTimeout,
	)
	defer waitCancel()

	WaitForRunningUpdate(log, waitCtx, lock)

	// Close the notification system to clean up resources during shutdown.
	if deps.Notifier != nil {
		deps.Notifier.Close()
	}

	log.Debug().Msg("Scheduler stopped and update completed.")

	return nil
}

// ShouldExitDueToInvalidRestart determines if the program should exit due to an invalid restart of an old Watchtower container.
//
// This function checks two conditions:
//  1. The current container's name matches the watchtower-old-* prefix, indicating it is
//     a predecessor renamed during self-update that should not run.
//  2. The current container is present in the container chain label, indicating it is
//     an ancestor in the self-update lineage.
//
// If either condition is true and runOnce is false, the program should exit
// to prevent an old Watchtower container from running.
//
// Parameters:
//   - c: The current Watchtower container to check.
//   - runOnce: Whether the process is in run-once mode.
//
// Returns:
//   - bool: True if the program should exit due to an invalid restart, false otherwise.
func ShouldExitDueToInvalidRestart(c types.Container, runOnce bool) bool {
	if c == nil {
		return false
	}

	if container.IsOldContainer(c.Name()) && !runOnce {
		return true
	}

	chain, present := c.GetContainerChain()
	if !present {
		return false
	}

	return container.IsWatchtowerParent(c.ID(), chain) && !runOnce
}
