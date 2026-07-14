// Package metrics provides the /v1/metrics HTTP API endpoint for serving Prometheus metrics.
//
// It wraps the standard promhttp.Handler via Fiber's adaptor middleware,
// preserving full compatibility with the Prometheus client_golang exposition format.
package metrics
