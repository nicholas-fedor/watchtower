package schedule

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/schedule/percontainer"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// updateWaitTimeout bounds how long shutdown waits for an in-flight update.
const updateWaitTimeout = 60 * time.Second

// waitForRunningUpdate waits for a running update to finish before shutdown
// and then keeps the update lock, so no further update starts during shutdown.
//
// The wait ends after updateWaitTimeout, or when ctx is done, whichever comes
// first. The running update then keeps the lock.
//
// Parameters:
//   - log: Process logger. Required and must be non-nil. A nil logger panics on the first log call.
//   - ctx: The context for cancellation, allowing early shutdown on context timeout.
//   - lock: The update lock shared by every caller that starts updates.
func waitForRunningUpdate(log *zerolog.Logger, ctx context.Context, lock *UpdateLock) {
	log.Debug().Msg("Checking lock status before shutdown.")

	if lock.TryLock() {
		log.Debug().Msg("No update running, lock available.")
	} else {
		waitCtx, cancel := context.WithTimeout(ctx, updateWaitTimeout)
		defer cancel()

		err := lock.Lock(waitCtx)

		switch {
		case err == nil:
			log.Debug().Msg("Lock acquired, update finished.")
		case errors.Is(err, context.DeadlineExceeded):
			log.Warn().Msg("Timeout waiting for running update to finish, proceeding with shutdown.")
		default:
			log.Warn().Msg("Context canceled while waiting for running update.")
		}
	}

	log.Debug().Msg("Lock check completed.")
}

// skipIfStillRunning returns a cron job wrapper that skips a scheduled run
// while the previous one is still running, including while it waits for an
// update started through the HTTP API.
//
// Each skip is logged at debug and counted as a skipped scan.
//
// Parameters:
//   - log: Scheduler logger.
//
// Returns:
//   - cron.JobWrapper: Wrapper for the scheduler's job chain.
func skipIfStillRunning(log *zerolog.Logger) cron.JobWrapper {
	return func(job cron.Job) cron.Job {
		running := make(chan struct{}, 1)

		return cron.FuncJob(func() {
			select {
			case running <- struct{}{}:
				defer func() { <-running }()

				job.Run()
			default:
				log.Debug().Msg("Update skipped: the previous scheduled update is still running")
				metrics.Default().RegisterSkippedScan()
			}
		})
	}
}

// recoverPanics returns a cron job wrapper that recovers a panic in a
// scheduled run, so later runs still start on schedule.
//
// The update session recovers and notifies its own panics. This wrapper covers
// the scheduler code around the session, so it logs the panic value and stack
// at error without notifying.
//
// Parameters:
//   - log: Scheduler logger.
//
// Returns:
//   - cron.JobWrapper: Wrapper for the scheduler's job chain.
func recoverPanics(log *zerolog.Logger) cron.JobWrapper {
	return func(job cron.Job) cron.Job {
		return cron.FuncJob(func() {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}

				log.Error().
					Str("notify", "no").
					Str("error", fmt.Sprint(recovered)).
					Str("stack", string(debug.Stack())).
					Msg("Scheduled run panicked")
			}()

			job.Run()
		})
	}
}

// writeStartupMessage writes the startup message while it holds the update
// lock.
//
// The HTTP API can start an update as soon as it serves, before this message.
// Because every update holds the lock, the message never opens its
// notification batch while an update's batch is open. It waits for an update
// that holds the lock and writes nothing when ctx is done first. The first
// scheduled run time is read once the lock is taken, after any such wait. The
// writer receives ctx and writes nothing for a process that is already
// stopping.
//
// Parameters:
//   - ctx: Context that ends the wait for the lock.
//   - lock: Update lock shared with every caller that starts updates.
//   - scheduler: Cron scheduler that holds the update schedule.
//   - deps: Schedule dependencies. Startup holds the message's fields.
//     WriteStartupMessage writes it.
//
// Returns:
//   - bool: True when another update held the lock and the message waited for
//     it.
func writeStartupMessage(ctx context.Context, lock *UpdateLock, scheduler *cron.Cron, deps ScheduleDeps) bool {
	waited := !lock.TryLock()
	if waited && lock.Lock(ctx) != nil {
		return waited
	}
	defer lock.Unlock()

	var nextRun time.Time
	if entries := scheduler.Entries(); len(entries) > 0 {
		nextRun = entries[0].Schedule.Next(time.Now())
	}

	// The update at startup is skipped after a wait, so the message does not
	// announce it.
	updateOnStart := deps.updatesOnStart() && !waited

	// Startup holds every other field of the message. Only the scheduler's
	// own values are set here.
	startup := deps.Startup
	startup.Sched = nextRun
	startup.UpdateOnStart = &updateOnStart
	deps.WriteStartupMessage(ctx, startup)

	return waited
}

