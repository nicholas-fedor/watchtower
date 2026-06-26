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
// It returns a fiber.Handler that must be registered directly as a route
// handler so the SSE middleware can manage the response lifecycle.
//
//	@Summary		Real-time events stream
//	@Description	Streams Watchtower operational events (scan started/completed, update started/completed/failed) via Server-Sent Events.
//	@Tags			events
//	@Produce		text/event-stream
//	@Success		200	{string}	string	"Event stream"
//	@Router			/v1/events [get]
func (h *Handler) Handle() fiber.Handler {
	return sse.New(sse.Config{
		Retry: sseRetryInterval,
		Handler: func(c fiber.Ctx, stream *sse.Stream) error {
			origin := c.Get("Origin")

			host := c.Host()
			if origin != "" && !isOriginAllowed(origin, host) {
				logrus.WithFields(logrus.Fields{
					"origin": origin,
					"host":   host,
					"ip":     c.IP(),
				}).Warn("Rejected SSE connection from disallowed origin")

				return fiber.ErrForbidden
			}

			logrus.WithFields(logrus.Fields{
				"method": c.Method(),
				"path":   c.Path(),
				"ip":     c.IP(),
			}).Info("New SSE subscriber connected")

			if h.Broadcaster == nil {
				return fiber.ErrServiceUnavailable
			}

			subCh := h.Broadcaster.Subscribe()
			if subCh == nil {
				sendErr := c.Status(fiber.StatusServiceUnavailable).SendString("maximum number of subscribers reached")
				if sendErr != nil {
					return fmt.Errorf("failed to send subscriber limit response: %w", sendErr)
				}

				return nil
			}

			defer h.Broadcaster.Unsubscribe(subCh)

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
				}
			}
		},
	})
}

// isOriginAllowed checks if the given origin is allowed to connect to the SSE endpoint.
// An origin is allowed if it matches the request host (same-origin) or if it's
// the explicit literal "null" (used by sandboxed iframes and privacy contexts).
//
// Parameters:
//   - origin: The Origin header value.
//   - host: The request host (from c.Host()).
//
// Returns:
//   - bool: True if the origin is allowed.
func isOriginAllowed(origin, host string) bool {
	if origin == "" {
		return true
	}

	if origin == "null" {
		return true
	}

	if origin == host {
		return true
	}

	return false
}
