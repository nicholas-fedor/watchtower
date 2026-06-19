package metrics

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/nicholas-fedor/watchtower/pkg/metrics"
)

// Handler serves the /v1/metrics endpoint.
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
