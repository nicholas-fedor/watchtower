package store

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"
)

// statusAllowed reports whether got is one of from.
//
// Parameters:
//   - got: Current status.
//   - from: Allowed statuses.
//
// Returns:
//   - bool: True when the transition may proceed.
func statusAllowed(got RunStatus, from []RunStatus) bool {
	return slices.Contains(from, got)
}

// CreateRun inserts a queued sitting.
//
// Parameters:
//   - ctx: Unused. Present to satisfy Store.
//   - run: Sitting to persist.
//
// Returns:
//   - error: ErrConflict when the ID exists.
func (m *Memory) CreateRun(_ context.Context, run Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.runs[run.ID]; exists {
		return fmt.Errorf("%w: run %s", ErrConflict, run.ID)
	}

	run.Status = cmp.Or(run.Status, RunQueued)

	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}

	m.runs[run.ID] = cloneRun(run)
	m.cases[run.ID] = make(map[string]Case)

	return nil
}

// GetRun loads one sitting.
//
// Parameters:
//   - ctx: Unused. Present to satisfy Store.
//   - id: Run UUID.
//
// Returns:
//   - Run: Sitting row.
//   - error: ErrNotFound or query failure.
func (m *Memory) GetRun(_ context.Context, id string) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	run, ok := m.runs[id]
	if !ok {
		return Run{}, fmt.Errorf("%w: run %s", ErrNotFound, id)
	}

	return cloneRun(run), nil
}

// ListRuns returns sittings newest first.
//
// Parameters:
//   - ctx: Unused. Present to satisfy Store.
//   - filter: Status and pagination.
//
// Returns:
//   - []Run: Page of sittings.
//   - error: Always nil.
func (m *Memory) ListRuns(_ context.Context, filter RunListFilter) ([]Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]Run, 0, len(m.runs))
	for _, run := range m.runs {
		if filter.Status != "" && run.Status != filter.Status {
			continue
		}

		out = append(out, cloneRun(run))
	}

	slices.SortFunc(out, func(a, b Run) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})

	limit := pageLimit(filter.Limit)
	if filter.Offset > len(out) {
		return []Run{}, nil
	}

	out = out[filter.Offset:]
	if len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}

// UpdateRunStatus sets status and related fields.
//
// Parameters:
//   - ctx: Unused. Present to satisfy Store.
//   - run: Sitting with fields to persist.
//
// Returns:
//   - error: ErrNotFound when the ID is unknown.
func (m *Memory) UpdateRunStatus(_ context.Context, run Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cur, ok := m.runs[run.ID]
	if !ok {
		return fmt.Errorf("%w: run %s", ErrNotFound, run.ID)
	}

	cur.Status = run.Status
	cur.StartedAt = run.StartedAt
	cur.FinishedAt = run.FinishedAt
	cur.Error = run.Error
	cur.Spec.Workers = run.Spec.Workers
	m.runs[run.ID] = cur

	return nil
}

// TransitionRun writes status fields when the sitting is currently one of from.
//
// Parameters:
//   - ctx: Unused. Present to satisfy Store.
//   - run: Sitting with fields to persist.
//   - from: Allowed current statuses.
//
// Returns:
//   - error: ErrNotFound or ErrConflict when the current status is not in from.
func (m *Memory) TransitionRun(_ context.Context, run Run, from ...RunStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cur, ok := m.runs[run.ID]
	if !ok {
		return fmt.Errorf("%w: run %s", ErrNotFound, run.ID)
	}

	if !statusAllowed(cur.Status, from) {
		return fmt.Errorf("%w: run %s is %s", ErrConflict, run.ID, cur.Status)
	}

	cur.Status = run.Status
	cur.StartedAt = run.StartedAt
	cur.FinishedAt = run.FinishedAt
	cur.Error = run.Error
	cur.Spec.Workers = run.Spec.Workers
	m.runs[run.ID] = cur

	return nil
}

// IncrementCounts atomically adds pass/fail/skip deltas.
//
// Parameters:
//   - ctx: Unused. Present to satisfy Store.
//   - runID: Sitting UUID.
//   - passed: Pass delta.
//   - failed: Fail delta.
//   - skipped: Skip delta.
//
// Returns:
//   - Run: Sitting after the increment.
//   - error: ErrNotFound when the ID is unknown.
func (m *Memory) IncrementCounts(_ context.Context, runID string, passed, failed, skipped int) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cur, ok := m.runs[runID]
	if !ok {
		return Run{}, fmt.Errorf("%w: run %s", ErrNotFound, runID)
	}

	cur.Passed += passed
	cur.Failed += failed
	cur.Skipped += skipped
	m.runs[runID] = cur

	return cloneRun(cur), nil
}

// SetRunWorkers writes the pool size when the sitting is still running.
//
// Parameters:
//   - ctx: Unused. Present to satisfy Store.
//   - runID: Sitting UUID.
//   - workers: Pool size.
//
// Returns:
//   - error: ErrNotFound when the ID is unknown.
func (m *Memory) SetRunWorkers(_ context.Context, runID string, workers int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cur, ok := m.runs[runID]
	if !ok {
		return fmt.Errorf("%w: run %s", ErrNotFound, runID)
	}

	if cur.Status != RunRunning {
		return nil
	}

	cur.Spec.Workers = workers
	m.runs[runID] = cur

	return nil
}

// NextQueued claims the oldest queued sitting when none is running.
//
// Parameters:
//   - ctx: Unused. Present to satisfy Store.
//
// Returns:
//   - Run: Claimed sitting, now running.
//   - error: ErrNotFound when the queue is empty or a sitting is already running.
func (m *Memory) NextQueued(_ context.Context) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, run := range m.runs {
		if run.Status == RunRunning {
			return Run{}, ErrNotFound
		}
	}

	var best Run

	found := false

	for _, run := range m.runs {
		if run.Status != RunQueued {
			continue
		}

		if !found || run.CreatedAt.Before(best.CreatedAt) || (run.CreatedAt.Equal(best.CreatedAt) && run.ID < best.ID) {
			best = run
			found = true
		}
	}

	if !found {
		return Run{}, ErrNotFound
	}

	best.Status = RunRunning
	best.StartedAt = cmp.Or(best.StartedAt, time.Now().UTC())
	m.runs[best.ID] = best

	return cloneRun(best), nil
}

// InterruptRunning marks running work interrupted.
//
// Parameters:
//   - ctx: Unused. Present to satisfy Store.
//
// Returns:
//   - error: Always nil.
func (m *Memory) InterruptRunning(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()

	for id, run := range m.runs {
		if run.Status != RunRunning {
			continue
		}

		run.Status = RunInterrupted
		run.FinishedAt = now
		m.runs[id] = run

		for caseID, item := range m.cases[id] {
			if item.Status != CaseRunning {
				continue
			}

			item.Status = CaseInterrupted
			item.FinishedAt = now
			m.cases[id][caseID] = item
		}
	}

	return nil
}
