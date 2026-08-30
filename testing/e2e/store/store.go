package store

import (
	"context"
	"strings"
)

// Store is the persistence seam for runs, cases, and events.
type Store interface {
	// CreateRun inserts a queued sitting.
	CreateRun(ctx context.Context, run Run) error
	// GetRun loads one sitting.
	GetRun(ctx context.Context, id string) (Run, error)
	// ListRuns returns sittings newest first.
	ListRuns(ctx context.Context, filter RunListFilter) ([]Run, error)
	// UpdateRunStatus sets status, timestamps, error, and workers. Counts are IncrementCounts.
	UpdateRunStatus(ctx context.Context, run Run) error
	// TransitionRun writes status fields when the sitting is currently one of from.
	TransitionRun(ctx context.Context, run Run, from ...RunStatus) error
	// NextQueued claims the oldest queued sitting (queued → running) when none is running.
	NextQueued(ctx context.Context) (Run, error)
	// IncrementCounts atomically adds pass/fail/skip deltas and returns the sitting.
	IncrementCounts(ctx context.Context, runID string, passed, failed, skipped int) (Run, error)
	// SetRunWorkers writes the pool size when the sitting is still running.
	SetRunWorkers(ctx context.Context, runID string, workers int) error
	// InterruptRunning marks every running run and running case interrupted.
	InterruptRunning(ctx context.Context) error

	// UpsertCase writes a case row.
	UpsertCase(ctx context.Context, item Case) error
	// GetCase loads one case.
	GetCase(ctx context.Context, runID, caseID string) (Case, error)
	// ListCases returns cases for a run.
	ListCases(ctx context.Context, runID string, filter CaseListFilter) ([]Case, int, error)
	// CompletedIDs returns case IDs already in a terminal status for resume.
	CompletedIDs(ctx context.Context, runID string) (map[string]CaseStatus, error)

	// AppendEvent writes one event and returns it with ID filled.
	AppendEvent(ctx context.Context, event Event) (Event, error)
	// ListEvents returns events after afterID (exclusive), oldest first.
	ListEvents(ctx context.Context, runID string, afterID int64, limit int) ([]Event, error)

	// Close releases connections.
	Close() error
}

const (
	// defaultPageSize is used when the caller omits Limit.
	defaultPageSize = 50
	// MaxPageSize is the upper bound for list endpoints.
	MaxPageSize = 200
)

// pageLimit clamps a caller limit.
//
// Parameters:
//   - limit: Requested page size.
//
// Returns:
//   - int: limit if 1..MaxPageSize, else defaultPageSize or MaxPageSize.
func pageLimit(limit int) int {
	if limit < 1 {
		return defaultPageSize
	}

	return min(limit, MaxPageSize)
}

// likePattern wraps q as an ILIKE substring, escaping %, _, and \.
//
// Parameters:
//   - q: Raw substring from the operator.
//
// Returns:
//   - string: Pattern for ILIKE … ESCAPE '\'.
func likePattern(q string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)

	return "%" + escaped + "%"
}
