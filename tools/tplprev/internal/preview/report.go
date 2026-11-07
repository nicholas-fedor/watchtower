package preview

import (
	"sort"

	"github.com/nicholas-fedor/tplprev/internal/report"
)

// State is the outcome of a container in a session report.
type State string

const (
	ScannedState   State = "scanned"
	UpdatedState   State = "updated"
	FailedState    State = "failed"
	SkippedState   State = "skipped"
	RestartedState State = "restarted"
	StaleState     State = "stale"
	FreshState     State = "fresh"
)

var _ report.Report = (*sessionReport)(nil)

// StatesFromString parses a string of state characters and returns a slice of the corresponding report states.
//
// Parameters:
//   - str: Compact state characters (c, u, e, k, r, t, f).
//
// Returns:
//   - []State: Parsed report states. Unknown characters are skipped.
func StatesFromString(str string) []State {
	states := make([]State, 0, len(str))

	for _, char := range str {
		switch char {
		case 'c':
			states = append(states, ScannedState)
		case 'u':
			states = append(states, UpdatedState)
		case 'e':
			states = append(states, FailedState)
		case 'k':
			states = append(states, SkippedState)
		case 'r':
			states = append(states, RestartedState)
		case 't':
			states = append(states, StaleState)
		case 'f':
			states = append(states, FreshState)
		default:
			continue
		}
	}

	return states
}

// sessionReport is the preview's report of a whole update session.
type sessionReport struct {
	scanned   []report.ContainerReport // Scanned containers.
	updated   []report.ContainerReport // Updated containers.
	failed    []report.ContainerReport // Failed containers.
	skipped   []report.ContainerReport // Skipped containers.
	stale     []report.ContainerReport // Stale containers.
	fresh     []report.ContainerReport // Fresh containers.
	restarted []report.ContainerReport // Restarted containers (linked dependencies).
}

// Scanned returns the scanned containers.
func (r *sessionReport) Scanned() []report.ContainerReport {
	return r.scanned
}

// Updated returns the updated containers.
func (r *sessionReport) Updated() []report.ContainerReport {
	return r.updated
}

// Failed returns the containers that failed to update.
func (r *sessionReport) Failed() []report.ContainerReport {
	return r.failed
}

// Skipped returns the skipped containers.
func (r *sessionReport) Skipped() []report.ContainerReport {
	return r.skipped
}

// Stale returns the containers with a newer image that was not applied.
func (r *sessionReport) Stale() []report.ContainerReport {
	return r.stale
}

// Fresh returns the containers already on their latest image.
func (r *sessionReport) Fresh() []report.ContainerReport {
	return r.fresh
}

// Restarted returns the containers restarted with an updated dependency.
func (r *sessionReport) Restarted() []report.ContainerReport {
	return r.restarted
}

// All returns each container of the report once, sorted by container ID.
//
// A container listed in more than one category appears with its most
// significant one.
func (r *sessionReport) All() []report.ContainerReport {
	allLen := len(r.scanned) + len(r.updated) + len(r.failed) + len(r.skipped) +
		len(r.stale) + len(r.fresh) + len(r.restarted)
	all := make([]report.ContainerReport, 0, allLen)

	presentIDs := map[report.ContainerID][]string{}

	appendUnique := func(reports []report.ContainerReport) {
		for _, item := range reports {
			_, found := presentIDs[item.ID()]
			if found {
				continue
			}

			all = append(all, item)
			presentIDs[item.ID()] = nil
		}
	}

	appendUnique(r.updated)
	appendUnique(r.restarted)
	appendUnique(r.failed)
	appendUnique(r.skipped)
	appendUnique(r.stale)
	appendUnique(r.fresh)
	appendUnique(r.scanned)

	sort.Sort(sortableContainers(all))

	return all
}

// Focus returns nil, since the preview renders the report of a whole session.
func (r *sessionReport) Focus() report.ContainerReport {
	return nil
}

// sortableContainers sorts container reports by container ID.
type sortableContainers []report.ContainerReport

// Len returns the number of reports.
func (s sortableContainers) Len() int { return len(s) }

// Less reports whether report i's container ID sorts before report j's.
func (s sortableContainers) Less(i, j int) bool { return s[i].ID() < s[j].ID() }

// Swap exchanges reports i and j.
func (s sortableContainers) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
