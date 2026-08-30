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
//   - ctx: Unused. Present to satisfy Store.
//   - event: Event without ID.
//
// Returns:
//   - Event: Row with ID filled.
//   - error: ErrNotFound when the run is unknown.
func (m *Memory) AppendEvent(_ context.Context, event Event) (Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.runs[event.RunID]; !ok {
		return Event{}, fmt.Errorf("%w: run %s", ErrNotFound, event.RunID)
	}

	m.nextEv++

	event.ID = m.nextEv
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}

	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage(`{}`)
	}

	m.events = append(m.events, event)

	return event, nil
}

// ListEvents returns events after afterID.
//
// Parameters:
//   - ctx: Unused. Present to satisfy Store.
//   - runID: Parent sitting.
//   - afterID: Exclusive cursor.
//   - limit: Page size.
//
// Returns:
//   - []Event: Events oldest first.
//   - error: ErrNotFound when the run is unknown.
func (m *Memory) ListEvents(_ context.Context, runID string, afterID int64, limit int) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.runs[runID]; !ok {
		return nil, fmt.Errorf("%w: run %s", ErrNotFound, runID)
	}

	limit = pageLimit(limit)
	out := make([]Event, 0, limit)

	for _, event := range m.events {
		if event.RunID != runID || event.ID <= afterID {
			continue
		}

		out = append(out, event)
		if len(out) >= limit {
			break
		}
	}

	return out, nil
}