// ScheduleDeps holds dependencies for scheduled update runs.
//
// BaseParams must be a complete [updateParams.Params] snapshot built from the
// process configuration.
// Each tick copies BaseParams and applies only per-run fields such as SkipSelfUpdate.
// A tick can disable self-updates, but never enables them when BaseParams disables them.
type ScheduleDeps struct {
	// ContainerSchedules, when set, lets containers follow their own schedule
	// label (see package percontainer): it reports which containers carry
	// which schedule. Those containers leave the global schedule. Nil keeps
	// every container on the global schedule.
	ContainerSchedules percontainer.Source
	// Logger is the process logger for scheduled runs. Required and must be non-nil.
	Logger *zerolog.Logger
	// Filter determines which containers are updated.
	Filter filters.Filter
	// Lock ensures only one update runs at a time. Share it with every other
	// caller that starts updates, such as the HTTP API. Nil gives the scheduler
	// a lock of its own.
	Lock *UpdateLock
	// WriteStartupMessage writes the startup message with scheduling
	// information. It receives the scheduler's context and writes nothing once
	// that context is done.
	WriteStartupMessage func(context.Context, logging.StartupParams)
	// RunUpdate performs container updates and sends notifications.
	RunUpdate func(context.Context, filters.Filter, updateParams.Params) *metrics.Metric
	// ScheduleSpec is the cron-formatted schedule string for periodic updates.
	ScheduleSpec string
	// Startup holds resolved values for startup messaging (no flag reads).
	// Callers must set Filtering, Scope, Client, Notifier, and Version on Startup.
	// RunUpgradesOnSchedule only applies Sched and UpdateOnStart at send time.
	Startup logging.StartupParams
	// BaseParams is the complete update policy snapshot for every scheduled tick.
	// Must include Cleanup, MonitorOnly, UseComposeDependsOn, ReviveStopped, and
	// all other process-wide [updateParams.Params] fields.
	BaseParams updateParams.Params
	// UpdateOnStart triggers an immediate update before the scheduler starts.
	UpdateOnStart bool
	// ReplacedOlderInstance reports that startup cleanup removed another
	// Watchtower instance in this process's scope. The scheduler then makes no
	// update at startup. Its first scheduled run also skips self-updates.
	ReplacedOlderInstance bool
}

// updatesOnStart reports whether the scheduler makes an update at startup.
//
// It does when update on start is set, unless the process replaced an older
// instance. That instance is usually the one whose self-update started this
// process, so an update has just run.
//
// Returns:
//   - bool: True when the scheduler updates at startup.
func (d ScheduleDeps) updatesOnStart() bool {
	return d.UpdateOnStart && !d.ReplacedOlderInstance
}

