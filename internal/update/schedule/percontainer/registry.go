package percontainer

import (
	"slices"
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
	// Remove forgets a container. With an ID, only that container: another
	// seen under the same name (its replacement) is kept.
	Remove(name, containerID string)
}

// held is what the registry knows of a container name: its schedule, and the
// IDs of the containers seen under that name that haven't been removed (a
// replacement and the container it replaced can both be in flight while
// their events arrive).
type held struct {
	spec string
	ids  []string
}

// has reports whether the ID is among those seen under the name.
func (h held) has(containerID string) bool {
	return slices.Contains(h.ids, containerID)
}

// maxIDs is the most container IDs remembered under one name: a replacement
// in flight, and some slack.
const maxIDs = 4

// Registry keeps one cron entry for each schedule in use, counting the
// containers on it. It is safe for concurrent use.
type Registry struct {
	timers  Timers
	log     *zerolog.Logger
	byName  map[string]held
	entries map[string]*entry
	// warned is the invalid label each container was last warned about (with
	// the container's ID, so a replacement is warned about too), so a label
	// read again on every refresh is warned about once.
	warned map[string]string
	// While a snapshot is applied, schedules left are released only at its
	// end: one still in use keeps its entry and next run, however the
	// containers on it moved (two swapping schedules, say).
	deferred  []string
	deferring bool
	mu        sync.Mutex
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
		byName:  make(map[string]held),
		entries: make(map[string]*entry),
		warned:  make(map[string]string),
	}
}

// Reset implements [Sink]. Every schedule the snapshot uses is taken up
// before any is let go, so a schedule still in use keeps its cron entry (and
// its next run) however the containers on it changed. The snapshot's IDs
// replace those remembered: it's what Docker has now.
func (r *Registry) Reset(containers map[string]Labeled) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.deferring = true

	for name, labeled := range containers {
		r.applyLocked(name, labeled.ID, labeled.Spec)

		if current, ok := r.byName[name]; ok {
			current.ids = nil
			if labeled.ID != "" {
				current.ids = []string{labeled.ID}
			}

			r.byName[name] = current
		}
	}

	for name := range r.byName {
		if _, still := containers[name]; !still {
			r.applyLocked(name, "", "")
		}
	}

	r.deferring = false
	for _, spec := range r.deferred {
		r.releaseLocked(spec)
	}

	r.deferred = r.deferred[:0]

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

// Remove implements [Sink]. With an ID, only that container goes: the name
// keeps its schedule while another container seen under it remains (its
// replacement), and the news of one not seen there is ignored.
func (r *Registry) Remove(name, containerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if current, known := r.byName[name]; known && containerID != "" && len(current.ids) > 0 {
		if !current.has(containerID) {
			r.log.Debug().
				Str("container", name).
				Str("removed_id", shortID(containerID)).
				Msg("Ignoring the removal of a container not seen under this name (already replaced)")

			return
		}

		current.ids = slices.DeleteFunc(current.ids, func(id string) bool { return id == containerID })
		if len(current.ids) > 0 {
			r.byName[name] = current

			r.log.Debug().
				Str("container", name).
				Str("removed_id", shortID(containerID)).
				Msg("A replaced container was removed: its replacement keeps the schedule")

			return
		}
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
		if seen := spec + "\x00" + containerID; r.warned[name] != seen {
			r.warned[name] = seen

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
	if known && old.spec == spec {
		// The same schedule (perhaps a new container under the same name, while
		// the one it replaces is still to be removed): its entry and its next
		// run stay as they are, and the container's ID is remembered
		if containerID != "" && !old.has(containerID) {
			old.ids = append(old.ids, containerID)
			if len(old.ids) > maxIDs {
				old.ids = old.ids[len(old.ids)-maxIDs:]
			}

			r.byName[name] = old
		}

		return
	}

	if spec == "" {
		if known {
			delete(r.byName, name)
			r.releaseLocked(old.spec)

			r.log.Debug().
				Str("container", name).
				Str("was", old.spec).
				Msg("Container left its own schedule")
		}

		return
	}

	// (onto the new schedule before off the old, though they differ: a
	// container moving never leaves a gap)
	if !r.holdLocked(spec) {
		if known {
			delete(r.byName, name)
			r.releaseLocked(old.spec)
		}

		return
	}

	var ids []string
	if containerID != "" {
		ids = []string{containerID}
	}

	r.byName[name] = held{spec: spec, ids: ids}

	if known {
		r.releaseLocked(old.spec)
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
// with the last (at the end of a snapshot, while one is applied).
func (r *Registry) releaseLocked(spec string) {
	if r.deferring {
		r.deferred = append(r.deferred, spec)

		return
	}

	schedule, ok := r.entries[spec]
	if !ok {
		return
	}

	schedule.containers--
	if schedule.containers > 0 {
		return
	}

	r.timers.Remove(schedule.id)
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
