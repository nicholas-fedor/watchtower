package store

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
)

// UpsertCase writes a case row, merging empty document fields with the prior row.
//
// Parameters:
//   - ctx: Unused. Present to satisfy Store.
//   - item: Case to persist.
//
// Returns:
//   - error: ErrNotFound when the run is unknown.
func (m *Memory) UpsertCase(_ context.Context, item Case) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.runs[item.RunID]; !ok {
		return fmt.Errorf("%w: run %s", ErrNotFound, item.RunID)
	}

	if m.cases[item.RunID] == nil {
		m.cases[item.RunID] = make(map[string]Case)
	}

	if prev, ok := m.cases[item.RunID][item.CaseID]; ok {
		item = mergeCase(prev, item)
	}

	m.cases[item.RunID][item.CaseID] = cloneCase(item)

	return nil
}

// GetCase loads one case.
//
// Parameters:
//   - ctx: Unused. Present to satisfy Store.
//   - runID: Parent sitting.
//   - caseID: Case identifier.
//
// Returns:
//   - Case: Row.
//   - error: ErrNotFound or lookup failure.
func (m *Memory) GetCase(_ context.Context, runID, caseID string) (Case, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	byRun, ok := m.cases[runID]
	if !ok {
		return Case{}, fmt.Errorf("%w: run %s", ErrNotFound, runID)
	}

	item, ok := byRun[caseID]
	if !ok {
		return Case{}, fmt.Errorf("%w: case %s", ErrNotFound, caseID)
	}

	return cloneCase(item), nil
}

// ListCases returns cases for a run.
//
// Parameters:
//   - ctx: Unused. Present to satisfy Store.
//   - runID: Parent sitting.
//   - filter: Status, query, pagination.
//
// Returns:
//   - []Case: Page of cases.
//   - int: Total matches.
//   - error: ErrNotFound when the run is unknown.
func (m *Memory) ListCases(_ context.Context, runID string, filter CaseListFilter) ([]Case, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	byRun, ok := m.cases[runID]
	if !ok {
		if _, exists := m.runs[runID]; !exists {
			return nil, 0, fmt.Errorf("%w: run %s", ErrNotFound, runID)
		}

		return []Case{}, 0, nil
	}

	matched := make([]Case, 0, len(byRun))
	for _, item := range byRun {
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}

		if filter.Query != "" && !caseMatches(item, filter.Query) {
			continue
		}

		matched = append(matched, cloneCase(item))
	}

	slices.SortFunc(matched, func(a, b Case) int {
		return strings.Compare(a.CaseID, b.CaseID)
	})

	total := len(matched)
	limit := pageLimit(filter.Limit)

	if filter.Offset > len(matched) {
		return []Case{}, total, nil
	}

	matched = matched[filter.Offset:]
	if len(matched) > limit {
		matched = matched[:limit]
	}

	return matched, total, nil
}

// CompletedIDs returns terminal case IDs.
//
// Parameters:
//   - ctx: Unused. Present to satisfy Store.
//   - runID: Parent sitting.
//
// Returns:
//   - map[string]CaseStatus: Completed IDs.
//   - error: ErrNotFound when the run is unknown.
func (m *Memory) CompletedIDs(_ context.Context, runID string) (map[string]CaseStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.runs[runID]; !ok {
		return nil, fmt.Errorf("%w: run %s", ErrNotFound, runID)
	}

	out := make(map[string]CaseStatus)

	for id, item := range m.cases[runID] {
		switch item.Status {
		case CasePass, CaseFail, CaseSkip:
			out[id] = item.Status
		case CasePending, CaseRunning, CaseInterrupted:
		}
	}

	return out, nil
}

// mergeCase copies empty fields on next from prev.
//
// Parameters:
//   - prev: Existing row.
//   - next: Incoming row.
//
// Returns:
//   - Case: Merged row.
func mergeCase(prev, next Case) Case {
	next.Status = cmp.Or(next.Status, prev.Status)
	next.Error = cmp.Or(next.Error, prev.Error)
	next.DurationMs = cmp.Or(next.DurationMs, prev.DurationMs)
	next.HTTPDetails = cmp.Or(next.HTTPDetails, prev.HTTPDetails)
	next.StartedAt = cmp.Or(next.StartedAt, prev.StartedAt)
	next.FinishedAt = cmp.Or(next.FinishedAt, prev.FinishedAt)

	if len(next.Factors) == 0 {
		next.Factors = prev.Factors
	}

	if len(next.Expect) == 0 {
		next.Expect = prev.Expect
	}

	if len(next.Argv) == 0 {
		next.Argv = prev.Argv
	}

	if len(next.Env) == 0 {
		next.Env = prev.Env
	}

	if len(next.InspectBefore) == 0 {
		next.InspectBefore = prev.InspectBefore
	}

	if len(next.InspectAfter) == 0 {
		next.InspectAfter = prev.InspectAfter
	}

	if len(next.Porcelain) == 0 {
		next.Porcelain = prev.Porcelain
	}

	return next
}
