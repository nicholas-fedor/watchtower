package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// UpsertCase writes a case row, merging empty document fields with the prior row.
//
// Parameters:
//   - ctx: Cancellation.
//   - item: Case to persist.
//
// Returns:
//   - error: Insert or update failure.
func (p *Postgres) UpsertCase(ctx context.Context, item Case) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO cases (`+caseColumns+`) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
		)
		ON CONFLICT (run_id, case_id) DO UPDATE SET
			status = EXCLUDED.status,
			factors = CASE WHEN EXCLUDED.factors = '{}'::jsonb THEN cases.factors ELSE EXCLUDED.factors END,
			expect = COALESCE(EXCLUDED.expect, cases.expect),
			argv = CASE WHEN EXCLUDED.argv = '[]'::jsonb THEN cases.argv ELSE EXCLUDED.argv END,
			env = CASE WHEN EXCLUDED.env = '{}'::jsonb THEN cases.env ELSE EXCLUDED.env END,
			error = CASE WHEN EXCLUDED.error = '' THEN cases.error ELSE EXCLUDED.error END,
			inspect_before = COALESCE(EXCLUDED.inspect_before, cases.inspect_before),
			inspect_after = COALESCE(EXCLUDED.inspect_after, cases.inspect_after),
			porcelain = COALESCE(EXCLUDED.porcelain, cases.porcelain),
			http_details = CASE WHEN EXCLUDED.http_details = '' THEN cases.http_details ELSE EXCLUDED.http_details END,
			started_at = COALESCE(EXCLUDED.started_at, cases.started_at),
			finished_at = COALESCE(EXCLUDED.finished_at, cases.finished_at),
			duration_ms = CASE WHEN EXCLUDED.duration_ms = 0 THEN cases.duration_ms ELSE EXCLUDED.duration_ms END`,
		item.RunID, item.CaseID, string(item.Status),
		jsonMap(item.Factors), nullJSON(item.Expect), jsonSlice(item.Argv), jsonMap(item.Env),
		item.Error, item.DurationMs,
		nullJSON(item.InspectBefore), nullJSON(item.InspectAfter), nullJSON(item.Porcelain),
		item.HTTPDetails, nullTime(item.StartedAt), nullTime(item.FinishedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert case: %w", err)
	}

	return nil
}

// GetCase loads one case.
//
// Parameters:
//   - ctx: Cancellation.
//   - runID: Parent sitting.
//   - caseID: Case identifier.
//
// Returns:
//   - Case: Row.
//   - error: ErrNotFound or query failure.
func (p *Postgres) GetCase(ctx context.Context, runID, caseID string) (Case, error) {
	row := p.pool.QueryRow(ctx, `SELECT `+caseColumns+` FROM cases WHERE run_id = $1 AND case_id = $2`, runID, caseID)

	item, err := scanCase(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Case{}, fmt.Errorf("%w: case %s", ErrNotFound, caseID)
	}

	if err != nil {
		return Case{}, fmt.Errorf("get case: %w", err)
	}

	return item, nil
}

// ListCases returns cases for a run.
//
// Parameters:
//   - ctx: Cancellation.
//   - runID: Parent sitting.
//   - filter: Status, query, pagination.
//
// Returns:
//   - []Case: Page of cases.
//   - int: Total matches.
//   - error: ErrNotFound or query failure.
func (p *Postgres) ListCases(ctx context.Context, runID string, filter CaseListFilter) ([]Case, int, error) {
	_, runErr := p.GetRun(ctx, runID)
	if runErr != nil {
		return nil, 0, runErr
	}

	where := []string{"run_id = $1"}
	args := []any{runID}
	idx := 2

	if filter.Status != "" {
		where = append(where, fmt.Sprintf("status = $%d", idx))
		args = append(args, string(filter.Status))
		idx++
	}

	if filter.Query != "" {
		where = append(where, fmt.Sprintf(
			`(case_id ILIKE $%d ESCAPE '\' OR factors::text ILIKE $%d ESCAPE '\')`, idx, idx,
		))
		args = append(args, likePattern(filter.Query))
		idx++
	}

	clause := strings.Join(where, " AND ")

	var total int

	countErr := p.pool.QueryRow(ctx, "SELECT COUNT(*) FROM cases WHERE "+clause, args...).Scan(&total)
	if countErr != nil {
		return nil, 0, fmt.Errorf("count cases: %w", countErr)
	}

	limit := pageLimit(filter.Limit)
	args = append(args, limit, filter.Offset)
	query := fmt.Sprintf(`SELECT `+caseColumns+` FROM cases WHERE %s ORDER BY case_id ASC LIMIT $%d OFFSET $%d`,
		clause, idx, idx+1)

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list cases: %w", err)
	}
	defer rows.Close()

	out := make([]Case, 0)

	for rows.Next() {
		item, scanErr := scanCase(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}

		out = append(out, item)
	}

	return out, total, rows.Err()
}

// CompletedIDs returns terminal case IDs.
//
// Parameters:
//   - ctx: Cancellation.
//   - runID: Parent sitting.
//
// Returns:
//   - map[string]CaseStatus: Completed IDs.
//   - error: Query failure.
func (p *Postgres) CompletedIDs(ctx context.Context, runID string) (map[string]CaseStatus, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT case_id, status FROM cases
		WHERE run_id = $1 AND status IN ('pass', 'fail', 'skip')`, runID)
	if err != nil {
		return nil, fmt.Errorf("completed ids: %w", err)
	}
	defer rows.Close()

	out := make(map[string]CaseStatus)

	for rows.Next() {
		var (
			id     string
			status string
		)

		scanErr := rows.Scan(&id, &status)
		if scanErr != nil {
			return nil, scanErr
		}

		out[id] = CaseStatus(status)
	}

	return out, rows.Err()
}
