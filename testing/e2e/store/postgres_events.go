package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// AppendEvent writes one event.
//
// Parameters:
//   - ctx: Cancellation.
//   - event: Event without ID.
//
// Returns:
//   - Event: Row with ID filled.
//   - error: Insert failure.
func (p *Postgres) AppendEvent(ctx context.Context, event Event) (Event, error) {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}

	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage(`{}`)
	}

	err := p.pool.QueryRow(ctx, `
		INSERT INTO events (run_id, case_id, kind, payload, created_at)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		event.RunID, event.CaseID, string(event.Kind), []byte(event.Payload), event.CreatedAt,
	).Scan(&event.ID)
	if err != nil {
		return Event{}, fmt.Errorf("append event: %w", err)
	}

	return event, nil
}

// ListEvents returns events after afterID.
//
// Parameters:
//   - ctx: Cancellation.
//   - runID: Parent sitting.
//   - afterID: Exclusive cursor.
//   - limit: Page size.
//
// Returns:
//   - []Event: Events oldest first.
//   - error: Query failure.
func (p *Postgres) ListEvents(ctx context.Context, runID string, afterID int64, limit int) ([]Event, error) {
	limit = pageLimit(limit)

	rows, err := p.pool.Query(ctx, `
		SELECT id, run_id, case_id, kind, payload, created_at
		FROM events WHERE run_id = $1 AND id > $2
		ORDER BY id ASC LIMIT $3`, runID, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	out := make([]Event, 0)

	for rows.Next() {
		var (
			event   Event
			kind    string
			payload []byte
		)

		scanErr := rows.Scan(&event.ID, &event.RunID, &event.CaseID, &kind, &payload, &event.CreatedAt)
		if scanErr != nil {
			return nil, scanErr
		}

		event.Kind = EventKind(kind)
		event.Payload = payload
		out = append(out, event)
	}

	return out, rows.Err()
}
