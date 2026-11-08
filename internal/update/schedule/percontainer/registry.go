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

// Labeled is a container's schedule label as a [Source] reports it: the
// container's ID (so news of a container that has since been replaced under
// the same name can be told apart) and the label's value, normalized. The
// value may not parse; the [Sink] decides what to do with it.
type Labeled struct {
	ID   string
	Spec string
}

// Sink is told which containers carry which schedule. A [Source] reports to it.
type Sink interface {
	// Reset replaces everything known with this snapshot: container name to
	// its label, for every container that has one. A source sends it on
	// connecting, again after reconnecting, and after each update run.
	Reset(containers map[string]Labeled)
	// Set records a container's schedule. An empty spec means it has none.
	// The ID may be empty when it isn't known.
	Set(name, containerID, spec string)
	// Remove forgets a container. With an ID, only that container: a later
	// one under the same name is kept.
	Remove(name, containerID string)
}

// Registry keeps one cron entry for each schedule in use, counting the
// containers on it. It is safe for concurrent use.
type Registry struct {
	timers  Timers
	log     *zerolog.Logger
	byName  map[string]Labeled
	entries map[string]*entry
	// warned is the invalid label each container was last warned about, so a
	// label read again on every refresh is warned about once.
	warned map[string]string
	mu     sync.Mutex
}

// entry is a schedule's cron entry and the number of containers on it.
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
		byName:  make(map[string]Labeled),
		entries: make(map[string]*entry),
		warned:  make(map[string]string),
	}
}

// Reset implements [Sink]. The containers in the snapshot are applied before
// those missing from it are removed, so a schedule that is still in use keeps
// its cron entry (and its next run) even when the containers on it changed.
func (r *Registry) Reset(containers map[string]Labeled) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, labeled := range containers {
		r.applyLocked(name, labeled.ID, labeled.Spec)
	}

	for name := range r.byName {
		if _, still := containers[name]; !still {
			r.applyLocked(name, "", "")
		}
	}

	for name := range r.warned {
		if _, still := containers[name]; !still {
			delete(r.warned, name)
		}
	}
}

// Set implements [Sink].
func (r *Registry) Set(name, containerID, spec string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.applyLocked(name, containerID, spec)
}

// Remove implements [Sink]. News of a container that has since been replaced
// under the same name (its ID differs from the one recorded) is ignored.
func (r *Registry) Remove(name, containerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if held, known := r.byName[name]; known && containerID != "" && held.ID != "" && held.ID != containerID {
		r.log.Debug().
			Str("container", name).
			Str("removed_id", shortID(containerID)).
			Str("current_id", shortID(held.ID)).
			Msg("Ignoring the removal of a container already replaced under the same name")

		return
	}

	delete(r.warned, name)
	r.applyLocked(name, "", "")
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
// container follows the global schedule), as the run filters treat it, with a
// warning the first time it's seen on the container.
func (r *Registry) applyLocked(name, containerID, spec string) {
	spec = Normalize(spec)
	if spec != "" && !Valid(spec) {
		if r.warned[name] != spec {
			r.warned[name] = spec

			r.log.Warn().
				Str("container", name).
				Str("schedule", spec).
				Msg("Ignoring an invalid schedule label: the container follows the global schedule")
		}

		spec = ""
	} else if spec != "" {
		delete(r.warned, name)
	}

	old, known := r.byName[name]
	if known && old.Spec == spec {
		// The same schedule (perhaps a new container under the same name): its
		// entry and its next run stay as they are
		if containerID != "" && containerID != old.ID {
			r.byName[name] = Labeled{ID: containerID, Spec: spec}
		}

		return
	}

	if spec == "" {
		if known {
			delete(r.byName, name)
			r.releaseLocked(old.Spec)

			r.log.Debug().
				Str("container", name).
				Str("was", old.Spec).
				Msg("Container left its own schedule")
		}

		return
	}

	// (onto the new schedule before off the old, though they differ: a
	// container moving never leaves a gap)
	if !r.holdLocked(spec) {
		if known {
			delete(r.byName, name)
			r.releaseLocked(old.Spec)
		}

		return
	}

	r.byName[name] = Labeled{ID: containerID, Spec: spec}

	if known {
		r.releaseLocked(old.Spec)
	}

	r.log.Debug().
		Str("container", name).
		Str("schedule", spec).
		Msg("Container follows its own schedule")
}

// holdLocked counts one more container on spec, adding its cron entry with
// the first. It reports whether the schedule is held (adding can fail).
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

// releaseLocked counts one container fewer on spec, removing its cron entry
// with the last.
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

// shortID is a container ID as Docker prints it: its first twelve characters.
func shortID(containerID string) string {
	const short = 12
	if len(containerID) > short {
		return containerID[:short]
	}

	return containerID
}
