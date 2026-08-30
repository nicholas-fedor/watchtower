package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// CreateRun inserts a queued sitting.
//
// Parameters:
//   - ctx: Cancellation.
//   - run: Sitting to persist.
//
// Returns:
//   - error: Insert failure.
func (p *Postgres) CreateRun(ctx context.Context, run Run) error {
	run.Status = cmp.Or(run.Status, RunQueued)

	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}

	_, err := p.pool.Exec(ctx, `
		INSERT INTO runs (`+runColumns+`) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, $12,
			$13, $14, $15, $16, $17, $18, $19, $20
		)`,
		run.ID, run.Label, run.CreatedAt, nullTime(run.StartedAt), nullTime(run.FinishedAt), string(run.Status),
		run.Spec.Generator, run.Spec.Seed, run.Spec.Topic, run.Spec.Filter, run.Spec.FilePath, run.Spec.Shard,
		run.Spec.Offset, run.Spec.Limit, run.Spec.Workers, run.Spec.Keep, run.Passed, run.Failed, run.Skipped, run.Error,
	)
	if err != nil {
		return fmt.Errorf("insert run: %w", err)
	}

	return nil
}

// GetRun loads one sitting.
//
// Parameters:
//   - ctx: Cancellation.
//   - id: Run UUID.
//
// Returns:
//   - Run: Sitting row.
//   - error: ErrNotFound or query failure.
func (p *Postgres) GetRun(ctx context.Context, id string) (Run, error) {
	row := p.pool.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id = $1`, id)

	run, err := scanRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, fmt.Errorf("%w: run %s", ErrNotFound, id)
	}

	if err != nil {
		return Run{}, fmt.Errorf("get run: %w", err)
	}

	return run, nil
}

// ListRuns returns sittings newest first.
//
// Parameters:
//   - ctx: Cancellation.
//   - filter: Status and pagination.
//
// Returns:
//   - []Run: Page of sittings.
//   - error: Query failure.
func (p *Postgres) ListRuns(ctx context.Context, filter RunListFilter) ([]Run, error) {
	limit := pageLimit(filter.Limit)
	query := `SELECT ` + runColumns + ` FROM runs`
	args := []any{}

	if filter.Status != "" {
		query += ` WHERE status = $1`

		args = append(args, string(filter.Status))
		query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
		args = append(args, limit, filter.Offset)
	} else {
		query += ` ORDER BY created_at DESC LIMIT $1 OFFSET $2`

		args = append(args, limit, filter.Offset)
	}

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	out := make([]Run, 0)

	for rows.Next() {
		run, scanErr := scanRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}

		out = append(out, run)
	}

	return out, rows.Err()
}

// UpdateRunStatus sets status and related fields.
//
// Parameters:
//   - ctx: Cancellation.
//   - run: Sitting with fields to persist.
//
// Returns:
//   - error: ErrNotFound or update failure.
func (p *Postgres) UpdateRunStatus(ctx context.Context, run Run) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE runs SET
			status = $2, started_at = $3, finished_at = $4,
			error = $5, workers = $6
		WHERE id = $1`,
		run.ID, string(run.Status), nullTime(run.StartedAt), nullTime(run.FinishedAt),
		run.Error, run.Spec.Workers,
	)
	if err != nil {
		return fmt.Errorf("update run: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: run %s", ErrNotFound, run.ID)
	}

	return nil
}

// TransitionRun writes status fields when the sitting is currently one of from.
//
// Parameters:
//   - ctx: Cancellation.
//   - run: Sitting with fields to persist.
//   - from: Allowed current statuses.
//
// Returns:
//   - error: ErrNotFound, ErrConflict, or update failure.
func (p *Postgres) TransitionRun(ctx context.Context, run Run, from ...RunStatus) error {
	allowed := make([]string, len(from))
	for i, status := range from {
		allowed[i] = string(status)
	}

	tag, err := p.pool.Exec(ctx, `
		UPDATE runs SET
			status = $2, started_at = $3, finished_at = $4,
			error = $5, workers = $6
		WHERE id = $1 AND status = ANY($7)`,
		run.ID, string(run.Status), nullTime(run.StartedAt), nullTime(run.FinishedAt),
		run.Error, run.Spec.Workers, allowed,
	)
	if err != nil {
		return fmt.Errorf("transition run: %w", err)
	}

	if tag.RowsAffected() > 0 {
		return nil
	}

	cur, getErr := p.GetRun(ctx, run.ID)
	if getErr != nil {
		return getErr
	}

	return fmt.Errorf("%w: run %s is %s", ErrConflict, run.ID, cur.Status)
}