// RunUpgradesOnSchedule schedules and executes periodic container updates according to the cron specification.
//
// It sets up a cron scheduler, runs updates at specified intervals, and shuts down gracefully when ctx is
// canceled, handling concurrency with an update lock. The caller cancels ctx on shutdown signals.
// If update-on-start is enabled, it triggers the first update immediately before starting the scheduler.
// [ScheduleDeps.ReplacedOlderInstance] describes how replacing an older
// instance changes startup and the first scheduled run.
//
// Parameters:
//   - ctx: The context controlling the scheduler's lifecycle. Canceling it, for example on SIGINT or SIGTERM, shuts
//     the scheduler down.
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
		lock = NewUpdateLock()
	}

	// Create a new cron scheduler for managing periodic updates.
	// Configured with optional seconds, skip overlapping runs, and panic recovery.
	scheduler := cron.New(
		cron.WithParser(cron.NewParser(percontainer.ParseOptions)),
		cron.WithChain(
			skipIfStillRunning(log),
			recoverPanics(log),
		),
	)

	// Define the update function to be used both for scheduled runs and immediate execution.
	// skipWatchtowerSelfUpdate: whether to skip updating the Watchtower container itself
	// blocking: whether to wait for the lock (true for scheduled runs, false for immediate runs)
	// narrow: which of the filtered containers this run covers (nil: all of them)
	refresher, _ := deps.ContainerSchedules.(percontainer.Refresher)

	updateFunc := func(skipWatchtowerSelfUpdate, blocking bool, narrow func(types.Filter) types.Filter) {
		// Acquire the update lock: blocking waits until shutdown, non-blocking returns if unavailable
		if blocking {
			err := lock.Lock(ctx)
			if err != nil {
				log.Debug().Err(err).Msg("Update skipped: shutting down while waiting for the update lock")

				return
			}
		} else if !lock.TryLock() {
			log.Debug().Msg("Update skipped: another update is currently running")
			metrics.Default().RegisterSkippedScan()

			return
		}

		defer lock.Unlock()

		// Shutdown can begin just after the lock is taken. The tick then does
		// nothing, as a tick that is still waiting for the lock does.
		if ctx.Err() != nil {
			log.Debug().Err(ctx.Err()).Msg("Update skipped: shutting down")

			return
		}

		params := deps.BaseParams
		params.RunOnce = false
		params.SkipSelfUpdate = skipWatchtowerSelfUpdate || deps.BaseParams.SkipSelfUpdate

		// One filter for this tick: schedule filter when set, else BaseParams.
		// Keep params.Filter and the positional argument identical so
		// runUpdatesWithNotifications cannot prefer a divergent source.
		updateFilter := deps.Filter
		if updateFilter == nil {
			updateFilter = params.Filter
		}

		if narrow != nil {
			if updateFilter == nil {
				updateFilter = func(types.FilterableContainer) bool { return true }
			}

			updateFilter = narrow(updateFilter)
		}

		params.Filter = updateFilter

		if deps.RunUpdate == nil {
			log.Debug().Msg("Update skipped: RunUpdate hook is not configured")

			return
		}

		metrics.Default().RegisterScan(deps.RunUpdate(ctx, updateFilter, params))

		// Container schedules: read the containers again, in case a change was
		// missed (a connection that died without an error). No polling: it
		// happens only after a run.
		if refresher != nil {
			refresher.Refresh()
		}

		log.Debug().Msg("Update operation completed")

		nextRuns := scheduler.Entries()
		if len(nextRuns) > 0 {
			log.Debug().Msg("Scheduled next run: " + nextRuns[0].Schedule.Next(time.Now()).String())
		}
	}

	// With container schedules, the global schedule covers only the containers
	// without a schedule of their own.
	var global func(types.Filter) types.Filter
	if deps.ContainerSchedules != nil {
		global = percontainer.WithoutOwnSchedule
	}

	// A process that replaced an older instance does not update itself again
	// during its first update cycle: the first scheduled
	// run of each schedule (the global one, and each container schedule, since
	// Watchtower may follow one of its own) skips it.
	skipSelf := firstRuns{enabled: deps.ReplacedOlderInstance, log: log}

	scheduledUpdateFunc := func() {
		if global != nil {
			log.Debug().Msg("Running the global schedule: containers with their own schedule are left out")
		}

		updateFunc(skipSelf.first(""), true, global)
	}

	// Add the update function to the cron schedule, handling concurrency and metrics.
	scheduleSpec := strings.Trim(deps.ScheduleSpec, `"'`)
	if scheduleSpec != "" {
		_, err := scheduler.AddFunc(
			scheduleSpec,
			scheduledUpdateFunc,
		)
		if err != nil {
			return fmt.Errorf("invalid schedule %q: %w", scheduleSpec, err)
		}
	}

	waitedForUpdate := false
	if deps.WriteStartupMessage != nil {
		waitedForUpdate = writeStartupMessage(ctx, lock, scheduler, deps)
	}

	if deps.UpdateOnStart && !deps.updatesOnStart() {
		log.Debug().Msg("Disabled update-on-start due to cleanup of old Watchtower containers")
	}

	// Check if update-on-start is enabled and trigger immediate update if so.
	// A process that is already stopping neither runs it nor counts it as a
	// skipped scan. The update at startup does not wait for another update.
	// When the startup message had to wait for one, the update at startup is
	// skipped as it is when it finds the lock taken.
	if deps.updatesOnStart() && ctx.Err() == nil {
		if waitedForUpdate {
			log.Debug().Msg("Update skipped: another update was running at startup")
			metrics.Default().RegisterSkippedScan()
		} else {
			updateFunc(false, false, global)
		}
	}

	// Container schedules: one entry per schedule in use, kept current by the
	// source. Started after the startup message, so it reports the global
	// schedule's next run.
	sourceDone := make(chan struct{})

	if deps.ContainerSchedules != nil {
		registry := percontainer.NewRegistry(log, cronTimers{scheduler: scheduler, log: log, run: func(spec string) {
			updateFunc(skipSelf.first(spec), true, func(base types.Filter) types.Filter {
				return percontainer.OnSchedule(spec, base)
			})
		}})

		go func() {
			defer close(sourceDone)

			superviseSource(ctx, log, deps.ContainerSchedules, registry, time.Second, time.Minute)
		}()
	} else {
		close(sourceDone)
	}

	// Start the scheduler to begin periodic execution if scheduling is enabled.
	// Only start if a schedule spec was provided (empty string means no
	// scheduling), or containers may bring schedules of their own.
	if scheduleSpec != "" || deps.ContainerSchedules != nil {
		scheduler.Start()
	}

	// Wait for cancellation. The caller's context carries shutdown signals.
	<-ctx.Done()
	log.Debug().Msg("Context canceled, stopping scheduler...")

	// Stop the source first, so it adds no entries to a stopped scheduler, then
	// stop the scheduler and wait for any running update to complete.
	<-sourceDone
	scheduler.Stop()
	log.Debug().Msg("Waiting for running update to be finished...")

	// Original ctx is often already canceled at shutdown. Detach cancel and
	// apply a bound so an active RunUpdate can finish and send its notifications.
	waitCtx, waitCancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		updateWaitTimeout,
	)
	defer waitCancel()

	waitForRunningUpdate(log, waitCtx, lock)

	log.Debug().Msg("Scheduler stopped and update completed.")

	return nil
}

