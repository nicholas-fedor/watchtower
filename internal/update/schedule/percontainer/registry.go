package percontainer

import (
	"sort"
	"sync"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
)

// Timers adds and removes the scheduler's cron entries, one per schedule.
type Timers interface {
	// Add schedules a run of the containers on spec.
	Add(spec string) (cron.EntryID, error)
	// Remove takes a schedule's entry out again.
	Remove(id cron.EntryID)
}

// Sink is told which containers carry which schedule. A [Source] reports to it.
type Sink interface {
	// Reset replaces everything known with this snapshot: container name to
	// schedule, for every container that has one. A source sends it on
	// connecting, and again after reconnecting.
	Reset(schedules map[string]string)
	// Set records a container's schedule. An empty spec means it has none.
	Set(name, spec string)
	// Remove forgets a container.
	Remove(name string)
}

// Registry keeps one cron entry for each schedule in use, counting the
// containers on it. It is safe for concurrent use.
type Registry struct {
	timers  Timers
	log     *zerolog.Logger
	byName  map[string]string
	entries map[string]*entry
	mu      sync.Mutex
}

type entry struct {
	id         cron.EntryID
	containers int
}

var _ Sink = (*Registry)(nil)

// NewRegistry returns an empty registry adding its entries to timers.
func NewRegistry(log *zerolog.Logger, timers Timers) *Registry {
	if log == nil {
		nop := zerolog.Nop()
		log = &nop
	}

	return &Registry{
		log:     log,
		timers:  timers,
		byName:  make(map[string]string),
		entries: make(map[string]*entry),
	}
}

// Reset implements [Sink].
func (r *Registry) Reset(schedules map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name := range r.byName {
		if _, still := schedules[name]; !still {
			r.applyLocked(name, "")
		}
	}

	for name, spec := range schedules {
		r.applyLocked(name, spec)
	}
}

// Set implements [Sink].
func (r *Registry) Set(name, spec string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.applyLocked(name, spec)
}

// Remove implements [Sink].
func (r *Registry) Remove(name string) {
	r.Set(name, "")
}

// Schedules returns the schedules in use, sorted.
func (r *Registry) Schedules() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	specs := make([]string, 0, len(r.entries))
	for spec := range r.entries {
		specs = append(specs, spec)
	}

	sort.Strings(specs)

	return specs
}

// applyLocked moves a container to spec ("" for none): the schedule it leaves
// loses its entry when no container is left on it, and a schedule gains one
// with its first container. A schedule that does not parse is ignored (the
// container follows the global schedule), as the run filters treat it.
func (r *Registry) applyLocked(name, spec string) {
	spec = Normalize(spec)
	if spec != "" && !Valid(spec) {
		r.log.Warn().
			Str("container", name).
			Str("schedule", spec).
			Msg("Ignoring an invalid schedule label: the container follows the global schedule")

		spec = ""
	}

	old, known := r.byName[name]
	if known && old == spec {
		return
	}

	if known {
		r.releaseLocked(old)
		delete(r.byName, name)
	}

	if spec == "" {
		if known {
			r.log.Debug().
				Str("container", name).
				Str("was", old).
				Msg("Container left its own schedule")
		}

		return
	}

	if !r.holdLocked(spec) {
		return
	}

	r.byName[name] = spec

	r.log.Debug().
		Str("container", name).
		Str("schedule", spec).
		Msg("Container follows its own schedule")
}

func (r *Registry) holdLocked(spec string) bool {
	if e, ok := r.entries[spec]; ok {
		e.containers++

		return true
	}

	entryID, err := r.timers.Add(spec)
	if err != nil {
		r.log.Warn().Err(err).Str("schedule", spec).Msg("Could not schedule a container schedule")

		return false
	}

	r.entries[spec] = &entry{id: entryID, containers: 1}

	r.log.Info().Str("schedule", spec).Msg("Scheduled updates for containers with their own schedule")

	return true
}

func (r *Registry) releaseLocked(spec string) {
	held, ok := r.entries[spec]
	if !ok {
		return
	}

	held.containers--
	if held.containers > 0 {
		return
	}

	r.timers.Remove(held.id)
	delete(r.entries, spec)

	r.log.Info().Str("schedule", spec).Msg("No containers left on a container schedule: removed it")
}
