package events

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/sse"
	"github.com/sirupsen/logrus"
)

// sseRetryInterval is the reconnect delay for SSE clients.
const sseRetryInterval = 5 * time.Second

// Handler serves the /v1/events endpoint with Server-Sent Events.
type Handler struct {
	Path        string
	Broadcaster *Broadcaster
}

// NewHandler creates a new events handler backed by the given broadcaster.
func NewHandler(b *Broadcaster) *Handler {
	return &Handler{
		Path:        "/v1/events",
		Broadcaster: b,
	}
}

// Handle streams Watchtower events to the client using Server-Sent Events.
//
//	@Summary		Real-time events stream
//	@Description	Streams Watchtower operational events (scan started/completed, update started/completed/failed) via Server-Sent Events.
//	@Tags			events
//	@Produce		text/event-stream
//	@Success		200				{string}	string	"Event stream"
//	@Router			/v1/events [get]
func (h *Handler) Handle(c fiber.Ctx) error {
	logrus.WithFields(logrus.Fields{
		"method": c.Method(),
		"path":   c.Path(),
	}).Debug("New SSE subscriber connected")

	ctx := c.Context()

	subCh := h.Broadcaster.Subscribe()
	defer h.Broadcaster.Unsubscribe(subCh)

	return sse.New(sse.Config{
		Retry: sseRetryInterval,
		Handler: func(_ fiber.Ctx, stream *sse.Stream) error {
			for {
				select {
				case event, ok := <-subCh:
					if !ok {
						return nil
					}

					data, err := json.Marshal(event)
					if err != nil {
						logrus.WithError(err).Warn("Failed to marshal event")

						continue
					}

					err = stream.Event(sse.Event{
						Name: event.Type,
						Data: string(data),
					})
					if err != nil {
						return fmt.Errorf("failed to send SSE event: %w", err)
					}
				case <-stream.Done():
					return stream.Err()
				case <-ctx.Done():
					return nil
				}
			}
		},
	})(c)
}
