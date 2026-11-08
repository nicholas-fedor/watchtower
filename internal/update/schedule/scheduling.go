package schedule

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync/atomic"
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
	// ContainerSchedules, when set, lets containers follow their own schedule
	// label (see package percontainer): it reports which containers carry
	// which schedule. Those containers leave the global schedule. Nil keeps
	// every container on the global schedule.
	ContainerSchedules percontainer.Source
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
	deps, containers := withContainerSchedules(deps)
	// Initialize lock if not provided, ensuring single-update concurrency.
	lock := deps.Lock
	if lock == nil {
		lock = NewUpdateLock()
	}

	// Create a new cron scheduler for managing periodic updates.
	// Configured with optional seconds, skip overlapping runs, and panic recovery.
	scheduler := cron.New(
		cron.WithParser(
			cron.NewParser(
				cron.SecondOptional|cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow|cron.Descriptor,
			),
		),
		cron.WithChain(
			skipIfStillRunning(log),
			recoverPanics(log),
		),
	)

	// Define the update function to be used both for scheduled runs and immediate execution.
	// skipWatchtowerSelfUpdate: whether to skip updating the Watchtower container itself
	// blocking: whether to wait for the lock (true for scheduled runs, false for immediate runs)
	// only: the containers this run covers instead (a container schedule's)
	updateFunc := func(skipWatchtowerSelfUpdate, blocking bool, only ...types.Filter) {
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

		if len(only) > 0 {
			updateFilter = only[0]
		}

		params.Filter = updateFilter

		if deps.RunUpdate == nil {
			log.Debug().Msg("Update skipped: RunUpdate hook is not configured")

			return
		}

		metrics.Default().RegisterScan(deps.RunUpdate(ctx, updateFilter, params))

		log.Debug().Msg("Update operation completed")

		nextRuns := scheduler.Entries()
		if len(nextRuns) > 0 {
			log.Debug().Msg("Scheduled next run: " + nextRuns[0].Schedule.Next(time.Now()).String())
		}
	}

	// A process that replaced an older instance does not update itself again
	// during its first scheduled run.
	var ranOnce atomic.Bool

	scheduledUpdateFunc := func() {
		skipWatchtowerSelfUpdate := deps.ReplacedOlderInstance && !ranOnce.Swap(true)
		if skipWatchtowerSelfUpdate {
			log.Debug().Msg("Skipping Watchtower self-update on first scheduled run due to cleanup")
		}

		updateFunc(skipWatchtowerSelfUpdate, true)
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
			updateFunc(false, false)
		}
	}

	// Containers' own schedules, added to the scheduler as containers bring them.
	stopContainerSchedules := containers.start(ctx, scheduler, func(skipSelf bool, only types.Filter) {
		updateFunc(skipSelf, true, only)
	})

	// Start the scheduler to begin periodic execution if scheduling is enabled.
	// Only start if a schedule spec was provided (empty string means no scheduling).
	if scheduleSpec != "" || containers != nil {
		scheduler.Start()
	}

	// Wait for cancellation. The caller's context carries shutdown signals.
	<-ctx.Done()
	log.Debug().Msg("Context canceled, stopping scheduler...")

	// Stop the scheduler and wait for any running update to complete.
	stopContainerSchedules()
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
