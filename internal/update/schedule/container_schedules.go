package schedule

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/schedule/percontainer"
)

// containerSchedules runs the containers that carry a schedule label of their
// own, beside the global schedule (see package percontainer). A nil
// *containerSchedules is the option turned off: every method then does
// nothing.
type containerSchedules struct {
	source percontainer.Source
	log    *zerolog.Logger
	// base is the containers Watchtower manages, before the global schedule
	// leaves out those with a schedule of their own.
	base filters.Filter
	// skipSelf skips Watchtower's own update on each container schedule's
	// first run after the process replaced an older instance, as the global
	// schedule's first run does.
	skipSelf firstRuns
}

// withContainerSchedules sets deps up for container schedules when
// [ScheduleDeps.ContainerSchedules] is set: the global schedule then covers
// only containers without a schedule of their own, and the source reads the
// containers again after each run, in case it missed a change. It returns
// deps unchanged and nil when container schedules are off.
func withContainerSchedules(deps ScheduleDeps) (ScheduleDeps, *containerSchedules) {
	if deps.ContainerSchedules == nil {
		return deps, nil
	}

	base := deps.Filter
	if base == nil {
		base = deps.BaseParams.Filter
	}

	if base == nil {
		base = func(filters.FilterableContainer) bool { return true }
	}

	deps.Filter = percontainer.WithoutOwnSchedule(base)

	if refresher, ok := deps.ContainerSchedules.(percontainer.Refresher); ok && deps.RunUpdate != nil {
		runUpdate := deps.RunUpdate
		deps.RunUpdate = func(ctx context.Context, filter filters.Filter, params updateParams.Params) *metrics.Metric {
			defer refresher.Refresh()

			return runUpdate(ctx, filter, params)
		}
	}

	return deps, &containerSchedules{
		source:   deps.ContainerSchedules,
		log:      deps.Logger,
		base:     base,
		skipSelf: firstRuns{enabled: deps.ReplacedOlderInstance, log: deps.Logger},
	}
}

// start adds a scheduler entry for each schedule containers bring, kept
// current by the source until ctx ends. run makes one update: whether to skip
// Watchtower's own update, and the containers it covers. The returned
// function waits for the source to stop, so it adds no entries to a stopped
// scheduler.
func (c *containerSchedules) start(
	ctx context.Context,
	scheduler *cron.Cron,
	run func(skipSelf bool, only filters.Filter),
) func() {
	if c == nil {
		return func() {}
	}

	registry := percontainer.NewRegistry(c.log, cronTimers{scheduler: scheduler, log: c.log, run: func(spec string) {
		run(c.skipSelf.first(spec), percontainer.OnSchedule(spec, c.base))
	}})

	done := make(chan struct{})

	go func() {
		defer close(done)

		superviseSource(ctx, c.log, c.source, registry, time.Second, time.Minute)
	}()

	return func() { <-done }
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
