package state

import (
	"cmp"
	"slices"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container"
)

// Report is the outcome of an update session, with each scanned container
// listed by what happened to it.
//
// [Progress.Report] builds the report of a whole session. When the
// notification-split-by-container configuration option is enabled,
// [NewSingleContainerReport] builds one report per notified container from it.
// Such a report is about that container, its focus. It keeps the session's
// other lists as context.
//
// The method set is part of the notification template contract.
type Report struct {
	focus     *ContainerStatus   // Container a split report is about. Nil for a session report.
	scanned   []*ContainerStatus // Scanned containers.
	updated   []*ContainerStatus // Updated containers.
	failed    []*ContainerStatus // Failed containers.
	skipped   []*ContainerStatus // Skipped containers.
	stale     []*ContainerStatus // Stale containers.
	fresh     []*ContainerStatus // Fresh containers.
	restarted []*ContainerStatus // Restarted containers (linked dependencies).
}

// NewSingleContainerReport builds the split notification report for one
// container of a session.
//
// The updated and restarted lists say what happened to focus. Each holds focus
// when the session lists it there and is empty otherwise. The scanned, failed,
// skipped, stale and fresh lists are the session's, the same context for every
// split notification of the session. They include focus when the session lists
// it in one of them.
//
// Parameters:
//   - focus: The container the notification is about.
//   - session: The full session report.
//
// Returns:
//   - *Report: The report about focus with session context.
func NewSingleContainerReport(focus *ContainerStatus, session *Report) *Report {
	report := &Report{
		focus:   focus,
		scanned: session.scanned,
		failed:  session.failed,
		skipped: session.skipped,
		stale:   session.stale,
		fresh:   session.fresh,
	}

	// The session already sorted focus into one category.
	switch {
	case slices.Contains(session.updated, focus):
		report.updated = []*ContainerStatus{focus}
	case slices.Contains(session.restarted, focus):
		report.restarted = []*ContainerStatus{focus}
	default:
		// Focus is not updated or restarted. The session's context lists hold it
		// when the session lists it at all.
	}

	return report
}

// Scanned returns scanned containers.
//
// Returns:
//   - []*ContainerStatus: Scanned list.
func (r *Report) Scanned() []*ContainerStatus {
	return r.scanned
}

// Updated returns updated containers.
//
// Returns:
//   - []*ContainerStatus: Updated list.
func (r *Report) Updated() []*ContainerStatus {
	return r.updated
}

// Failed returns failed containers.
//
// Returns:
//   - []*ContainerStatus: Failed list.
func (r *Report) Failed() []*ContainerStatus {
	return r.failed
}

// Skipped returns skipped containers.
//
// Returns:
//   - []*ContainerStatus: Skipped list.
func (r *Report) Skipped() []*ContainerStatus {
	return r.skipped
}

// Stale returns stale containers.
//
// Returns:
//   - []*ContainerStatus: Stale list.
func (r *Report) Stale() []*ContainerStatus {
	return r.stale
}

// Fresh returns fresh containers.
//
// Returns:
//   - []*ContainerStatus: Fresh list.
func (r *Report) Fresh() []*ContainerStatus {
	return r.fresh
}

// Restarted returns restarted containers.
//
// Returns:
//   - []*ContainerStatus: Restarted list.
func (r *Report) Restarted() []*ContainerStatus {
	return r.restarted
}

// All returns each container of the report once, sorted by container ID.
//
// A container listed in more than one category appears with its most
// significant one, in this order: updated, restarted, failed, skipped, stale,
// fresh, scanned.
//
// Returns:
//   - []*ContainerStatus: Sorted, unique list.
func (r *Report) All() []*ContainerStatus {
	allLen := len(r.scanned) + len(r.updated) + len(r.restarted) + len(r.failed) +
		len(r.skipped) + len(r.stale) + len(r.fresh)
	all := make([]*ContainerStatus, 0, allLen)
	presentIDs := make(map[container.ContainerID]struct{}, allLen)

	// appendUnique adds the containers not already added from a more
	// significant category.
	appendUnique := func(reports []*ContainerStatus) {
		for _, report := range reports {
			if _, found := presentIDs[report.ID()]; found {
				continue
			}

			all = append(all, report)
			presentIDs[report.ID()] = struct{}{}
		}
	}

	appendUnique(r.updated)
	appendUnique(r.restarted)
	appendUnique(r.failed)
	appendUnique(r.skipped)
	appendUnique(r.stale)
	appendUnique(r.fresh)
	appendUnique(r.scanned)

	slices.SortFunc(all, compareIDs)

	return all
}

// Focus returns the container a split report is about.
//
// Returns:
//   - *ContainerStatus: The container the notification is about, or nil
//     for the report of a whole session.
func (r *Report) Focus() *ContainerStatus {
	return r.focus
}

// newReport creates a report from progress data.
//
// Parameters:
//   - progress: Progress map to process.
//
// Returns:
//   - *Report: Categorized and sorted report.
func newReport(log *zerolog.Logger, progress Progress) *Report {
	report := &Report{
		scanned:   make([]*ContainerStatus, 0, len(progress)),
		updated:   make([]*ContainerStatus, 0),
		failed:    make([]*ContainerStatus, 0),
		skipped:   make([]*ContainerStatus, 0),
		stale:     make([]*ContainerStatus, 0),
		fresh:     make([]*ContainerStatus, 0),
		restarted: make([]*ContainerStatus, 0),
	}

	// Categorize each container status.
	for _, update := range progress {
		categorizeContainer(log, report, update)
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
func categorizeContainer(log *zerolog.Logger, report *Report, update *ContainerStatus) {
	if update.state == SkippedState {
		report.skipped = append(report.skipped, update)

		return
	}

	// Add non-skipped to scanned list.
	report.scanned = append(report.scanned, update)

	log.Debug().
		Str("container", update.containerName).
		Str("state_before_image_check", update.State()).
		Str("new_image", update.newImage.ShortID()).
		Str("old_image", update.oldImage.ShortID()).
		Bool("images_equal", update.newImage == update.oldImage).
		Msg("Categorizing container status")

	// Categorize based on image or state.
	if update.newImage == update.oldImage &&
		update.state != RestartedState &&
		update.state != FailedState {
		log.Debug().
			Str("container", update.containerName).
			Str("old_state", update.State()).
			Str("new_state", freshStateString).
			Msg("Setting container state to fresh due to equal images")
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
	case RestartedState:
		log.Debug().
			Str("container", update.containerName).
			Msg("Adding container to restarted list")
		report.restarted = append(report.restarted, update)
	default:
		update.state = StaleState
		report.stale = append(report.stale, update)
	}
}

// sortCategories sorts all report categories by container ID.
//
// Parameters:
//   - report: Report to sort.
func sortCategories(report *Report) {
	for _, list := range [][]*ContainerStatus{
		report.scanned, report.updated, report.failed, report.skipped,
		report.stale, report.fresh, report.restarted,
	} {
		slices.SortFunc(list, compareIDs)
	}
}

// compareIDs orders two container reports by container ID.
//
// Parameters:
//   - a, b: The reports to compare.
//
// Returns:
//   - int: Negative when a's ID sorts first, positive when b's does, zero when
//     they are equal.
func compareIDs(a, b *ContainerStatus) int {
	return cmp.Compare(a.ID(), b.ID())
}
