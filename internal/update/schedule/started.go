package schedule

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/container/watch"
)

// ContainerChanges follows container changes until ctx ends, calling changed
// for each from one goroutine.
type ContainerChanges func(ctx context.Context, changed func(watch.Change))

// Timing of the check of new containers.
const (
	// startedQuietPeriod is how long no new container may start before the
	// new ones are checked, so a Compose project brought up together is
	// checked in one session.
	startedQuietPeriod = 10 * time.Second
	// startedMaxWait bounds the wait for a quiet period.
	startedMaxWait = time.Minute
	// maxRememberedStarts bounds the containers remembered as checked.
	maxRememberedStarts = 1024
)

// startedChecks checks the containers started between scheduled runs, such
// as one started or recreated with Compose, as soon as they start instead of
// at the next scheduled run.
//
// A container is checked on its first start only, so restarting it, or a
// container that keeps failing, does not check it again. A recreated
// container is a new container, with a new ID, and is checked.
//
// Containers are told apart by ID, as their names change: Compose creates a
// replacement under a temporary name and renames it before it starts.
//
// Each check is one ordinary update session over the containers Watchtower
// manages, with Due naming the new ones, so dependencies work as in a
// scheduled run. A container Watchtower recreates in an update is checked
// once more, which finds it up to date.
type startedChecks struct {
	log         *zerolog.Logger
	run         func(due filters.Filter)
	quiet, most time.Duration
	// seen holds the IDs of the containers already taken for a check, oldest
	// first, at most maxRememberedStarts.
	seen []string
}

// watchStarted checks new containers as changes reports them starting, until
// ctx ends. It returns a function that waits for it to stop. A nil changes
// checks nothing, as does a ctx that has already ended (in API-only mode the
// scheduler is reached only at shutdown).
//
// Parameters:
//   - ctx: Ends the watch.
//   - log: Process logger.
//   - changes: Follows container changes, or nil.
//   - run: Runs one update session for the due containers.
//
// Returns:
//   - func(): Waits for the watch to stop.
func watchStarted(
	ctx context.Context,
	log *zerolog.Logger,
	changes ContainerChanges,
	run func(due filters.Filter),
) func() {
	if changes == nil || ctx.Err() != nil {
		return func() {}
	}

	checks := &startedChecks{log: log, run: run, quiet: startedQuietPeriod, most: startedMaxWait}
	received := make(chan watch.Change)
	done := make(chan struct{})

	go func() {
		changes(ctx, func(change watch.Change) {
			select {
			case received <- change:
			case <-ctx.Done():
			}
		})
	}()

	go func() {
		defer close(done)

		checks.loop(ctx, received)
	}()

	log.Info().Msg("New containers are checked for updates as they start")

	return func() { <-done }
}

// loop collects the started containers and checks them once no new container
// has started for the quiet period, or the longest wait has passed. Each check
// runs on its own goroutine, waiting for the update lock there, so the events
// that arrive meanwhile are still received when they happen. It returns once
// ctx ends and every check has stopped.
func (c *startedChecks) loop(ctx context.Context, received <-chan watch.Change) {
	var (
		ids, names    []string
		quiet, latest <-chan time.Time
		checks        sync.WaitGroup
	)

	defer checks.Wait()

	for {
		select {
		case <-ctx.Done():
			return
		case change := <-received:
			if c.wanted(change) {
				ids = append(ids, change.ID)
				names = append(names, change.Name)
				quiet = time.After(c.quiet)

				if latest == nil {
					latest = time.After(c.most)
				}
			}

			continue
		case <-quiet:
		case <-latest:
		}

		due := ids

		c.log.Info().Strs("containers", names).Msg("Checking new containers for updates")

		ids, names, quiet, latest = nil, nil, nil, nil

		checks.Go(func() { c.run(dueByID(due)) })
	}
}

// wanted reports whether a change is a container's first start, and takes it
// for a check.
func (c *startedChecks) wanted(change watch.Change) bool {
	if change.Action != events.ActionStart || change.ID == "" {
		return false
	}

	if slices.Contains(c.seen, change.ID) {
		c.log.Debug().Str("container", change.Name).Msg("Container restarted: already checked")

		return false
	}

	// The previous Watchtower container of a self-update.
	if container.IsOldContainer(change.Name) {
		return false
	}

	c.seen = append(c.seen, change.ID)
	if len(c.seen) > maxRememberedStarts {
		c.seen = c.seen[1:]
	}

	c.log.Debug().Str("container", change.Name).Msg("Container started: checking it for updates shortly")

	return true
}

// identified is a container that has an ID.
type identified interface {
	ID() container.ContainerID
}

// dueByID returns a Due filter accepting the containers with the given IDs,
// whatever they are named now, except Watchtower itself.
func dueByID(ids []string) filters.Filter {
	return func(candidate filters.FilterableContainer) bool {
		withID, ok := candidate.(identified)

		return ok && !candidate.IsWatchtower() && slices.Contains(ids, string(withID.ID()))
	}
}
