package session

import (
	"sort"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// report implements the Report interface for session results.
type report struct {
	scanned []types.ContainerReport // Scanned containers.
	updated []types.ContainerReport // Updated containers.
	failed  []types.ContainerReport // Failed containers.
	skipped []types.ContainerReport // Skipped containers.
	stale   []types.ContainerReport // Stale containers.
	fresh   []types.ContainerReport // Fresh containers.
}

// Scanned returns scanned containers.
//
// Returns:
//   - []types.ContainerReport: Scanned list.
func (r *report) Scanned() []types.ContainerReport {
	return r.scanned
}

// Updated returns updated containers.
//
// Returns:
//   - []types.ContainerReport: Updated list.
func (r *report) Updated() []types.ContainerReport {
	return r.updated
}

// Failed returns failed containers.
//
// Returns:
//   - []types.ContainerReport: Failed list.
func (r *report) Failed() []types.ContainerReport {
	return r.failed
}

// Skipped returns skipped containers.
//
// Returns:
//   - []types.ContainerReport: Skipped list.
func (r *report) Skipped() []types.ContainerReport {
	return r.skipped
}

// Stale returns stale containers.
//
// Returns:
//   - []types.ContainerReport: Stale list.
func (r *report) Stale() []types.ContainerReport {
	return r.stale
}

// Fresh returns fresh containers.
//
// Returns:
//   - []types.ContainerReport: Fresh list.
func (r *report) Fresh() []types.ContainerReport {
	return r.fresh
}

// All returns deduplicated containers, prioritized by state.
//
// Returns:
//   - []types.ContainerReport: Sorted, unique list.
func (r *report) All() []types.ContainerReport {
	// Calculate total capacity for all containers.
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
	)
	all := make([]types.ContainerReport, 0, allLen)
	presentIDs := map[types.ContainerID][]string{}

	// Append unique containers in priority order.
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
	appendUnique(r.failed)
	appendUnique(r.skipped)
	appendUnique(r.stale)
	appendUnique(r.fresh)
	appendUnique(r.scanned)

	sort.Sort(sortableContainers(all))

	return all
}

// NewReport creates a report from progress data.
//
// Parameters:
//   - progress: Progress map to process.
//
// Returns:
//   - types.Report: Categorized and sorted report.
func NewReport(progress Progress) types.Report {
	report := &report{
		scanned: make([]types.ContainerReport, 0, len(progress)),
		updated: make([]types.ContainerReport, 0),
		failed:  make([]types.ContainerReport, 0),
		skipped: make([]types.ContainerReport, 0),
		stale:   make([]types.ContainerReport, 0),
		fresh:   make([]types.ContainerReport, 0),
	}

	// Categorize each container status.
	for _, update := range progress {
		categorizeContainer(report, update)
	}

	// Sort all categories by ID.
	sortCategories(report)

	return report
}

// categorizeContainer assigns a status to report categories.
//
// Parameters:
//   - report: Report to update.
//   - update: Container status to categorize.
func categorizeContainer(report *report, update *ContainerStatus) {
	if update.state == SkippedState {
		report.skipped = append(report.skipped, update)

		return
	}

	// Add non-skipped to scanned list.
	report.scanned = append(report.scanned, update)

	// Categorize based on image or state.
	if update.newImage == update.oldImage {
		update.state = FreshState
		report.fresh = append(report.fresh, update)

		return
	}

	// Handle explicit states.
	//nolint:exhaustive // Other states handled above.
	switch update.state {
	case UpdatedState:
		report.updated = append(report.updated, update)
	case FailedState:
		report.failed = append(report.failed, update)
	case StaleState:
		report.stale = append(report.stale, update)
	default:
		update.state = StaleState
		report.stale = append(report.stale, update)
	}
}

// sortCategories sorts all report categories by container ID.
//
// Parameters:
//   - report: Report to sort.
func sortCategories(report *report) {
	sort.Sort(sortableContainers(report.scanned))
	sort.Sort(sortableContainers(report.updated))
	sort.Sort(sortableContainers(report.failed))
	sort.Sort(sortableContainers(report.skipped))
	sort.Sort(sortableContainers(report.stale))
	sort.Sort(sortableContainers(report.fresh))
}

// sortableContainers implements sort.Interface for reports.
type sortableContainers []types.ContainerReport

// Len returns the slice length.
//
// Returns:
//   - int: Number of reports.
func (s sortableContainers) Len() int {
	return len(s)
}

// Less compares container IDs.
//
// Parameters:
//   - i, j: Indices to compare.
//
// Returns:
//   - bool: True if i’s ID is less than j’s.
func (s sortableContainers) Less(i, j int) bool {
	return s[i].ID() < s[j].ID()
}

// Swap exchanges two reports.
//
// Parameters:
//   - i, j: Indices to swap.
func (s sortableContainers) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
