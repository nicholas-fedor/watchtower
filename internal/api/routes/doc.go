// Package routes registers all enabled API endpoints on a Fiber application.
//
// It provides the central ValidateAndRegister dispatcher that validates options
// and delegates to per-endpoint registration functions. Each route file wires a
// handler from the corresponding handlers subpackage into the Fiber router with
// appropriate middleware.
//
// Registered endpoints:
//
//	GET  /livez                  Liveness probe (no auth)
//	GET  /readyz                 Readiness probe — checks Docker client connectivity (no auth)
//	GET  /startupz               Startup probe (no auth)
//	POST /v1/update              Trigger container update scan (requires auth)
//	GET  /v1/metrics             Prometheus metrics (requires auth)
//	GET  /v1/status              Last scan results (requires auth)
//	GET  /v1/containers          List watched container statuses (requires auth)
//	GET  /v1/containers/details  Detailed container info (requires auth)
//	POST /v1/check               Check for available updates (requires auth)
//	GET  /v1/history             Historical scan results (requires auth)
//	GET  /v1/images              Tracked images with digests (requires auth)
//	GET  /v1/config              Active configuration settings (requires auth)
//	GET  /v1/events              Real-time events via SSE (events token auth)
//	GET  /swagger/*              Swagger UI
//
// File organization:
//   - routes.go:  ValidateAndRegister, Register dispatch.
//   - health.go:  /livez, /readyz, /startupz probes.
//   - update.go:  POST /v1/update.
//   - metrics.go: GET /v1/metrics, GET /v1/status.
//   - containers.go: GET /v1/containers, GET /v1/containers/details.
//   - check.go:   POST /v1/check.
//   - history.go: GET /v1/history.
//   - images.go:  GET /v1/images.
//   - config.go:  GET /v1/config.
//   - events.go:  GET /v1/events.
package routes