// firstRuns tells each schedule's first scheduled run apart (the global
// schedule's is ""), for the self-update skipped after a cleanup.
type firstRuns struct {
	log     *zerolog.Logger
	ran     map[string]bool
	mu      sync.Mutex
	enabled bool
}

// first reports whether this is the first scheduled run on spec, when the
// skip is enabled; once per schedule.
func (f *firstRuns) first(spec string) bool {
	if !f.enabled {
		return false
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.ran[spec] {
		return false
	}

	if f.ran == nil {
		f.ran = make(map[string]bool)
	}

	f.ran[spec] = true

	event := f.log.Debug()
	if spec != "" {
		event = event.Str("schedule", spec)
	}

	event.Msg("Skipping Watchtower self-update on first scheduled run due to cleanup")

	return true
}

// cronTimers adds container schedules to the scheduler: each entry runs the
// containers on its schedule.
type cronTimers struct {
	scheduler *cron.Cron
	log       *zerolog.Logger
	run       func(spec string)
}

// Add implements [percontainer.Timers].
func (t cronTimers) Add(spec string) (cron.EntryID, error) {
	entryID, err := t.scheduler.AddFunc(spec, func() {
		t.log.Debug().Str("schedule", spec).Msg("Running a container schedule")
		t.run(spec)
		t.log.Debug().
			Str("schedule", spec).
			Time("next", percontainer.Next(spec, time.Now())).
			Msg("Container schedule done")
	})
	if err != nil {
		return 0, fmt.Errorf("invalid container schedule %q: %w", spec, err)
	}

	t.log.Debug().Str("schedule", spec).Time("next", percontainer.Next(spec, time.Now())).
		Msg("Container schedule's first run")

	return entryID, nil
}

// Remove implements [percontainer.Timers].
func (t cronTimers) Remove(id cron.EntryID) {
	t.scheduler.Remove(id)
}

// superviseSource runs the container schedules' source until ctx ends. If it
// stops early, with an error or a panic, it is logged and started again after
// a growing delay, so a fault there never takes down the rest of Watchtower.
func superviseSource(
	ctx context.Context,
	log *zerolog.Logger,
	source percontainer.Source,
	sink percontainer.Sink,
	delay, most time.Duration,
) {
	wait := delay

	for {
		err := runSource(ctx, source, sink)
		if ctx.Err() != nil {
			return
		}

		log.Error().Err(err).Dur("restart_in", wait).
			Msg("Container schedules stopped unexpectedly: restarting them")

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		wait = min(wait*2, most) //nolint:mnd // doubling backoff
	}
}

var (
	// errSourceStopped is a source that returned before it was asked to stop.
	errSourceStopped = errors.New("container schedules source stopped")
	// errSourcePanicked is a source that panicked.
	errSourcePanicked = errors.New("container schedules source panicked")
)

// runSource runs the source once, turning a panic into an error.
func runSource(ctx context.Context, source percontainer.Source, sink percontainer.Sink) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: %v\n%s", errSourcePanicked, recovered, debug.Stack())
		}
	}()

	runErr := source.Run(ctx, sink)
	if runErr != nil {
		return fmt.Errorf("container schedules source: %w", runErr)
	}

	return errSourceStopped
}
