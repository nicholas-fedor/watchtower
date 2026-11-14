package schedule

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
)

// ListContainers lists every container, for container schedules to read their
// schedule labels.
type ListContainers func(ctx context.Context) ([]filters.FilterableContainer, error)

// cronParseOptions is the cron syntax of the global schedule and of the
// schedule label alike.
const cronParseOptions = cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom |
	cron.Month | cron.Dow | cron.Descriptor

// globalSchedule names the global schedule among the schedules that fire.
const globalSchedule = ""

// maxClaimedTicks bounds the fire times remembered as already run.
const maxClaimedTicks = 16

// containerSchedules gives each container with a schedule label its own cron
// entry, one per distinct schedule, beside the global one.
//
// The entries follow the labels Watchtower last read: from its own listing at
// startup, then from the containers each update session lists anyway. Nothing
// else lists or watches the containers, so a new container's schedule starts
// after the next session.
//
// Every update runs as one ordinary session over the containers Watchtower
// manages, which keeps dependency ordering and the restart of dependents as
// in a global run. The session's Due filter names the containers checked for
// updates: those whose schedule fired. Schedules that fire at the same moment
// share one session.
//
// A nil *containerSchedules is the option turned off: every method then does
// nothing.
type containerSchedules struct {
	log       *zerolog.Logger
	list      ListContainers
	base      filters.Filter
	scheduler *cron.Cron
	run       func(due filters.Filter)
	parser    cron.Parser

	// entries maps each cron entry to its schedule: the global schedule's
	// entry to globalSchedule.
	entries map[cron.EntryID]string
	// bySpec is the entry of each container schedule.
	bySpec map[string]cron.EntryID
	// claimed holds the fire times whose session has started.
	claimed []time.Time
	// warned is the invalid label each container, by name, was last warned
	// about.
	warned map[string]string
	mu     sync.Mutex
}

// newContainerSchedules returns container schedules adding their entries to
// scheduler, or nil when list is nil (the option off). base is the
// containers Watchtower manages. run starts one update session for the due
// containers.
func newContainerSchedules(
	log *zerolog.Logger,
	list ListContainers,
	base filters.Filter,
	scheduler *cron.Cron,
	run func(due filters.Filter),
) *containerSchedules {
	if list == nil {
		return nil
	}

	if base == nil {
		base = filters.NoFilter
	}

	return &containerSchedules{
		log:       log,
		list:      list,
		base:      base,
		scheduler: scheduler,
		run:       run,
		parser:    cron.NewParser(cronParseOptions),
		entries:   make(map[cron.EntryID]string),
		bySpec:    make(map[string]cron.EntryID),
		warned:    make(map[string]string),
	}
}

// addGlobal adds the global schedule's entry. Its runs check the containers
// without a schedule of their own.
func (c *containerSchedules) addGlobal(spec string) error {
	entryID, err := c.scheduler.AddFunc(spec, c.job())
	if err != nil {
		return err //nolint:wrapcheck // the caller wraps it as it wraps the global schedule's error
	}

	c.mu.Lock()
	c.entries[entryID] = globalSchedule
	c.mu.Unlock()

	return nil
}

// addSchedule adds the global schedule's entry to scheduler: through
// container schedules when they are on, so it shares its sessions with them.
//
// Parameters:
//   - scheduler: The scheduler.
//   - containers: Container schedules, or nil when off.
//   - spec: The global schedule.
//   - run: Runs a session for the global schedule alone.
//
// Returns:
//   - error: Non-nil when spec does not parse.
func addSchedule(scheduler *cron.Cron, containers *containerSchedules, spec string, run func()) error {
	if containers != nil {
		return containers.addGlobal(spec)
	}

	_, err := scheduler.AddFunc(spec, run)

	return err //nolint:wrapcheck // the caller wraps it
}

// globalDue returns the Due filter of a run of the global schedule alone, such
// as the update at startup.
func (c *containerSchedules) globalDue() filters.Filter {
	if c == nil {
		return nil
	}

	return c.dueOn(map[string]bool{globalSchedule: true})
}

// refresh lists the containers and reads their schedule labels, at startup
// before any session has run.
func (c *containerSchedules) refresh(ctx context.Context) {
	if c == nil || ctx.Err() != nil {
		return
	}

	containers, err := c.list(ctx)
	if err != nil {
		c.log.Warn().Err(err).Msg("Could not read container schedules: keeping the schedules in use")

		return
	}

	c.read(containers)
}

// listed returns the Listed parameter of a session: it reads the
// schedule labels of the containers the session lists. It returns nil when
// container schedules are off.
func (c *containerSchedules) listed() func([]filters.FilterableContainer) {
	if c == nil {
		return nil
	}

	return c.read
}

