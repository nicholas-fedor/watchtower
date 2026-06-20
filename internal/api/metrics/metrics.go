package metrics

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/metrics"
)

// Handler serves the /v1/metrics endpoint.
//
//	@Summary		Prometheus metrics
//	@Description	Returns Watchtower scan metrics in Prometheus exposition format.
//	@Tags			metrics
//	@Produce		plain
//	@Success		200	{string}	string	"Prometheus exposition format metrics"
//	@Router			/v1/metrics [get]
type Handler struct {
	Path   string
	Handle fiber.Handler
}

// New creates a new metrics Handler backed by the default Prometheus registry.
//
// It initializes the default Watchtower metrics instance (registering gauges
// and counters with prometheus.DefaultRegisterer) and returns a Fiber handler
// that serves Prometheus exposition format metrics.
func New() *Handler {
	metrics.Default()

	handler := adaptor.HTTPHandler(promhttp.Handler())

	return &Handler{
		Path:   "/v1/metrics",
		Handle: handler,
	}
}

// StatusHandler serves the /v1/status endpoint.
type StatusHandler struct {
	Path    string
	getLast func() *metrics.Metric
}

// NewStatusHandler creates a new status handler that returns the last scan
// results as JSON.
func NewStatusHandler(getLast func() *metrics.Metric) *StatusHandler {
	return &StatusHandler{
		Path:    "/v1/status",
		getLast: getLast,
	}
}

// Handle responds with the last scan results as JSON.
//
//	@Summary		Last scan status
//	@Description	Returns the summary of the most recent Watchtower scan, including counts of scanned, updated, failed, restarted, and skipped containers.
//	@Tags			metrics
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	map[string]interface{}	"Scan summary with counts and timestamp"
//	@Success		204
//	@Router			/v1/status [get]
func (h *StatusHandler) Handle(c fiber.Ctx) error {
	logrus.WithFields(logrus.Fields{
		"method": c.Method(),
		"path":   c.Path(),
	}).Debug("Received HTTP API status request")

	last := h.getLast()
	if last == nil {
		sendErr := c.SendStatus(http.StatusNoContent)
		if sendErr != nil {
			return fmt.Errorf("failed to send no content response: %w", sendErr)
		}

		return nil
	}

	err := c.Status(http.StatusOK).JSON(fiber.Map{
		"summary": fiber.Map{
			"scanned":   last.Scanned,
			"updated":   last.Updated,
			"failed":    last.Failed,
			"restarted": last.Restarted,
			"skipped":   last.Skipped,
		},
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"api_version": "v1",
	})
	if err != nil {
		return fmt.Errorf("failed to send JSON response: %w", err)
	}

	return nil
}
