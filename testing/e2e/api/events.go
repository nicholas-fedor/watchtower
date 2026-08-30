package api

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"

	"github.com/nicholas-fedor/watchtower/testing/e2e/control"
	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
)

const (
	// eventPoll is how often the SSE handler rereads the event log.
	eventPoll = 250 * time.Millisecond
	// eventPageSize is events fetched per poll.
	eventPageSize = 100
)

// registerEvents mounts the sitting SSE stream.
//
// Parameters:
//   - api: Huma API.
//   - svc: Control-plane service.
func registerEvents(api huma.API, svc *control.Service) {
	sse.Register(api, huma.Operation{
		OperationID: "stream-run-events",
		Method:      http.MethodGet,
		Path:        "/v1/runs/{id}/events",
		Summary:     "Server-sent events for a sitting",
		Tags:        []string{"runs"},
	}, map[string]any{
		"run_status": store.Event{},
		"case_start": store.Event{},
		"case_end":   store.Event{},
		"counts":     store.Event{},
		"pool":       store.Event{},
		"error":      store.Event{},
	}, streamRunEvents(svc))
}

// streamRunEvents returns the GET /v1/runs/{id}/events SSE handler.
//
// Parameters:
//   - svc: Control-plane service.
//
// Returns:
//   - func: Huma SSE handler.
func streamRunEvents(svc *control.Service) func(context.Context, *eventsInput, sse.Sender) {
	return func(ctx context.Context, input *eventsInput, send sse.Sender) {
		after := parseEventID(input.LastEventID)

		ticker := time.NewTicker(eventPoll)
		defer ticker.Stop()

		for {
			events, err := svc.ListEvents(ctx, input.ID, after, eventPageSize)
			if err != nil {
				payload, _ := jsonv2.Marshal(map[string]string{"error": err.Error()})
				_ = send(sse.Message{Data: store.Event{
					RunID:   input.ID,
					Kind:    "error",
					Payload: json.RawMessage(payload),
				}})

				return
			}

			for _, event := range events {
				if send(sse.Message{ID: int(event.ID), Data: event}) != nil {
					return
				}

				after = event.ID
			}

			select {
			case <-ctx.Done():
				return
			case <-svc.Stopping():
				return
			case <-ticker.C:
			}
		}
	}
}

// parseEventID reads Last-Event-ID as an int64 cursor.
//
// Parameters:
//   - raw: Header value. Empty or invalid becomes 0.
//
// Returns:
//   - int64: Exclusive event cursor.
func parseEventID(raw string) int64 {
	if raw == "" {
		return 0
	}

	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}

	return parsed
}