// read reads the containers' schedule labels and adds or removes the entries
// so there is one for each schedule in use.
func (c *containerSchedules) read(containers []filters.FilterableContainer) {
	inUse := make(map[string]bool)

	for _, listed := range containers {
		if !c.base(listed) {
			continue
		}

		if spec, own := c.spec(listed); own {
			inUse[spec] = true
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for spec, entryID := range c.bySpec {
		if inUse[spec] {
			continue
		}

		c.scheduler.Remove(entryID)
		delete(c.bySpec, spec)
		delete(c.entries, entryID)
		c.log.Debug().Str("schedule", spec).Msg("No container follows this schedule any more")
	}

	for spec := range inUse {
		if _, ok := c.bySpec[spec]; ok {
			continue
		}

		entryID, err := c.scheduler.AddFunc(spec, c.job())
		if err != nil {
			continue // spec parsed above, so this does not happen
		}

		c.bySpec[spec] = entryID
		c.entries[entryID] = spec
		c.log.Info().Str("schedule", spec).Msg("Containers follow their own schedule")
	}
}

// job returns the function of a cron entry: it runs a session for every
// entry that fired at the same moment, unless one of them already has.
func (c *containerSchedules) job() func() {
	return func() {
		fired, ok := c.claim()
		if !ok {
			return
		}

		c.run(c.dueOn(fired))
	}
}

// claim returns the schedules that fired at the latest fire time, and true
// when this call is the first for that time.
//
// The scheduler starts every entry that is due before it answers for its
// entries again, so all the entries that fired with this one show the same
// previous run time.
func (c *containerSchedules) claim() (map[string]bool, bool) {
	entries := c.scheduler.Entries()

	var latest time.Time

	for _, entry := range entries {
		if entry.Prev.After(latest) {
			latest = entry.Prev
		}
	}

	if latest.IsZero() {
		return nil, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, claimed := range c.claimed {
		if claimed.Equal(latest) {
			return nil, false
		}
	}

	c.claimed = append(c.claimed, latest)
	if len(c.claimed) > maxClaimedTicks {
		c.claimed = c.claimed[1:]
	}

	fired := make(map[string]bool)

	for _, entry := range entries {
		spec, known := c.entries[entry.ID]
		if known && entry.Prev.Equal(latest) {
			fired[spec] = true
		}
	}

	names := make([]string, 0, len(fired))
	for spec := range fired {
		if spec == globalSchedule {
			spec = "global"
		}

		names = append(names, spec)
	}

	c.log.Debug().Strs("schedules", names).Msg("Running scheduled update")

	return fired, len(fired) > 0
}

// dueOn returns the Due filter of a session for the schedules that fired: a
// container is due when its own schedule fired, or when it has none and the
// global schedule fired.
func (c *containerSchedules) dueOn(fired map[string]bool) filters.Filter {
	return func(candidate filters.FilterableContainer) bool {
		spec, own := c.spec(candidate)
		if !own {
			spec = globalSchedule
		}

		return fired[spec]
	}
}

// spec returns the container's own schedule: its schedule label, trimmed of
// spaces and quotes, when set and valid. A container whose label does not
// parse follows the global schedule, and is warned about once.
func (c *containerSchedules) spec(candidate filters.FilterableContainer) (string, bool) {
	raw, ok := candidate.GetLabel(labels.Schedule)
	if !ok {
		return "", false
	}

	spec := strings.Trim(strings.TrimSpace(raw), `"'`)
	if spec == "" {
		return "", false
	}

	_, err := c.parser.Parse(spec)
	if err == nil {
		return spec, true
	}

	c.mu.Lock()
	warn := c.warned[candidate.Name()] != spec
	c.warned[candidate.Name()] = spec
	c.mu.Unlock()

	if warn {
		c.log.Warn().Err(err).
			Str("container", candidate.Name()).
			Str("schedule", spec).
			Msg("Ignoring invalid schedule label: the container follows the global schedule")
	}

	return "", false
}

// ContainerLister is the part of the container client that container
// schedules use.
type ContainerLister interface {
	ListContainers(ctx context.Context, filter ...filters.Filter) ([]*container.Container, error)
}

// ListFrom returns the [ListContainers] of container schedules when enabled
// (--container-schedules), or nil to keep every container on the global
// schedule.
//
// Parameters:
//   - enabled: Whether containers may follow their own schedule label.
//   - client: Container client that lists the containers.
//
// Returns:
//   - ListContainers: Lists every container through client, or nil.
func ListFrom(enabled bool, client ContainerLister) ListContainers {
	if !enabled {
		return nil
	}

	return func(ctx context.Context) ([]filters.FilterableContainer, error) {
		listed, err := client.ListContainers(ctx, filters.NoFilter)
		if err != nil {
			return nil, fmt.Errorf("list containers: %w", err)
		}

		found := make([]filters.FilterableContainer, 0, len(listed))
		for _, c := range listed {
			found = append(found, c)
		}

		return found, nil
	}
}
