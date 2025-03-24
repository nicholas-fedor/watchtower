package session

import (
	"sort"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Implements Report type interface.
type report struct {
	scanned []types.ContainerReport
	updated []types.ContainerReport
	failed  []types.ContainerReport
	skipped []types.ContainerReport
	stale   []types.ContainerReport
	fresh   []types.ContainerReport
}

// Scanned returns containers scanned during the session.
func (r *report) Scanned() []types.ContainerReport {
	return r.scanned
}

// Updated returns containers updated during the session.
func (r *report) Updated() []types.ContainerReport {
	return r.updated
}

// Failed returns containers that failed during the session.
func (r *report) Failed() []types.ContainerReport {
	return r.failed
}

// Skipped returns containers skipped during the session.
func (r *report) Skipped() []types.ContainerReport {
	return r.skipped
}

// Stale returns containers marked as stale during the session.
func (r *report) Stale() []types.ContainerReport {
	return r.stale
}

// Fresh returns containers marked as fresh during the session.
func (r *report) Fresh() []types.ContainerReport {
	return r.fresh
}

// Deduplicates by container ID, prioritizing updated, failed, skipped, stale, fresh, then scanned.
func (r *report) All() []types.ContainerReport {
	allLen := len(r.scanned) + len(r.updated) + len(r.failed) + len(r.skipped) + len(r.stale) + len(r.fresh)
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
	appendUnique(r.failed)
	appendUnique(r.skipped)
	appendUnique(r.stale)
	appendUnique(r.fresh)
	appendUnique(r.scanned)

	sort.Sort(sortableContainers(all))

	return all
}

// NewReport creates a new Report from a Progress instance, categorizing and sorting container statuses.
// It processes each container in the progress map, assigns them to appropriate categories (scanned,
// updated, failed, skipped, stale, fresh), and ensures each category is sorted by container ID.
// Non-skipped containers are added to the scanned list, with further categorization based on their state
// or image comparison.
func NewReport(progress Progress) types.Report {
	report := &report{
		scanned: make([]types.ContainerReport, 0, len(progress)),
		updated: make([]types.ContainerReport, 0),
		failed:  make([]types.ContainerReport, 0),
		skipped: make([]types.ContainerReport, 0),
		stale:   make([]types.ContainerReport, 0),
		fresh:   make([]types.ContainerReport, 0),
	}

	// Categorize all containers from progress
	for _, update := range progress {
		categorizeContainer(report, update)
	}

	// Sort all categories by container ID
	sortCategories(report)

	return report
}

// categorizeContainer assigns a container status to the appropriate report categories based on its state
// and image IDs. Skipped containers go to the skipped list only. Non-skipped containers are added to
// scanned and may also be categorized as fresh, updated, failed, or stale depending on their state
// and whether their images match.
func categorizeContainer(report *report, update *ContainerStatus) {
	if update.state == SkippedState {
		report.skipped = append(report.skipped, update)
		return
	}

	// All non-skipped containers are scanned
	report.scanned = append(report.scanned, update)

	// Categorize based on image comparison or state
	if update.newImage == update.oldImage {
		update.state = FreshState
		report.fresh = append(report.fresh, update)
		return
	}

	// Handle remaining states explicitly
	//nolint:exhaustive // Missing states handled above.
	switch update.state {
	case UpdatedState:
		report.updated = append(report.updated, update)
	case FailedState:
		report.failed = append(report.failed, update)
	case StaleState:
		report.stale = append(report.stale, update)
	default:
		// Default to stale for unhandled or unknown states
		update.state = StaleState
		report.stale = append(report.stale, update)
	}
}

// sortCategories sorts each category in the report by container ID in ascending order.
// This ensures consistent ordering when retrieving containers from the report.
func sortCategories(report *report) {
	sort.Sort(sortableContainers(report.scanned))
	sort.Sort(sortableContainers(report.updated))
	sort.Sort(sortableContainers(report.failed))
	sort.Sort(sortableContainers(report.skipped))
	sort.Sort(sortableContainers(report.stale))
	sort.Sort(sortableContainers(report.fresh))
}

// sortableContainers implements sort.Interface for sorting container reports by ID.
type sortableContainers []types.ContainerReport

// Len returns the length of the container report slice.
func (s sortableContainers) Len() int { return len(s) }

// Less determines if one container report’s ID is less than another’s.
func (s sortableContainers) Less(i, j int) bool { return s[i].ID() < s[j].ID() }

// Swap exchanges two container reports in the slice.
func (s sortableContainers) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