// IncrementCounts atomically adds pass/fail/skip deltas.
//
// Parameters:
//   - ctx: Cancellation.
//   - runID: Sitting UUID.
//   - passed: Pass delta.
//   - failed: Fail delta.
//   - skipped: Skip delta.
//
// Returns:
//   - Run: Sitting after the increment.
//   - error: ErrNotFound or update failure.
func (p *Postgres) IncrementCounts(ctx context.Context, runID string, passed, failed, skipped int) (Run, error) {
	row := p.pool.QueryRow(ctx, `
		UPDATE runs SET
			passed = passed + $2, failed = failed + $3, skipped = skipped + $4
		WHERE id = $1
		RETURNING `+runColumns, runID, passed, failed, skipped)

	run, err := scanRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, fmt.Errorf("%w: run %s", ErrNotFound, runID)
	}

	if err != nil {
		return Run{}, fmt.Errorf("increment counts: %w", err)
	}

	return run, nil
}

// SetRunWorkers writes the pool size when the sitting is still running.
//
// Parameters:
//   - ctx: Cancellation.
//   - runID: Sitting UUID.
//   - workers: Pool size.
//
// Returns:
//   - error: Update failure. Missing IDs are ignored when the sitting is not running.
func (p *Postgres) SetRunWorkers(ctx context.Context, runID string, workers int) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE runs SET workers = $2
		WHERE id = $1 AND status = $3`, runID, workers, string(RunRunning))
	if err != nil {
		return fmt.Errorf("set workers: %w", err)
	}

	if tag.RowsAffected() == 0 {
		_, getErr := p.GetRun(ctx, runID)
		if getErr != nil {
			return getErr
		}
	}

	return nil
}

// NextQueued claims the oldest queued sitting when none is running.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - Run: Claimed sitting, now running.
//   - error: ErrNotFound when the queue is empty or a sitting is already running.
func (p *Postgres) NextQueued(ctx context.Context) (Run, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Run{}, fmt.Errorf("next queued begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, lockErr := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(872314)`); lockErr != nil {
		return Run{}, fmt.Errorf("next queued lock: %w", lockErr)
	}

	now := time.Now().UTC()
	row := tx.QueryRow(ctx, `
		UPDATE runs
		SET status = $1, started_at = COALESCE(started_at, $2)
		WHERE status = $3
		  AND id = (
			SELECT id FROM runs
			WHERE status = $3
			  AND NOT EXISTS (SELECT 1 FROM runs WHERE status = $1)
			ORDER BY created_at ASC, id ASC
			LIMIT 1
		)
		RETURNING `+runColumns, string(RunRunning), now, string(RunQueued))

	run, scanErr := scanRun(row)
	if errors.Is(scanErr, pgx.ErrNoRows) {
		return Run{}, ErrNotFound
	}

	if scanErr != nil {
		return Run{}, fmt.Errorf("next queued: %w", scanErr)
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		return Run{}, fmt.Errorf("next queued commit: %w", commitErr)
	}

	return run, nil
}

// InterruptRunning marks running work interrupted.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: Update failure.
func (p *Postgres) InterruptRunning(ctx context.Context) error {
	now := time.Now().UTC()

	_, err := p.pool.Exec(ctx, `
		UPDATE runs SET status = $1, finished_at = $2
		WHERE status = $3`, RunInterrupted, now, RunRunning)
	if err != nil {
		return fmt.Errorf("interrupt runs: %w", err)
	}

	_, caseErr := p.pool.Exec(ctx, `
		UPDATE cases SET status = $1, finished_at = $2
		WHERE status = $3`, CaseInterrupted, now, CaseRunning)
	if caseErr != nil {
		return fmt.Errorf("interrupt cases: %w", caseErr)
	}

	return nil
}
