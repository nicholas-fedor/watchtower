package metrics

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/internal/metrics"
)

// Handler serves the /v1/metrics endpoint.
type Handler struct {
	Path string
}

// New creates a new metrics Handler backed by the default Prometheus registry.
//
// It initializes the default Watchtower metrics instance (registering gauges
// and counters with prometheus.DefaultRegisterer).
func New() *Handler {
	metrics.Default()

	return &Handler{
		Path: "/v1/metrics",
	}
}

// Handle serves Prometheus exposition format metrics.
//
//	@Summary		Prometheus metrics
//	@Description	Returns Watchtower scan metrics in Prometheus exposition format.
//	@Tags			metrics
//	@Produce		plain
//	@Success		200	{string}	string	"Prometheus exposition format metrics"
//	@Router			/v1/metrics [get]
func (h *Handler) Handle(c fiber.Ctx) error {
	return adaptor.HTTPHandler(promhttp.Handler())(c)
}

// StatusHandler serves the /v1/status endpoint.
type StatusHandler struct {
	Path    string
	getLast func() *metrics.Metric
}

// NewStatusHandler creates a new status handler that returns the last scan
// results as JSON.
//
// Parameters:
//   - getLast: Function that returns the last scan metric, or nil if no scan
//     has completed.
func NewStatusHandler(getLast func() *metrics.Metric) *StatusHandler {
	return &StatusHandler{
		Path:    "/v1/status",
		getLast: getLast,
	}
}

// Handle responds with the last scan results.
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
