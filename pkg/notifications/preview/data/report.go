package data

import (
	"sort"

	"github.com/nicholas-fedor/watchtower/pkg/types"
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

// StatesFromString parses a string of state characters and returns a slice of the corresponding report states.
func StatesFromString(str string) []State {
	states := make([]State, 0, len(str))

	for _, c := range str {
		switch c {
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

type report struct {
	scanned   []types.ContainerReport
	updated   []types.ContainerReport
	failed    []types.ContainerReport
	skipped   []types.ContainerReport
	stale     []types.ContainerReport
	fresh     []types.ContainerReport
	restarted []types.ContainerReport
}

func (r *report) Scanned() []types.ContainerReport {
	return r.scanned
}

func (r *report) Updated() []types.ContainerReport {
	return r.updated
}

func (r *report) Failed() []types.ContainerReport {
	return r.failed
}

func (r *report) Skipped() []types.ContainerReport {
	return r.skipped
}

func (r *report) Stale() []types.ContainerReport {
	return r.stale
}

func (r *report) Fresh() []types.ContainerReport {
	return r.fresh
}

func (r *report) Restarted() []types.ContainerReport {
	return r.restarted
}

func (r *report) All() []types.ContainerReport {
	allLen := len(
		r.scanned,
	) + len(
		r.updated,
	) + len(
		r.failed,
	) + len(
		r.skipped,
	) + len(
		r.stale,
	) + len(
		r.fresh,
	) + len(
		r.restarted,
	)
	all := make([]types.ContainerReport, 0, allLen)

	presentIDs := map[types.ContainerID][]string{}

	appendUnique := func(reports []types.ContainerReport) {
		for _, report := range reports {
			if _, found := presentIDs[report.ID()]; found {
				continue
			}

			all = append(all, report)
			presentIDs[report.ID()] = nil
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

// Filter returns a new report containing only containers that pass the provided filter.
func (r *report) Filter(filter types.Filter) types.Report {
	filtered := &report{
		scanned:   filterContainers(r.scanned, filter),
		updated:   filterContainers(r.updated, filter),
		failed:    filterContainers(r.failed, filter),
		skipped:   filterContainers(r.skipped, filter),
		stale:     filterContainers(r.stale, filter),
		fresh:     filterContainers(r.fresh, filter),
		restarted: filterContainers(r.restarted, filter),
	}

	return filtered
}

type sortableContainers []types.ContainerReport

func (s sortableContainers) Len() int { return len(s) }

func (s sortableContainers) Less(i, j int) bool { return s[i].ID() < s[j].ID() }

func (s sortableContainers) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

// containerReportAdapter adapts a ContainerReport to FilterableContainer for filtering.
type containerReportAdapter struct {
	report types.ContainerReport
}

func (a *containerReportAdapter) Name() string {
	return a.report.Name()
}

func (a *containerReportAdapter) IsWatchtower() bool {
	return false // Reports don't have watchtower status
}

func (a *containerReportAdapter) Enabled() (bool, bool) {
	return false, false // Reports don't have enable status
}

func (a *containerReportAdapter) Scope() (string, bool) {
	return "", false // Reports don't have scope
}

func (a *containerReportAdapter) ImageName() string {
	return a.report.ImageName()
}

// filterContainers applies a filter to a slice of container reports.
func filterContainers(
	containers []types.ContainerReport,
	filter types.Filter,
) []types.ContainerReport {
	if filter == nil {
		return containers
	}

	filtered := make([]types.ContainerReport, 0, len(containers))
	for _, container := range containers {
		adapter := &containerReportAdapter{report: container}
		if filter(adapter) {
			filtered = append(filtered, container)
		}
	}

	return filtered
}
