package events

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/sse"
	"github.com/sirupsen/logrus"
)

// sseRetryInterval is the reconnect delay for SSE clients.
const sseRetryInterval = 5 * time.Second

// Handler serves the /v1/events endpoint with Server-Sent Events.
type Handler struct {
	Path           string
	Broadcaster    *Broadcaster
	AllowedOrigins []string
}

// NewHandler creates a new events handler backed by the given broadcaster.
func NewHandler(b *Broadcaster, allowedOrigins []string) *Handler {
	return &Handler{
		Path:           "/v1/events",
		Broadcaster:    b,
		AllowedOrigins: allowedOrigins,
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
			if origin != "" && !isOriginAllowed(origin, host, h.AllowedOrigins) {
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

		subCh, done := h.Broadcaster.SubscribeWithDone()
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
			case <-done:
				return nil
			case <-stream.Done():
				return stream.Err()
			}
		}
		},
	})
}

// isOriginAllowed reports whether origin is allowed for the SSE endpoint.
//
// Same-origin (with/without scheme), "null", and origins in allowedOrigins (or "*")
// are permitted.
//
// Parameters:
//   - origin: The Origin header value.
//   - host: The request host (from c.Host()).
//   - allowedOrigins: CORS allowed origins from config.
//
// Returns:
//   - bool: True if the origin is allowed.
func isOriginAllowed(origin, host string, allowedOrigins []string) bool {
	if origin == "" || origin == "null" {
		return true
	}

	// Parse the origin to extract just the host (scheme + host + port).
	originURL, err := url.Parse(origin)
	if err == nil && originURL.Host == host {
		return true
	}

	// Fallback to exact string comparison if URL parsing fails.
	if origin == host ||
		origin == "http://"+host ||
		origin == "https://"+host {
		return true
	}

	for _, allowed := range allowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}

	return false
}
